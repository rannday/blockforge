package blockforge_test

import (
	"crypto/sha1"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureDataDir(t *testing.T) string {
	t.Helper()

	dir, err := filepath.Abs(filepath.Join("..", "testsdata"))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func integrationTempRoot() (string, error) {
	return filepath.Abs(filepath.Join("..", "..", "..", "tmp", "int_test"))
}

func integrationTempDir(t *testing.T, suite, prefix string) string {
	t.Helper()

	root, err := integrationTempRoot()
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Join(root, suite)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(root, prefix+"-")
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func patchPortableManifest(t *testing.T, fixtureDir, dstDir string) string {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join(fixtureDir, "test-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	testdataURL := fileURLFromPath(fixtureDir) + "/"
	patched := strings.ReplaceAll(string(raw), "file://TESTDATA/", testdataURL)
	if strings.Contains(patched, "file://TESTDATA/") {
		t.Fatalf("portable TESTDATA placeholder was not patched")
	}

	dst := filepath.Join(dstDir, "test-manifest.json")
	if err := os.WriteFile(dst, []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}
	return dst
}

func fileURLFromPath(path string) string {
	slashes := filepath.ToSlash(path)
	if len(slashes) >= 2 && slashes[1] == ':' {
		slashes = "/" + slashes
	}
	return (&url.URL{Scheme: "file", Path: slashes}).String()
}

func assertPathExists(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected path %s: %v", path, err)
	}
}

func assertFileSHA1(t *testing.T, path, expected string) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha1.Sum(data)
	got := hex.EncodeToString(sum[:])
	if got != expected {
		t.Fatalf("SHA1(%s) = %s, want %s", path, got, expected)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
