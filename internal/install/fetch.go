package install

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Downloading and unpacking a release.

// maxArchive bounds what is read from a release, so a wrong URL that serves
// something enormous cannot fill the disk before anyone notices.
const maxArchive = 256 << 20

// Progress is told how a download is going. It is called from the goroutine
// doing the fetch, so an implementation that touches a UI has to marshal.
type Progress func(done, total int64)

// Fetch downloads a release archive, trying each of its URLs in turn.
//
// The fallback is the point of the list: a release found on GitHub is still
// downloaded from the mirror when GitHub is unreachable, which is the case the
// mirror exists for. Each attempt is reported so the caller can say which host
// it is using.
func (c *Client) Fetch(ctx context.Context, rel Release, dst string, report func(host string), onProgress Progress) error {
	if len(rel.URLs) == 0 {
		return fmt.Errorf("%s %s: no download address", rel.Source.Name, rel.Version)
	}
	var errs []string
	for _, url := range rel.URLs {
		if report != nil {
			report(hostOf(url))
		}
		if err := c.fetchOne(ctx, url, dst, onProgress); err != nil {
			errs = append(errs, hostOf(url)+": "+err.Error())
			continue
		}
		return nil
	}
	return fmt.Errorf("could not download %s %s: %s", rel.Source.Name, rel.Version, strings.Join(errs, "; "))
}

// fetchOne downloads a single URL to a file.
//
// The body is written to a temporary file beside the destination and renamed
// once it is complete, so an interrupted download never leaves a half-written
// archive where the next run would find it and think it whole.
func (c *Client) fetchOne(ctx context.Context, url, dst string, onProgress Progress) error {
	// A download gets its own deadline rather than the client's, which is sized
	// for a metadata check: a 9 MB archive over a slow link is minutes, and a
	// check that gives up in twenty seconds would never finish it.
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.downloadClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s", resp.Status)
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer func() {
		f.Close()
		_ = os.Remove(tmp)
	}()

	total := resp.ContentLength
	var done int64
	buf := make([]byte, 64<<10)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			done += int64(n)
			if done > maxArchive {
				return fmt.Errorf("the archive is larger than %d MB", maxArchive>>20)
			}
			if _, err := f.Write(buf[:n]); err != nil {
				return err
			}
			if onProgress != nil {
				onProgress(done, total)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// downloadClient is the client used for archives: no overall deadline of its
// own, because the context already carries one that a download can respect.
func (c *Client) downloadClient() *http.Client {
	base := c.HTTP
	if base == nil {
		return &http.Client{}
	}
	clone := *base
	clone.Timeout = 0
	return &clone
}

// hostOf names the site a URL belongs to, for a message.
func hostOf(url string) string {
	switch {
	case strings.Contains(url, "github.com"):
		return "GitHub"
	case strings.Contains(url, "cnb.cool"):
		return "cnb.cool"
	}
	return url
}

// Extracted is one file taken out of a release archive.
type Extracted struct {
	// Name is the path inside the archive.
	Name string
	// Path is where it was written.
	Path string
}

// Unzip extracts an archive into a directory.
//
// Only the archive's own relative paths are honoured, and anything that would
// escape the destination is refused: an archive is a file from the network, and
// a path containing ".." or a drive letter is how a file from the network ends
// up somewhere it was never meant to go.
func Unzip(archive, dir string) ([]Extracted, error) {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}

	var out []Extracted
	for _, f := range r.File {
		// A directory entry carries no data.
		if f.FileInfo().IsDir() {
			continue
		}
		name := filepath.FromSlash(f.Name)
		if !safeRelative(name) {
			return nil, fmt.Errorf("the archive contains an unsafe path: %s", f.Name)
		}
		dst := filepath.Join(root, name)
		// Belt and braces: the join is checked as well as the name, so a rule
		// that turns out to be incomplete still cannot write outside.
		if !within(root, dst) {
			return nil, fmt.Errorf("the archive contains a path outside its own directory: %s", f.Name)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return nil, err
		}
		if err := writeZipEntry(f, dst); err != nil {
			return nil, err
		}
		out = append(out, Extracted{Name: name, Path: dst})
	}
	return out, nil
}

// safeRelative reports whether a path from an archive stays inside the
// directory it is extracted to.
func safeRelative(name string) bool {
	if name == "" || filepath.IsAbs(name) {
		return false
	}
	// A drive letter or a UNC prefix is absolute on Windows even though
	// filepath.IsAbs sees it only after cleaning.
	if filepath.VolumeName(name) != "" {
		return false
	}
	cleaned := filepath.Clean(name)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

// within reports whether a path is inside a directory.
func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// writeZipEntry writes one file out of an archive.
func writeZipEntry(f *zip.File, dst string) error {
	src, err := f.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer dstFile.Close()
	if _, err := io.Copy(dstFile, io.LimitReader(src, maxArchive)); err != nil {
		return err
	}
	return dstFile.Sync()
}
