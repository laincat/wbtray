// Package install fetches and updates the two things wbtray works with: its own
// releases and the gateway's.
//
// Both are published the same way — a GitHub release with platform archives
// attached — and both have a mirror on cnb.cool for readers who cannot reach
// GitHub. The package therefore models "a project that publishes releases", and
// the two projects differ only in which repository and which asset name.
package install

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Source is a project that publishes platform archives as release assets.
type Source struct {
	// Name identifies it in messages.
	Name string
	// GitHub is the owner/repo on GitHub, empty when there is no GitHub project.
	GitHub string
	// CNB is the owner/repo on cnb.cool, empty when there is no mirror.
	CNB string
	// AssetName builds the archive's file name for a version. It takes the
	// version because the upstream project puts its tag in the name.
	AssetName func(version string) string
	// MirrorAssetName is the name this project's archive is given inside the
	// tray's own release, or empty when the project is the tray itself.
	MirrorAssetName string
	// MirrorVersionAsset is the name of a one-line file published beside the
	// mirrored archive, holding the upstream tag it was copied from. It is what
	// makes an update check possible when GitHub cannot be reached at all: the
	// archive's own name cannot carry the version, because the mirrored copy is
	// replaced in place on every release.
	MirrorVersionAsset string
}

// Gateway is the upstream workbuddy2api-panel release.
func Gateway() Source {
	return Source{
		Name:   "workbuddy2api-panel",
		GitHub: "linguo2625469/workbuddy2api-panel",
		// No cnb.cool project of its own: its archive is carried inside wbtray's
		// release, which is what makes one mirror do for both.
		CNB: "",
		AssetName: func(v string) string {
			return "wb2api-panel-" + v + "-windows-amd64.zip"
		},
		MirrorAssetName:    "workbuddy2api-panel-windows-amd64.zip",
		MirrorVersionAsset: "workbuddy2api-panel-version.txt",
	}
}

// Tray is wbtray's own release.
func Tray() Source {
	return Source{
		Name:   "wbtray",
		GitHub: "laincat/wbtray",
		CNB:    "laincat/wbtray",
		AssetName: func(v string) string {
			return "wbtray-" + v + "-windows-amd64.zip"
		},
	}
}

// Release is one published version and the places its archive can be fetched.
type Release struct {
	Source Source
	// Version is the tag as published, with its leading v.
	Version string
	// URLs are the places the archive can be fetched, best first: GitHub before
	// the mirror, and any mirror-carried copy last.
	URLs []string
}

// URL is the first place to try.
func (r Release) URL() string {
	if len(r.URLs) == 0 {
		return ""
	}
	return r.URLs[0]
}

// AssetName is the archive's file name for this release.
func (r Release) AssetName() string { return r.Source.AssetName(r.Version) }

// Client discovers and fetches releases.
type Client struct {
	HTTP *http.Client
	// UserAgent is sent on every request. GitHub rejects requests without one,
	// and a proxy in front of a mirror may do the same.
	UserAgent string
	// GitHubBase and CNBBase are the sites to ask, as scheme and host. They are
	// fields rather than constants so a test can point them at its own server:
	// the two hosts answer in different shapes, and those shapes are the part
	// most likely to be wrong and therefore the part worth testing.
	GitHubBase string
	CNBBase    string
	// CNBMirror is the cnb.cool project carrying mirrored copies of other
	// projects' archives. Empty means the tray's own project.
	CNBMirror string
}

// New builds a client with a timeout suitable for a tray: short enough that a
// blocked host does not hang the check for long, long enough for a slow response
// to arrive. Downloads use their own, longer deadline.
func New() *Client {
	return &Client{
		HTTP:       &http.Client{Timeout: 20 * time.Second},
		UserAgent:  "wbtray",
		GitHubBase: "https://github.com",
		CNBBase:    "https://cnb.cool",
	}
}

// Latest finds a project's newest release, trying each host in turn.
//
// Every host that answers contributes a URL, in the order it was tried, so a
// release discovered on GitHub can still be downloaded from the mirror if GitHub
// turns out to be unreachable at the moment of the download — which is the case
// this mechanism exists for.
func (c *Client) Latest(ctx context.Context, src Source) (Release, error) {
	var rel Release
	var errs []string
	// Which hosts answered. Only the ones that answered contribute download
	// addresses: offering a download from a host whose check has just failed
	// spends the fetch's first attempt on the site already known to be
	// unreachable, which is the opposite of a fallback.
	var fromGitHub, fromCNB, fromMirror bool
	var mirrorTag string

	if src.GitHub != "" {
		version, err := c.latestOnGitHub(ctx, src.GitHub)
		if err != nil {
			errs = append(errs, "github: "+err.Error())
		} else {
			rel = Release{Source: src, Version: version}
			fromGitHub = true
		}
	}
	if src.CNB != "" {
		version, err := c.latestOnCNB(ctx, src.CNB)
		if err != nil {
			errs = append(errs, "cnb: "+err.Error())
		} else {
			fromCNB = true
			if rel.Version == "" {
				rel = Release{Source: src, Version: version}
			}
		}
	}
	// The tray's own mirror can also carry this project's archive, which is the
	// only way the upstream gateway is reachable when GitHub is not. Its version
	// is read from the small file published beside the archive, because the
	// mirrored copy's name cannot carry a version: it is replaced in place.
	if src.MirrorAssetName != "" && c.trayMirror() != "" {
		if version, err := c.latestOnCNB(ctx, c.trayMirror()); err != nil {
			errs = append(errs, "mirror: "+err.Error())
		} else {
			mirrorTag = version
			fromMirror = true
			if rel.Version == "" {
				upstream, err := c.mirrorVersion(ctx, mirrorTag, src.MirrorVersionAsset)
				if err != nil {
					errs = append(errs, "mirror: "+err.Error())
					fromMirror = false
				} else {
					rel = Release{Source: src, Version: upstream}
				}
			}
		}
	}
	if rel.Version == "" {
		return Release{}, fmt.Errorf("no release found: %s", strings.Join(errs, "; "))
	}

	// The download order, GitHub first and the mirrored copy last: the mirror is
	// the one that depends on wbtray's own release having been published.
	if fromGitHub {
		rel.URLs = append(rel.URLs,
			c.githubAssetURL(src.GitHub, rel.Version, src.AssetName(rel.Version)))
	}
	if fromCNB {
		rel.URLs = append(rel.URLs,
			c.cnbAssetURL(src.CNB, rel.Version, src.AssetName(rel.Version)))
	}
	if fromMirror {
		rel.URLs = append(rel.URLs,
			c.cnbAssetURL(c.trayMirror(), mirrorTag, src.MirrorAssetName))
	}
	return rel, nil
}

// mirrorVersion reads the upstream version recorded beside a mirrored archive.
func (c *Client) mirrorVersion(ctx context.Context, tag, asset string) (string, error) {
	if asset == "" {
		return "", fmt.Errorf("no version file is published for the mirrored archive")
	}
	url := c.cnbAssetURL(c.trayMirror(), tag, asset)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("the mirrored version file returned %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<10))
	if err != nil {
		return "", err
	}
	version := strings.TrimSpace(string(body))
	if version == "" {
		return "", fmt.Errorf("the mirrored version file is empty")
	}
	return version, nil
}

// trayMirror is the cnb.cool project whose releases carry the mirrored upstream
// archive.
func (c *Client) trayMirror() string {
	if c.CNBMirror != "" {
		return c.CNBMirror
	}
	return "laincat/wbtray"
}

// latestOnGitHub asks GitHub which release is newest.
//
// The API would be the obvious way and is the wrong one here: it is rate-limited
// per address, and a tray behind a shared address would start failing for the
// wrong reason. The release page redirects to the newest tag instead, costs one
// request, and needs no token.
func (c *Client) latestOnGitHub(ctx context.Context, repo string) (string, error) {
	url := c.githubBase() + "/" + repo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", c.UserAgent)

	// The redirect must not be followed: the tag is in the location it points at,
	// and following it would fetch the whole release page.
	client := *c.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))

	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("%s has no releases", repo)
	}
	if resp.StatusCode < 300 || resp.StatusCode >= 400 {
		return "", fmt.Errorf("%s returned %s", url, resp.Status)
	}
	loc := resp.Header.Get("Location")
	i := strings.Index(loc, "/releases/tag/")
	if i < 0 {
		return "", fmt.Errorf("%s redirected to %s, which names no tag", url, loc)
	}
	tag := strings.Trim(loc[i+len("/releases/tag/"):], "/")
	if tag == "" {
		return "", fmt.Errorf("%s redirected to an empty tag", url)
	}
	return tag, nil
}

// latestOnCNB asks cnb.cool for the newest tag.
//
// cnb.cool has no "latest release" endpoint to redirect, so the tags are read the
// way git itself reads them: one request to the upload-pack advertisement, which
// lists every ref and needs no credential for a public repository. It is the same
// request `git ls-remote` makes, and the alternative — the web page — is a
// JavaScript application whose content cannot be read without running it.
func (c *Client) latestOnCNB(ctx context.Context, repo string) (string, error) {
	url := c.cnbBase() + "/" + repo + ".git/info/refs?service=git-upload-pack"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s returned %s", url, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	return newestTag(parseRefs(string(body)))
}

// tagRefRe matches the tag refs in a git ref advertisement.
var tagRefRe = regexp.MustCompile(`refs/tags/([^\x00-\x20^]+)`)

// parseRefs pulls the tag names out of an upload-pack advertisement.
func parseRefs(body string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range tagRefRe.FindAllStringSubmatch(body, -1) {
		name := m[1]
		// A tag pointing at a tag object also advertises a peeled ref with a
		// ^{} suffix; it names the same version and would be a duplicate.
		name = strings.TrimSuffix(name, "^{}")
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

// TagsOnGitHub returns every version tag a GitHub project has published.
//
// It reads the same ref advertisement the mirror is read with, rather than the
// API: the advertisement is what git itself uses, it costs one request, and it is
// not rate-limited by address. The API would be needed for the richer fields —
// assets, dates — and the updater does not need any of them.
func (c *Client) TagsOnGitHub(ctx context.Context, repo string) ([]string, error) {
	url := c.githubBase() + "/" + repo + ".git/info/refs?service=git-upload-pack"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned %s", url, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	return parseRefs(string(body)), nil
}

// newestTag picks the highest version from a list of tags.
//
// Versions are compared as versions rather than as text, because otherwise
// v1.9.0 sorts above v1.11.0 — and a mirror that silently offers an older
// release as the newest is worse than one that offers nothing.
func newestTag(tags []string) (string, error) {
	if len(tags) == 0 {
		return "", fmt.Errorf("no tags found")
	}
	best := tags[0]
	for _, t := range tags[1:] {
		if CompareVersions(t, best) > 0 {
			best = t
		}
	}
	return best, nil
}

// CompareVersions orders two tags, returning a negative number when a is older
// than b. A tag that cannot be parsed compares by its text, so an unusual tag
// never crashes the check and never outranks a real version.
func CompareVersions(a, b string) int {
	an, aok := parseVersion(a)
	bn, bok := parseVersion(b)
	if !aok || !bok {
		return strings.Compare(a, b)
	}
	for i := 0; i < len(an) || i < len(bn); i++ {
		var av, bv int
		if i < len(an) {
			av = an[i]
		}
		if i < len(bn) {
			bv = bn[i]
		}
		if av != bv {
			return av - bv
		}
	}
	return 0
}

// parseVersion reads the numeric parts of a tag such as v1.11.6-panel.
func parseVersion(tag string) ([]int, bool) {
	s := strings.TrimPrefix(strings.TrimSpace(tag), "v")
	// Everything from the first character that is neither a digit nor a dot is a
	// pre-release or channel suffix and takes no part in the comparison.
	end := len(s)
	for i, r := range s {
		if (r < '0' || r > '9') && r != '.' {
			end = i
			break
		}
	}
	s = s[:end]
	if s == "" {
		return nil, false
	}
	parts := strings.Split(s, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}

// githubBase is the site to ask about GitHub releases, without a trailing slash.
func (c *Client) githubBase() string {
	if c.GitHubBase != "" {
		return strings.TrimRight(c.GitHubBase, "/")
	}
	return "https://github.com"
}

// cnbBase is the site to ask about the mirror, without a trailing slash.
func (c *Client) cnbBase() string {
	if c.CNBBase != "" {
		return strings.TrimRight(c.CNBBase, "/")
	}
	return "https://cnb.cool"
}

// githubAssetURL is where a release asset is downloaded from on GitHub.
func (c *Client) githubAssetURL(repo, version, asset string) string {
	return c.githubBase() + "/" + repo + "/releases/download/" + version + "/" + asset
}

// cnbAssetURL is the same on cnb.cool. The shape is the platform's rather than a
// guess: it is what the release page's own download links resolve through.
func (c *Client) cnbAssetURL(repo, version, asset string) string {
	return c.cnbBase() + "/" + repo + "/-/releases/download/" + version + "/" + asset
}
