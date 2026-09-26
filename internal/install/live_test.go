package install

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// Live checks against the real hosts.
//
// They are skipped by default because they need the network and would fail on a
// machine that is offline or behind a firewall that blocks one of the two sites.
// They are worth having anyway: the shapes of the two hosts' answers are the
// part of this package that cannot be reasoned about from the documentation, and
// the only way to know they are still right is to ask.
//
//	$env:WBTRAY_LIVE = "1"
//	go test ./internal/install -run Live -v

func liveOnly(t *testing.T) {
	t.Helper()
	if os.Getenv("WBTRAY_LIVE") == "" {
		t.Skip("set WBTRAY_LIVE=1 to check the real hosts")
	}
}

// TestLiveUpstreamLatest checks that the upstream project's newest release is
// found, and that the archive name the source builds is one it actually publishes.
func TestLiveUpstreamLatest(t *testing.T) {
	liveOnly(t)
	c := New()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rel, err := c.Latest(ctx, Gateway())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("upstream latest: %s", rel.Version)
	t.Logf("download addresses: %v", rel.URLs)
	if !strings.HasPrefix(rel.Version, "v") {
		t.Errorf("version %q does not look like a tag", rel.Version)
	}
	// The archive name is built from the tag and has to match what upstream
	// actually attaches, or every download would 404.
	if got := rel.AssetName(); !strings.Contains(got, rel.Version) {
		t.Errorf("asset name %q does not contain the version", got)
	}
	checkURLExists(t, rel.URL())
}

// TestLiveTrayLatest checks the tray's own repository on both hosts.
func TestLiveTrayLatest(t *testing.T) {
	liveOnly(t)
	c := New()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// The mirror's tag list, which needs no release to exist.
	tag, err := c.latestOnCNB(ctx, c.trayMirror())
	if err != nil {
		t.Fatalf("cnb.cool tag discovery failed: %v", err)
	}
	t.Logf("cnb.cool says the newest wbtray tag is %s", tag)

	// And GitHub's, compared so the two can be seen to agree.
	gh, err := c.latestOnGitHub(ctx, Tray().GitHub)
	if err != nil {
		t.Fatalf("github tag discovery failed: %v", err)
	}
	t.Logf("github says the newest wbtray tag is %s", gh)
}

// TestLiveTagDiscoveryAgrees is the check that the two hosts see the same
// versions, which is what makes the mirror a mirror.
func TestLiveTagDiscoveryAgrees(t *testing.T) {
	liveOnly(t)
	c := New()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ghTags, err := c.TagsOnGitHub(ctx, Gateway().GitHub)
	if err != nil {
		t.Fatal(err)
	}
	if len(ghTags) == 0 {
		t.Fatal("no upstream tags were found")
	}
	newest, err := newestTag(ghTags)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("upstream has %d tags; newest is %s", len(ghTags), newest)
}

// checkURLExists confirms that a download address is real, without reading the
// whole file.
func checkURLExists(t *testing.T, url string) {
	t.Helper()
	c := New()
	req, err := http.NewRequest(http.MethodHead, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("could not reach %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("%s returned %s", url, resp.Status)
	} else {
		t.Logf("%s -> %d (%d bytes)", url, resp.StatusCode, resp.ContentLength)
	}
}
