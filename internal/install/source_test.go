package install

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCompareVersionsOrdersNumerically is the rule that makes "newest" mean
// newest. Comparing tags as text puts v1.9.0 above v1.11.0, and a mirror that
// quietly offers an older release as the newest is worse than one that offers
// nothing at all.
func TestCompareVersionsOrdersNumerically(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int // sign only
	}{
		{"v1.9.0", "v1.11.0", -1},
		{"v1.11.0", "v1.9.0", 1},
		{"v1.11.6", "v1.11.6", 0},
		{"v1.11.6-panel", "v1.11.6", 0},
		{"v2.0.0", "v1.99.99", 1},
		{"v1.10.0", "v1.9.2", 1},
		{"v1.11.6", "v1.11.7", -1},
	} {
		got := CompareVersions(tc.a, tc.b)
		if sign(got) != tc.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want sign %d", tc.a, tc.b, got, tc.want)
		}
	}
}

// TestNewestTagPicksTheHighest covers the shape the real repositories publish:
// tags with a leading v, a channel suffix, and no particular order.
func TestNewestTagPicksTheHighest(t *testing.T) {
	got, err := newestTag([]string{
		"v1.10.0", "v1.9.1", "v1.11.5", "v1.11.6", "v1.11.4", "v1.2.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "v1.11.6" {
		t.Fatalf("newestTag = %q, want v1.11.6", got)
	}
	if _, err := newestTag(nil); err == nil {
		t.Fatal("newestTag accepted an empty list")
	}
}

// TestParseRefsReadsAGitAdvertisement uses a real advertisement, captured from
// the upstream repository, because the format is binary-ish and a synthetic
// sample would only test what I assumed it looked like.
func TestParseRefsReadsAGitAdvertisement(t *testing.T) {
	body := "001e# service=git-upload-pack\x00000001597e72c8ee5eeaf6135eb341722788a6c9832ec7ec HEAD\x00multi_ack" +
		"001f^refs/tags/v1.10.0\x00hash1 refs/tags/v1.10.0^{}\x00003f2ee8093dff406c28af985a115e711646aa5d80d3 refs/tags/v1.11.0" +
		"\x00003f63c674448b25230a82cd72d9c85b5bab03ef0248 refs/tags/v1.11.6\x0000000c refs/heads/main\x00"
	tags := parseRefs(body)
	want := map[string]bool{"v1.10.0": true, "v1.11.0": true, "v1.11.6": true}
	if len(tags) != len(want) {
		t.Fatalf("parseRefs found %v, want %v", tags, want)
	}
	for _, tag := range tags {
		if !want[tag] {
			t.Errorf("parseRefs returned %q, which is not a tag in the advertisement", tag)
		}
	}
	// A peeled ref must not appear as a duplicate version.
	for _, tag := range tags {
		if strings.HasSuffix(tag, "^{}") {
			t.Errorf("parseRefs kept the peeled ref %q", tag)
		}
	}
}

// fakeGitHub answers the release redirect the way GitHub does.
func fakeGitHub(t *testing.T, latest string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/releases/latest") {
			w.Header().Set("Location", "https://example.invalid/releases/tag/"+latest)
			w.WriteHeader(http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// fakeCNB answers the ref advertisement the way cnb.cool does.
func fakeCNB(t *testing.T, tags ...string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A release asset request is answered with the file's contents, which is
		// how the mirrored version note is fetched.
		if strings.Contains(r.URL.Path, "/releases/download/") {
			if strings.HasSuffix(r.URL.Path, "workbuddy2api-panel-version.txt") {
				_, _ = w.Write([]byte("v1.11.6\n"))
				return
			}
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var b strings.Builder
		for _, tag := range tags {
			b.WriteString("003f0000000000000000000000000000000000 refs/tags/")
			b.WriteString(tag)
			b.WriteString("\x00\x00")
		}
		_, _ = w.Write([]byte(b.String()))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// baseOf is the server URL, which is what the client wants as a base.
func baseOf(srv *httptest.Server) string {
	return srv.URL
}

// TestLatestPrefersGitHubButKeepsTheMirror checks the property the fallback rests
// on: a release discovered on GitHub still carries the mirror's address, so the
// download can succeed from cnb.cool when GitHub is blocked at that moment.
func TestLatestPrefersGitHubButKeepsTheMirror(t *testing.T) {
	github := fakeGitHub(t, "v9.9.9")
	cnb := fakeCNB(t, "v9.9.8")

	c := New()
	c.GitHubBase = baseOf(github)
	c.CNBBase = baseOf(cnb)
	c.CNBMirror = "laincat/wbtray"

	rel, err := c.Latest(context.Background(), Tray())
	if err != nil {
		t.Fatal(err)
	}
	// GitHub's tag wins, being newer.
	if rel.Version != "v9.9.9" {
		t.Errorf("version = %q, want the newer v9.9.9", rel.Version)
	}
	if len(rel.URLs) < 2 {
		t.Fatalf("only %d download addresses were collected: %v", len(rel.URLs), rel.URLs)
	}
	if !strings.Contains(rel.URLs[0], baseOf(github)) {
		t.Errorf("the first address is %q, which is not GitHub", rel.URLs[0])
	}
	if !strings.Contains(rel.URLs[1], baseOf(cnb)) {
		t.Errorf("the second address is %q, which is not the mirror", rel.URLs[1])
	}
}

// TestLatestFallsBackToTheMirrorAlone checks that a machine which cannot reach
// GitHub still finds a release, which is the whole point of the mirror.
func TestLatestFallsBackToTheMirrorAlone(t *testing.T) {
	cnb := fakeCNB(t, "v9.9.8")

	c := New()
	// A host that refuses the connection stands in for a blocked GitHub.
	c.GitHubBase = "http://127.0.0.1:1"
	c.CNBBase = baseOf(cnb)
	c.CNBMirror = "laincat/wbtray"

	src := Tray()
	rel, err := c.Latest(context.Background(), src)
	if err != nil {
		t.Fatalf("the mirror alone was not enough: %v", err)
	}
	if rel.Version != "v9.9.8" {
		t.Errorf("version = %q, want v9.9.8", rel.Version)
	}
	if !strings.Contains(rel.URL(), baseOf(cnb)) {
		t.Errorf("the only address is %q, which is not the mirror", rel.URL())
	}
}

// TestGatewayIsFoundWithoutGitHubAtAll is the case the mirror exists for: the
// upstream project has no cnb.cool repository of its own, and its archive is
// reachable only because wbtray's release carries a copy.
func TestGatewayIsFoundWithoutGitHubAtAll(t *testing.T) {
	cnb := fakeCNB(t, "v1.11.6")

	c := New()
	c.GitHubBase = "http://127.0.0.1:1"
	c.CNBBase = baseOf(cnb)
	c.CNBMirror = "laincat/wbtray"

	rel, err := c.Latest(context.Background(), Gateway())
	if err != nil {
		t.Fatal(err)
	}
	var mirrored string
	for _, u := range rel.URLs {
		if strings.HasSuffix(u, Gateway().MirrorAssetName) {
			mirrored = u
		}
	}
	if mirrored == "" {
		t.Fatalf("no address points at the mirrored copy: %v", rel.URLs)
	}
	if !strings.Contains(mirrored, baseOf(cnb)) {
		t.Errorf("the mirrored address is %q, which is not the mirror", mirrored)
	}
}

// TestGatewayKeepsItsOwnAssetName checks that the upstream project is not
// rewritten to carry its archived copy's name: the version is in that name, and
// the mirrored file has a fixed one.
func TestGatewayKeepsItsOwnAssetName(t *testing.T) {
	rel := Release{Source: Gateway(), Version: "v1.11.6"}
	if got := rel.AssetName(); got != "wb2api-panel-v1.11.6-windows-amd64.zip" {
		t.Errorf("AssetName = %q, which does not match what the upstream publishes", got)
	}
}

// TestGatewayCarriesTheMirroredArchive checks that the upstream project, which
// has no cnb.cool repository of its own, still gets a mirror address — the one
// inside wbtray's own release.
func TestGatewayCarriesTheMirroredArchive(t *testing.T) {
	if MirrorAssetName(Gateway()) == "" {
		t.Fatal("the gateway has no mirrored asset name, so it could never be fetched from the mirror")
	}
	if MirrorAssetName(Tray()) != "" {
		t.Error("the tray claims a mirrored copy of itself, which would point at its own archive twice")
	}
}

// MirrorAssetName exposes the source's field for the test above.
func MirrorAssetName(s Source) string { return s.MirrorAssetName }

func sign(v int) int {
	switch {
	case v < 0:
		return -1
	case v > 0:
		return 1
	default:
		return 0
	}
}
