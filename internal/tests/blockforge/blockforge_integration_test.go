//go:build integration

package blockforge_test

import (
	"crypto/sha1"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rannday/blockforge/internal/serverinstaller"
)

func TestInstallAndUpdateFromPortableManifestFixture(t *testing.T) {
	fixtureDir := fixtureDataDir(t)
	manifestPath := patchPortableManifest(t, fixtureDir, t.TempDir())
	installDir := filepath.Join(t.TempDir(), "server")

	if err := serverinstaller.Run([]string{
		"--dir", installDir,
		"--manifest", manifestPath,
		"--workers", "2",
	}); err != nil {
		t.Fatalf("first Run() error = %v", err)
	}

	for _, path := range []string{
		filepath.Join(installDir, ".blockforge", "manifest-url"),
		filepath.Join(installDir, ".blockforge", "manifest-version"),
		filepath.Join(installDir, "config"),
		filepath.Join(installDir, "mods", "ars_nouveau-1.21.1-5.11.5.jar"),
		filepath.Join(installDir, "libraries", "net", "neoforged", "neoforge", "21.1.229"),
		filepath.Join(installDir, "run.sh"),
		filepath.Join(installDir, "run.bat"),
		filepath.Join(installDir, "user_jvm_args.txt"),
	} {
		assertPathExists(t, path)
	}

	version := strings.TrimSpace(readFile(t, filepath.Join(installDir, ".blockforge", "manifest-version")))
	if version != "0.1.15-test" {
		t.Fatalf("manifest-version = %q, want 0.1.15-test", version)
	}
	savedSource := strings.TrimSpace(readFile(t, filepath.Join(installDir, ".blockforge", "manifest-url")))
	if savedSource == "" {
		t.Fatalf("saved manifest source empty")
	}

	if err := serverinstaller.Run([]string{
		"--dir", installDir,
		"--workers", "2",
	}); err != nil {
		t.Fatalf("second Run() error = %v", err)
	}

	for _, path := range []string{
		filepath.Join(installDir, "mods", "ars_nouveau-1.21.1-5.11.5.jar"),
		filepath.Join(installDir, "libraries", "net", "neoforged", "neoforge", "21.1.229"),
		filepath.Join(installDir, "run.sh"),
		filepath.Join(installDir, "run.bat"),
		filepath.Join(installDir, "user_jvm_args.txt"),
	} {
		assertPathExists(t, path)
	}
}

func fixtureDataDir(t *testing.T) string {
	t.Helper()

	dir, err := filepath.Abs(filepath.Join("..", "testsdata"))
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
