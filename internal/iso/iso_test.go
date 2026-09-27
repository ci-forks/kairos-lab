package iso

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// SelectDownloaded takes no input when exactly one ISO is cached, so without
// an explicit line it attaches that ISO with no output at all. That is the
// branch a dropped `start <path>` argument fell through to in
// kairos-io/kairos#4432, which is why the wrong ISO booted unnoticed.
func TestSelectDownloadedNamesTheOnlyCachedISO(t *testing.T) {
	dir := t.TempDir()
	only := filepath.Join(dir, "kairos-ubuntu-24.04-core-arm64-generic-v1.0.0.iso")
	if err := os.WriteFile(only, []byte("iso"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	got, err := SelectDownloaded(SelectConfig{
		DownloadsDir: dir,
		Stdin:        strings.NewReader(""),
		Stdout:       &stdout,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != only {
		t.Fatalf("got %q, want %q", got, only)
	}
	if !strings.Contains(stdout.String(), filepath.Base(only)) {
		t.Fatalf("the selected ISO was not named on stdout, got %q", stdout.String())
	}
}

// With more than one cached ISO the user picks, so the choice is already
// visible and the auto-select line must not appear.
func TestSelectDownloadedPromptsWhenSeveralAreCached(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"kairos-a.iso", "kairos-b.iso"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("iso"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var stdout bytes.Buffer
	got, err := SelectDownloaded(SelectConfig{
		DownloadsDir: dir,
		Stdin:        strings.NewReader("2\n"),
		Stdout:       &stdout,
	})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "kairos-b.iso" {
		t.Fatalf("got %q, want kairos-b.iso", got)
	}
	if strings.Contains(stdout.String(), "only downloaded ISO") {
		t.Fatalf("auto-select line printed on the prompting path, got %q", stdout.String())
	}
}

// truncatingServer answers with the announced length in the header but hangs
// up after sending part of the body, which is what a dropped connection or a
// Ctrl-C mid-transfer looks like to the client.
func truncatingServer(t *testing.T, full []byte, sendFirst int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(full)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(full[:sendFirst])
		w.(http.Flusher).Flush()
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		_ = conn.Close()
	}))
}

// The cache check in downloadISO is a bare os.Stat, so anything left at the
// target path is served as a complete ISO forever after. An interrupted
// download must therefore leave nothing there.
func TestInterruptedDownloadLeavesNoCachedISO(t *testing.T) {
	full := bytes.Repeat([]byte("A"), 4096)
	srv := truncatingServer(t, full, 1024)
	defer srv.Close()

	dir := t.TempDir()
	var out bytes.Buffer
	if _, err := downloadISO(srv.URL+"/kairos.iso", dir, &out); err == nil {
		t.Fatal("interrupted download reported success")
	}

	target := filepath.Join(dir, "kairos.iso")
	if info, err := os.Stat(target); err == nil {
		t.Fatalf("left a %d byte file at %s; the next run would report it as cached", info.Size(), target)
	}

	isos, err := ListDownloaded(dir)
	if err != nil {
		t.Fatalf("ListDownloaded: %v", err)
	}
	if len(isos) != 0 {
		t.Fatalf("ListDownloaded offers %v after a failed download", isos)
	}

	// The partial bytes must not survive under any name either, or a retried
	// download leaves one dead file per attempt in the downloads directory.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 0 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("downloads directory holds %v after a failed download", names)
	}
}

// The retry after an interrupted download has to fetch the ISO again rather
// than report the leftover as cached.
func TestDownloadAfterAnInterruptedOneRefetches(t *testing.T) {
	full := bytes.Repeat([]byte("A"), 4096)
	dir := t.TempDir()

	bad := truncatingServer(t, full, 1024)
	var out bytes.Buffer
	if _, err := downloadISO(bad.URL+"/kairos.iso", dir, &out); err == nil {
		t.Fatal("interrupted download reported success")
	}
	bad.Close()

	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(full)
	}))
	defer good.Close()

	out.Reset()
	path, err := downloadISO(good.URL+"/kairos.iso", dir, &out)
	if err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	if strings.Contains(out.String(), "Using cached ISO") {
		t.Fatalf("retry reported the truncated file as cached: %q", out.String())
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read iso: %v", err)
	}
	if !bytes.Equal(got, full) {
		t.Fatalf("iso is %d bytes, want %d", len(got), len(full))
	}
}

// A completed download lands at the target path under its own name, and the
// temporary file it was written through is gone.
func TestCompletedDownloadLeavesOnlyTheISO(t *testing.T) {
	full := bytes.Repeat([]byte("A"), 4096)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(full)
	}))
	defer srv.Close()

	dir := t.TempDir()
	var out bytes.Buffer
	path, err := downloadISO(srv.URL+"/kairos.iso", dir, &out)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if want := filepath.Join(dir, "kairos.iso"); path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "kairos.iso" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("downloads directory holds %v, want only kairos.iso", names)
	}

	// The second call must now take the cache path.
	out.Reset()
	if _, err := downloadISO(srv.URL+"/kairos.iso", dir, &out); err != nil {
		t.Fatalf("cached download: %v", err)
	}
	if !strings.Contains(out.String(), "Using cached ISO") {
		t.Fatalf("a complete ISO was not served from the cache: %q", out.String())
	}
}
