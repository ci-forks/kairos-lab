package iso

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// The parsing here is deliberately kept apart from the HTTP calls so it can be
// exercised without a network, the same split network_darwin_parse.go uses.

// parseSHA256Sum pulls the digest of isoName out of the body of a `.sha256`
// asset. The published file is the output of `sha256sum`, so a line is
// "<hex>  <path>" and the path is the one the release job hashed rather than
// the name the asset is served under:
//
//	2d126227...c3f  artifacts/kairos-hadron-v0.5.1-core-amd64-generic-v4.3.0.iso
//
// Comparing that column to the ISO name as-is never matches, so the name is
// reduced to its base. A leading "*" on the name marks binary mode and is not
// part of it. A single-column body (some tools emit the digest alone) is taken
// as the digest for the file that was asked for.
func parseSHA256Sum(body, isoName string) (string, error) {
	want := path.Base(isoName)
	for _, line := range strings.Split(body, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		digest, err := normalizeSHA256(fields[0])
		if err != nil {
			continue
		}
		if len(fields) == 1 {
			return digest, nil
		}
		// Only the last field can be the name: sha256sum prints exactly two
		// columns, and a name with a space in it is escaped by the tool.
		name := strings.TrimPrefix(fields[len(fields)-1], "*")
		if path.Base(filepath.ToSlash(name)) == want {
			return digest, nil
		}
	}
	return "", fmt.Errorf("no sha256 line for %s in the published checksum", want)
}

// normalizeSHA256 accepts a 64-character hex digest in any case and returns it
// lowercased, so a comparison never has to care.
func normalizeSHA256(s string) (string, error) {
	if len(s) != sha256.Size*2 {
		return "", fmt.Errorf("not a sha256 digest: %q", s)
	}
	if _, err := hex.DecodeString(s); err != nil {
		return "", fmt.Errorf("not a sha256 digest: %q", s)
	}
	return strings.ToLower(s), nil
}

// fileSHA256 streams the file through the hash rather than reading it, because
// the files this runs on are whole ISOs.
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// verifyFileSHA256 reports whether the file at path hashes to want. name is
// what the file is called for the user: path can be the temporary file the
// download is still writing through, and naming that in an error would point
// at something already gone by the time the error is read. want is expected to
// have been through normalizeSHA256 already.
func verifyFileSHA256(path, name, want string) error {
	got, err := fileSHA256(path)
	if err != nil {
		return fmt.Errorf("hash %s: %w", name, err)
	}
	if got != want {
		return fmt.Errorf("sha256 mismatch for %s: got %s, the release publishes %s", name, got, want)
	}
	return nil
}
