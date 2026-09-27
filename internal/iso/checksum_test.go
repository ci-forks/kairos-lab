package iso

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// The published .sha256 names the path the release job hashed, not the asset
// name, so the name column carries an "artifacts/" prefix. Comparing it to the
// ISO name as-is matches nothing and every download would be rejected.
func TestParseSHA256SumAcceptsThePublishedFormat(t *testing.T) {
	const digest = "2d126227e6297682231984bfa0ee8d5084a621967f3c5afe8edeea77fcc46c3f"
	const name = "kairos-hadron-v0.5.1-core-amd64-generic-v4.3.0.iso"

	for _, tc := range []struct {
		what string
		body string
	}{
		{"as published", digest + "  artifacts/" + name + "\n"},
		{"without the path prefix", digest + "  " + name + "\n"},
		{"binary mode marker", digest + " *" + name + "\n"},
		{"uppercase digest", strings.ToUpper(digest) + "  artifacts/" + name + "\n"},
		{"digest alone", digest + "\n"},
		{"several isos in one file", "aa  artifacts/other.iso\n" + digest + "  artifacts/" + name + "\n"},
		{"no trailing newline", digest + "  artifacts/" + name},
	} {
		got, err := parseSHA256Sum(tc.body, name)
		if err != nil {
			t.Fatalf("%s: %v", tc.what, err)
		}
		if got != digest {
			t.Fatalf("%s: got %q, want %q", tc.what, got, digest)
		}
	}
}

// A checksum file that names a different ISO must not be accepted for this
// one: silently taking the first digest would verify the download against the
// wrong artifact, which is worse than not verifying it at all.
func TestParseSHA256SumRejectsAnotherISOsDigest(t *testing.T) {
	const other = "2d126227e6297682231984bfa0ee8d5084a621967f3c5afe8edeea77fcc46c3f"
	body := other + "  artifacts/kairos-hadron-v0.5.1-core-arm64-generic-v4.3.0.iso\n"

	if got, err := parseSHA256Sum(body, "kairos-hadron-v0.5.1-core-amd64-generic-v4.3.0.iso"); err == nil {
		t.Fatalf("accepted another ISO's digest: %q", got)
	}
}

func TestParseSHA256SumRejectsAnUnusableBody(t *testing.T) {
	for _, body := range []string{
		"",
		"404: Not Found\n",
		"<html><body>nope</body></html>\n",
		"zz26227e6297682231984bfa0ee8d5084a621967f3c5afe8edeea77fcc46c3f  artifacts/kairos.iso\n",
		"2d126227e6  artifacts/kairos.iso\n",
	} {
		if got, err := parseSHA256Sum(body, "kairos.iso"); err == nil {
			t.Fatalf("accepted %q as a checksum, returned %q", body, got)
		}
	}
}

func TestVerifyFileSHA256(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kairos.iso")
	body := bytes.Repeat([]byte("A"), 4096)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := verifyFileSHA256(path, "kairos.iso", sha256Hex(body)); err != nil {
		t.Fatalf("matching file rejected: %v", err)
	}

	err := verifyFileSHA256(path, "kairos.iso", sha256Hex([]byte("something else")))
	if err == nil {
		t.Fatal("a file that does not match the digest was accepted")
	}
	// Both digests belong in the message: without them the user cannot tell a
	// corrupt download from a checksum fetched for the wrong ISO.
	if !strings.Contains(err.Error(), sha256Hex(body)) {
		t.Fatalf("error does not name the digest the file has: %v", err)
	}
	if !strings.Contains(err.Error(), "kairos.iso") {
		t.Fatalf("error does not name the ISO: %v", err)
	}
}

// ParseISOAssets has both the ISO and its .sha256 in the same asset list, so
// the checksum URL is known before the download starts rather than guessed.
func TestParseISOAssetsPairsTheChecksumAsset(t *testing.T) {
	const iso = "kairos-hadron-v0.5.1-core-amd64-generic-v4.3.0.iso"
	release := &Release{
		TagName: "v4.3.0",
		Assets: []Asset{
			// The checksum is listed after the ISO in the real release payload.
			{Name: iso, BrowserDownloadURL: "https://example.invalid/" + iso, Size: 42},
			{Name: iso + ".sha256", BrowserDownloadURL: "https://example.invalid/" + iso + ".sha256"},
			{Name: iso + ".sha256.bundle", BrowserDownloadURL: "https://example.invalid/bundle"},
		},
	}

	options := ParseISOAssets(release)
	if len(options) != 1 {
		t.Fatalf("got %d options, want 1: %+v", len(options), options)
	}
	if want := "https://example.invalid/" + iso + ".sha256"; options[0].ChecksumURL != want {
		t.Fatalf("ChecksumURL = %q, want %q", options[0].ChecksumURL, want)
	}
}

func TestFetchChecksum(t *testing.T) {
	const iso = "kairos-hadron-v0.5.1-core-amd64-generic-v4.3.0.iso"
	digest := sha256Hex([]byte("iso"))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(digest + "  artifacts/" + iso + "\n"))
	}))
	defer srv.Close()

	got, err := FetchChecksum(&ISOOption{Name: iso, ChecksumURL: srv.URL + "/" + iso + ".sha256"})
	if err != nil {
		t.Fatalf("FetchChecksum: %v", err)
	}
	if got != digest {
		t.Fatalf("got %q, want %q", got, digest)
	}
}

// An ISO with no published checksum has to stop the download rather than take
// the unverified path, which is what the tool did for every ISO before this.
// The message has to say that much: an empty URL also fails inside the HTTP
// client, and "unsupported protocol scheme" tells the user nothing about what
// the release is missing.
func TestFetchChecksumRefusesAnISOWithNoChecksumAsset(t *testing.T) {
	_, err := FetchChecksum(&ISOOption{Name: "kairos.iso"})
	if err == nil {
		t.Fatal("an ISO with no .sha256 asset was allowed through")
	}
	if !strings.Contains(err.Error(), "kairos.iso") {
		t.Fatalf("error does not name the ISO: %v", err)
	}
	if !strings.Contains(err.Error(), "publishes no .sha256") {
		t.Fatalf("error does not say the release publishes no checksum: %v", err)
	}
}

// The status code is what decides whether a body is a checksum, not whether
// the body happens to parse. An error page that carries a well-formed line
// would otherwise be accepted as the digest for this ISO.
func TestFetchChecksumRejectsANon200(t *testing.T) {
	const iso = "kairos.iso"
	digest := sha256Hex([]byte("whatever the error page says"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(digest + "  artifacts/" + iso + "\n"))
	}))
	defer srv.Close()

	if got, err := FetchChecksum(&ISOOption{Name: iso, ChecksumURL: srv.URL}); err == nil {
		t.Fatalf("a 404 body was taken as a checksum: %q", got)
	}
}
