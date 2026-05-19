package serverinstaller

import (
	"crypto/sha1"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testConfigSHA1 = "813f2cc7a417af4cea8f5eeb7c88752cee018374"

func TestRootManifestFixtureValidatesWithPortableConfigURL(t *testing.T) {
	root := repoRootFromServerInstallerTest(t)
	manifestPath := patchPortableManifestFixture(t, root, t.TempDir())

	manifest, err := FetchRemoteManifest(pathToFileURL(manifestPath))
	if err != nil {
		t.Fatalf("FetchRemoteManifest() error = %v", err)
	}
	if err := manifest.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if manifest.SchemaVersion != 1 {
		t.Fatalf("schema_version = %d, want 1", manifest.SchemaVersion)
	}
	if manifest.Pack != "blockforge-test" {
		t.Fatalf("pack = %q, want blockforge-test", manifest.Pack)
	}
	if manifest.Version != "0.1.15-test" {
		t.Fatalf("version = %q, want 0.1.15-test", manifest.Version)
	}
	if manifest.Minecraft != "1.21.1" {
		t.Fatalf("minecraft = %q, want 1.21.1", manifest.Minecraft)
	}
	if manifest.Loader.Type != "neoforge" {
		t.Fatalf("loader.type = %q, want neoforge", manifest.Loader.Type)
	}
	if manifest.Loader.Version != "21.1.229" {
		t.Fatalf("loader.version = %q, want 21.1.229", manifest.Loader.Version)
	}
	if len(manifest.Mods) != 1 {
		t.Fatalf("mods length = %d, want 1", len(manifest.Mods))
	}
	if manifest.Mods[0].Name != "Ars Nouveau" {
		t.Fatalf("mods[0].name = %q, want Ars Nouveau", manifest.Mods[0].Name)
	}
	if manifest.ServerConfig == nil {
		t.Fatalf("server_config missing")
	}
	parsed, err := url.Parse(manifest.ServerConfig.URL)
	if err != nil {
		t.Fatalf("parse server_config.url: %v", err)
	}
	if parsed.Scheme != "file" {
		t.Fatalf("server_config.url scheme = %q, want file", parsed.Scheme)
	}
}

func TestRootConfigZipFixtureSHA1(t *testing.T) {
	root := repoRootFromServerInstallerTest(t)
	configPath := filepath.Join(root, "testdata", "blockforge-test-config-0.1.15.zip")

	assertPathExists(t, configPath)
	assertFileSHA1(t, configPath, testConfigSHA1)
}

func repoRootFromServerInstallerTest(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func patchPortableManifestFixture(t *testing.T, root, dstDir string) string {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join(root, "testdata", "test-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	testdataURL := fileURLFromPath(filepath.Join(root, "testdata")) + "/"
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
