package serverinstaller

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestParseJavaVersion(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		output  string
		want    int
		wantErr bool
	}{
		{
			name:   "modern",
			output: `openjdk version "21.0.2" 2024-01-16`,
			want:   21,
		},
		{
			name:   "seventeen",
			output: `openjdk version "17.0.10" 2024-01-16`,
			want:   17,
		},
		{
			name:   "legacy",
			output: `java version "1.8.0_402"`,
			want:   8,
		},
		{
			name:    "invalid",
			output:  `java version`,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseJavaVersion(tc.output)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseJavaVersion() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("ParseJavaVersion() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestInferFilenameFromURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "https",
			raw:  "https://example.invalid/mods/example.jar",
			want: "example.jar",
		},
		{
			name: "file",
			raw:  "file:///tmp/Ars%20Fauna-1.4.1.jar",
			want: "Ars Fauna-1.4.1.jar",
		},
		{
			name: "spaces",
			raw:  "https://example.invalid/mods/Example%20Mod-1.0.0.jar",
			want: "Example Mod-1.0.0.jar",
		},
		{
			name: "plus",
			raw:  "https://edge.forgecdn.net/files/7515/858/comforts-neoforge-9.0.5%2b1.21.1.jar",
			want: "comforts-neoforge-9.0.5+1.21.1.jar",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := inferFilenameFromURL(tc.raw)
			if err != nil {
				t.Fatalf("inferFilenameFromURL() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("inferFilenameFromURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestInferFilenameFromURLRejectsUnsafeDecodedNames(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
	}{
		{
			name: "traversal",
			raw:  "https://example.invalid/mods/%2e%2e%2fevil.jar",
		},
		{
			name: "slash",
			raw:  "https://example.invalid/mods/%2fevil.jar",
		},
		{
			name: "backslash",
			raw:  "https://example.invalid/mods/%5cevil.jar",
		},
		{
			name: "non-jar",
			raw:  "https://example.invalid/mods/example.zip",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := inferFilenameFromURL(tc.raw)
			if err != nil {
				t.Fatalf("inferFilenameFromURL() error = %v", err)
			}
			if isSafeModFilename(got) {
				t.Fatalf("isSafeModFilename(%q) = true, want false", got)
			}
		})
	}
}

func TestLoadManifestFromBytes(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
  "schema_version": 1,
  "pack": "varda",
  "version": "0.1.4",
  "minecraft": "1.21.1",
  "java": {
    "major": 21
  },
  "loader": {
    "type": "neoforge",
    "version": "21.1.228",
    "installer_url": "https://maven.neoforged.net/releases/net/neoforged/neoforge/21.1.228/neoforge-21.1.228-installer.jar",
    "sha1": "0123456789abcdef0123456789abcdef01234567"
  },
  "server_config": {
    "url": "https://example.invalid/server-config.zip",
    "sha1": "0123456789abcdef0123456789abcdef01234567"
  },
  "mods": [
    {
      "name": "Example Mod",
      "url": "https://example.invalid/example.jar",
      "website_url": "https://example.invalid",
      "sha1": "0123456789abcdef0123456789abcdef01234567",
      "size": 1234
    }
  ]
}`)

	manifest, err := LoadManifestFromBytes(raw)
	if err != nil {
		t.Fatalf("LoadManifestFromBytes() error = %v", err)
	}
	if manifest.Pack != "varda" || manifest.Version != "0.1.4" || manifest.Loader.InstallerURL == "" {
		t.Fatalf("LoadManifestFromBytes() = %#v", manifest)
	}
	if manifest.Loader.Type != "neoforge" || manifest.Loader.Version != "21.1.228" || manifest.Loader.SHA1 != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("LoadManifestFromBytes() loader = %#v", manifest.Loader)
	}
	if manifest.Java.Major != 21 {
		t.Fatalf("LoadManifestFromBytes() java = %#v", manifest.Java)
	}
	if len(manifest.Mods) != 1 || manifest.Mods[0].URL != "https://example.invalid/example.jar" {
		t.Fatalf("LoadManifestFromBytes() mods = %#v", manifest.Mods)
	}
	if got := manifest.Version; got != "0.1.4" {
		t.Fatalf("Version = %q, want %q", got, "0.1.4")
	}
}

func TestManifestValidateAcceptsSimplifiedManifest(t *testing.T) {
	t.Parallel()

	manifest := validTestManifest()

	if err := manifest.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestManifestValidateRejectsMissingJavaMetadata(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
	}{
		{
			name: "missing root java",
			raw:  strings.Replace(validManifestJSON(), "  \"java\": {\n    \"major\": 21\n  },\n", "", 1),
		},
		{
			name: "missing java major",
			raw:  strings.Replace(validManifestJSON(), "  \"java\": {\n    \"major\": 21\n  },\n", "  \"java\": {},\n", 1),
		},
		{
			name: "below minimum",
			raw:  strings.Replace(validManifestJSON(), `"major": 21`, `"major": 7`, 1),
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			manifest, err := LoadManifestFromBytes([]byte(tc.raw))
			if err != nil {
				t.Fatalf("LoadManifestFromBytes() error = %v", err)
			}
			if err := manifest.Validate(); err == nil || !strings.Contains(strings.ToLower(err.Error()), "manifest java.major") {
				t.Fatalf("Validate() error = %v, want java.major rejection", err)
			}
		})
	}
}

func TestLoadManifestFromBytesRejectsInvalidJavaType(t *testing.T) {
	t.Parallel()

	raw := strings.Replace(validManifestJSON(), `"major": 21`, `"major": "21"`, 1)
	if _, err := LoadManifestFromBytes([]byte(raw)); err == nil || !strings.Contains(strings.ToLower(err.Error()), "decode manifest") {
		t.Fatalf("LoadManifestFromBytes() error = %v, want decode rejection", err)
	}
}

func TestManifestValidateRejectsMissingLoaderVersion(t *testing.T) {
	t.Parallel()

	manifest := validTestManifest()
	manifest.Loader.Version = ""

	if err := manifest.Validate(); err == nil || !strings.Contains(strings.ToLower(err.Error()), "loader.version") {
		t.Fatalf("Validate() error = %v, want version rejection", err)
	}
}

func TestManifestValidateRejectsInvalidLoaderSHA1(t *testing.T) {
	t.Parallel()

	manifest := validTestManifest()
	manifest.Loader.SHA1 = "bad"

	if err := manifest.Validate(); err == nil || !strings.Contains(strings.ToLower(err.Error()), "loader.sha1") {
		t.Fatalf("Validate() error = %v, want sha1 rejection", err)
	}
}

func TestManifestValidateRejectsMalformedLoaderURL(t *testing.T) {
	t.Parallel()

	manifest := validTestManifest()
	manifest.Loader.InstallerURL = "https://"

	if err := manifest.Validate(); err == nil || !strings.Contains(strings.ToLower(err.Error()), "host") {
		t.Fatalf("Validate() error = %v, want host rejection", err)
	}
}

func TestManifestValidateRejectsMissingLoaderHost(t *testing.T) {
	t.Parallel()

	manifest := validTestManifest()
	manifest.Loader.InstallerURL = "https:///neoforge-21.1.228-installer.jar"

	if err := manifest.Validate(); err == nil || !strings.Contains(strings.ToLower(err.Error()), "host") {
		t.Fatalf("Validate() error = %v, want missing host rejection", err)
	}
}

func TestManifestValidateRejectsUnsupportedLoaderType(t *testing.T) {
	t.Parallel()

	manifest := validTestManifest()
	manifest.Loader.Type = "badloader"

	if err := manifest.Validate(); err == nil || !strings.Contains(strings.ToLower(err.Error()), "loader.type") {
		t.Fatalf("Validate() error = %v, want loader type rejection", err)
	}
}

func TestManifestValidateAcceptsSchemaRecognizedLoaderTypes(t *testing.T) {
	t.Parallel()

	for _, loaderType := range []string{"neoforge", "forge", "fabric", "quilt"} {
		loaderType := loaderType
		t.Run(loaderType, func(t *testing.T) {
			t.Parallel()

			manifest := validTestManifest()
			manifest.Loader.Type = loaderType

			if err := manifest.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestLoadManifestFromBytesRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
	}{
		{
			name: "top-level",
			raw:  strings.Replace(validManifestJSON(), `"mods": [`, `"unexpected": true, "mods": [`, 1),
		},
		{
			name: "loader",
			raw:  strings.Replace(validManifestJSON(), `"installer_url":`, `"extra": true, "installer_url":`, 1),
		},
		{
			name: "server_config",
			raw:  strings.Replace(validManifestJSON(), `"server_config": {`, `"server_config": {"extra": true,`, 1),
		},
		{
			name: "mod",
			raw:  strings.Replace(validManifestJSON(), `"website_url":`, `"extra": true, "website_url":`, 1),
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if _, err := LoadManifestFromBytes([]byte(tc.raw)); err == nil || !strings.Contains(strings.ToLower(err.Error()), "unknown field") {
				t.Fatalf("LoadManifestFromBytes() error = %v, want unknown field", err)
			}
		})
	}
}

func TestManifestValidateRejectsUnsafeModFilename(t *testing.T) {
	t.Parallel()

	manifest := validTestManifest()
	manifest.Mods = []ManifestMod{{Name: "Example Mod", URL: "https://example.invalid/evil%5c.jar", WebsiteURL: "https://example.invalid", SHA1: strings.Repeat("1", 40), Size: 1}}

	if err := manifest.Validate(); err == nil || !strings.Contains(strings.ToLower(err.Error()), "unsafe") {
		t.Fatalf("Validate() error = %v, want unsafe filename rejection", err)
	}
}

func TestManifestValidateRejectsNonJarModFilename(t *testing.T) {
	t.Parallel()

	manifest := validTestManifest()
	manifest.Mods = []ManifestMod{{Name: "Example Mod", URL: "https://example.invalid/example.zip", WebsiteURL: "https://example.invalid", SHA1: strings.Repeat("1", 40), Size: 1}}

	if err := manifest.Validate(); err == nil || !strings.Contains(strings.ToLower(err.Error()), "non-jar") {
		t.Fatalf("Validate() error = %v, want non-jar rejection", err)
	}
}

func TestManifestValidateRejectsMalformedModURL(t *testing.T) {
	t.Parallel()

	manifest := validTestManifest()
	manifest.Mods = []ManifestMod{{Name: "Example Mod", URL: "https://", WebsiteURL: "https://example.invalid", SHA1: strings.Repeat("1", 40), Size: 1}}

	if err := manifest.Validate(); err == nil || !strings.Contains(strings.ToLower(err.Error()), "host") {
		t.Fatalf("Validate() error = %v, want malformed URL rejection", err)
	}
}

func TestManifestValidateRejectsSchemaVersion2(t *testing.T) {
	t.Parallel()

	manifest := validTestManifest()
	manifest.SchemaVersion = 2

	if err := manifest.Validate(); err == nil || !strings.Contains(strings.ToLower(err.Error()), "schema") {
		t.Fatalf("Validate() error = %v, want schema rejection", err)
	}
}

func TestManifestValidateRejectsInvalidModSHA1(t *testing.T) {
	t.Parallel()

	manifest := validTestManifest()
	manifest.Mods[0].SHA1 = "not-a-sha1"

	if err := manifest.Validate(); err == nil || !strings.Contains(strings.ToLower(err.Error()), "sha1") {
		t.Fatalf("Validate() error = %v, want sha1 rejection", err)
	}
}

func TestManifestValidateRejectsInvalidServerConfigSHA1(t *testing.T) {
	t.Parallel()

	manifest := validTestManifest()
	manifest.ServerConfig = &ServerConfigManifest{
		URL:  "https://example.invalid/server-config.zip",
		SHA1: "bad",
	}

	if err := manifest.Validate(); err == nil || !strings.Contains(strings.ToLower(err.Error()), "server_config.sha1") {
		t.Fatalf("Validate() error = %v, want server_config sha1 rejection", err)
	}
}

func TestManifestValidateAcceptsLoaderSHA1(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join("..", "manifest", "testdata", "valid", "manifest.v1.json"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	manifest, err := LoadManifestFromBytes([]byte(strings.Replace(string(raw), `"sha1": "0123456789abcdef0123456789abcdef01234567"`, `"sha1": "  0123456789abcdef0123456789abcdef01234567  "`, 1)))
	if err != nil {
		t.Fatalf("LoadManifestFromBytes() error = %v", err)
	}

	if err := manifest.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if manifest.Loader.SHA1 != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("normalize() = %q", manifest.Loader.SHA1)
	}
}

func TestWriteInstallDiagnosticsWritesOnlyDiagnostics(t *testing.T) {
	root := t.TempDir()
	oldVersion := Version
	Version = "test-123"
	defer func() { Version = oldVersion }()

	manifest := validTestManifest()
	if err := WriteInstallDiagnostics(root, manifest); err != nil {
		t.Fatalf("WriteInstallDiagnostics() error = %v", err)
	}

	for _, path := range []string{
		filepath.Join(root, ".blockforge", "pack-version.txt"),
		filepath.Join(root, ".blockforge", "java-major-version"),
		filepath.Join(root, ".blockforge", "installer-version.txt"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected diagnostic file %s: %v", path, err)
		}
	}
	for _, path := range []string{
		filepath.Join(root, ".blockforge", "manifest.json"),
		filepath.Join(root, ".blockforge", "mods-list.txt"),
		filepath.Join(root, ".blockforge", "neoforge-url.txt"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("unexpected persisted file %s: %v", path, err)
		}
	}
	javaMajor, err := os.ReadFile(filepath.Join(root, ".blockforge", "java-major-version"))
	if err != nil {
		t.Fatalf("ReadFile(java-major-version) error = %v", err)
	}
	if strings.TrimSpace(string(javaMajor)) != "21" {
		t.Fatalf("java-major-version = %q, want 21", javaMajor)
	}
}

func TestResolveManifestSourceUsesCLIAndSavedSource(t *testing.T) {
	root := t.TempDir()
	savedURL := "https://example.invalid/old.json"
	cliURL := "https://example.invalid/new.json"

	if err := saveManifestURL(root, savedURL); err != nil {
		t.Fatal(err)
	}

	got, err := resolveManifestSource(root, "")
	if err != nil {
		t.Fatalf("resolveManifestSource() error = %v", err)
	}
	if got != savedURL {
		t.Fatalf("resolveManifestSource() = %q, want saved source", got)
	}

	got, err = resolveManifestSource(root, cliURL)
	if err != nil {
		t.Fatalf("resolveManifestSource() CLI error = %v", err)
	}
	if got != cliURL {
		t.Fatalf("resolveManifestSource() = %q, want CLI source", got)
	}

	if err := saveManifestURL(root, cliURL); err != nil {
		t.Fatal(err)
	}
	got, err = resolveManifestSource(root, "")
	if err != nil {
		t.Fatalf("resolveManifestSource() updated error = %v", err)
	}
	if got != cliURL {
		t.Fatalf("resolveManifestSource() = %q, want updated saved source", got)
	}
}

func TestResolveManifestSourceRequiresSourceOnFirstInstall(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "missing")
	if _, err := resolveManifestSource(root, ""); err == nil || !strings.Contains(err.Error(), "manifest source required on first install; pass --manifest SOURCE") {
		t.Fatalf("resolveManifestSource() error = %v, want first-install error", err)
	}
}

func TestNormalizeManifestSource(t *testing.T) {
	t.Parallel()

	relative := filepath.Join("testdata", "manifest.json")
	relativeAbs, err := filepath.Abs(relative)
	if err != nil {
		t.Fatal(err)
	}
	absolutePath := filepath.Join(t.TempDir(), "blockforge-manifest.json")

	cases := []struct {
		name    string
		raw     string
		want    string
		wantErr string
	}{
		{
			name: "relative path",
			raw:  relative,
			want: pathToFileURL(relativeAbs),
		},
		{
			name: "absolute path",
			raw:  absolutePath,
			want: pathToFileURL(absolutePath),
		},
		{
			name: "file URL",
			raw:  "file:///tmp/blockforge-manifest.json",
			want: "file:///tmp/blockforge-manifest.json",
		},
		{
			name: "https URL",
			raw:  "https://example.invalid/manifest.json",
			want: "https://example.invalid/manifest.json",
		},
		{
			name:    "unsupported scheme",
			raw:     "ftp://example.invalid/manifest.json",
			wantErr: "unsupported manifest source scheme: ftp",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := normalizeManifestSource(tc.raw)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("normalizeManifestSource() error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeManifestSource() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("normalizeManifestSource() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNormalizeManifestSourceUnixAbsolutePath(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Unix absolute path normalization is runtime-specific")
	}

	got, err := normalizeManifestSource("/tmp/blockforge-manifest.json")
	if err != nil {
		t.Fatalf("normalizeManifestSource() error = %v", err)
	}
	want := "file:///tmp/blockforge-manifest.json"
	if got != want {
		t.Fatalf("normalizeManifestSource() = %q, want %q", got, want)
	}
}

func TestNormalizeManifestSourceWindowsDrivePath(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "windows" {
		t.Skip("Windows drive path normalization is runtime-specific")
	}

	got, err := normalizeManifestSource(`C:\Users\rannd\Downloads\varda-test-ars-manifest.json`)
	if err != nil {
		t.Fatalf("normalizeManifestSource() error = %v", err)
	}
	want := "file:///C:/Users/rannd/Downloads/varda-test-ars-manifest.json"
	if got != want {
		t.Fatalf("normalizeManifestSource() = %q, want %q", got, want)
	}
}

func TestSavedManifestVersionHelpersAndDecision(t *testing.T) {
	root := t.TempDir()

	got, err := readSavedManifestVersion(root)
	if err != nil {
		t.Fatalf("readSavedManifestVersion() error = %v", err)
	}
	if got != "" {
		t.Fatalf("readSavedManifestVersion() = %q, want empty", got)
	}
	if !shouldApplyServerConfig(false, got, "1.0.0") {
		t.Fatalf("first install should apply server config")
	}

	if err := writeSavedManifestVersion(root, "1.0.0"); err != nil {
		t.Fatal(err)
	}
	got, err = readSavedManifestVersion(root)
	if err != nil {
		t.Fatalf("readSavedManifestVersion() error = %v", err)
	}
	if shouldApplyServerConfig(false, got, "1.0.0") {
		t.Fatalf("same version should skip server config")
	}
	if !shouldApplyServerConfig(false, got, "1.0.1") {
		t.Fatalf("changed version should apply server config")
	}
	if !shouldApplyServerConfig(true, got, "1.0.0") {
		t.Fatalf("--force should apply server config")
	}
}

func TestCheckManifestDoesNotRequireTargetDirOrCreateIt(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing-target")
	manifestPath := filepath.Join(t.TempDir(), "manifest.json")
	raw, err := os.ReadFile(filepath.Join("..", "manifest", "testdata", "valid", "manifest.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Run([]string{"--dir", root, "--manifest", manifestPath, "--check-manifest"}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("check mode created target dir: %v", err)
	}
}

func TestCheckAcceptsSimplifiedManifest(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing-target")
	manifestPath := filepath.Join(t.TempDir(), "manifest.json")
	raw, err := os.ReadFile(filepath.Join("..", "manifest", "testdata", "valid", "manifest.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Run([]string{"--dir", root, "-m", manifestPath, "-c"}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestCheckManifestRejectsUnimplementedLoader(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing-target")
	manifestPath := filepath.Join(t.TempDir(), "manifest.json")
	raw := strings.Replace(validManifestJSON(), `"type": "neoforge"`, `"type": "forge"`, 1)
	if err := os.WriteFile(manifestPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	err := Run([]string{"--dir", root, "--manifest", manifestPath, "--check-manifest"})
	if err == nil || !strings.Contains(err.Error(), "recognized by the manifest schema but is not implemented") {
		t.Fatalf("Run() error = %v, want not implemented", err)
	}

	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("check mode created target dir: %v", err)
	}
}

func TestDryRunDoesNotRequireTargetDirOrCreateIt(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing-target")
	manifestPath := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(manifestPath, []byte(validManifestJSON()), 0o644); err != nil {
		t.Fatal(err)
	}

	output := captureStdout(t, func() {
		if err := Run([]string{"--dir", root, "--manifest", manifestPath, "--dry-run"}); err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	})

	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("dry run created target dir: %v", err)
	}
	for _, text := range []string{
		"Dry run:",
		"java.major:",
		"mods download:   1",
		"Dry run complete. No files changed.",
	} {
		if !strings.Contains(output, text) {
			t.Fatalf("dry-run output missing %q:\n%s", text, output)
		}
	}
}

func TestDryRunPlansManagedModsWithoutMutation(t *testing.T) {
	root := t.TempDir()
	modsDirPath := filepath.Join(root, "mods")
	if err := os.MkdirAll(modsDirPath, 0o755); err != nil {
		t.Fatal(err)
	}

	current := []byte("current")
	if err := os.WriteFile(filepath.Join(modsDirPath, "current.jar"), current, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modsDirPath, "replace.jar"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modsDirPath, "extra.jar"), []byte("extra"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(modsDirPath, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}

	manifest := validTestManifest()
	manifest.Mods = []ManifestMod{
		{Name: "Current Mod", URL: "https://example.invalid/current.jar", WebsiteURL: "https://example.invalid/current", SHA1: sha1HexOfBytes(current), Size: int64(len(current))},
		{Name: "Replace Mod", URL: "https://example.invalid/replace.jar", WebsiteURL: "https://example.invalid/replace", SHA1: strings.Repeat("4", 40), Size: 999},
	}

	plan, err := PlanDryRun(root, "file:///manifest.json", manifest, false)
	if err != nil {
		t.Fatalf("PlanDryRun() error = %v", err)
	}
	if len(plan.ModsToKeep) != 1 || plan.ModsToKeep[0].FileName != "current.jar" {
		t.Fatalf("ModsToKeep = %#v, want current.jar", plan.ModsToKeep)
	}
	if len(plan.ModsToDownload) != 1 || plan.ModsToDownload[0].FileName != "replace.jar" {
		t.Fatalf("ModsToDownload = %#v, want replace.jar", plan.ModsToDownload)
	}
	if len(plan.UnmanagedModPaths) != 2 {
		t.Fatalf("UnmanagedModPaths = %#v, want 2 removals", plan.UnmanagedModPaths)
	}
	if _, err := os.Stat(filepath.Join(modsDirPath, "extra.jar")); err != nil {
		t.Fatalf("dry run removed unmanaged file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(modsDirPath, "nested")); err != nil {
		t.Fatalf("dry run removed unmanaged dir: %v", err)
	}
}

func TestDryRunReportsDesiredModDirectoryError(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "mods", "example.jar"), 0o755); err != nil {
		t.Fatal(err)
	}

	manifest := validTestManifest()
	if err := manifest.Validate(); err != nil {
		t.Fatal(err)
	}
	_, err := PlanDryRun(root, "file:///manifest.json", manifest, false)
	if err == nil || !strings.Contains(err.Error(), "target path is a directory") {
		t.Fatalf("PlanDryRun() error = %v, want directory error", err)
	}
}

func TestPlanNeoForgeLoaderActions(t *testing.T) {
	t.Parallel()

	desired := LoaderManifest{
		Type:         "neoforge",
		Version:      "21.1.228",
		InstallerURL: "https://example.com/neoforge-21.1.228-installer.jar",
		SHA1:         "0000000000000000000000000000000000000000",
	}

	cases := []struct {
		name       string
		installed  []string
		force      bool
		wantAction string
		wantOld    []string
	}{
		{
			name:       "force fresh install reports install",
			force:      true,
			wantAction: "install",
		},
		{
			name:       "desired installed skips without force",
			installed:  []string{"21.1.228"},
			wantAction: "skip",
		},
		{
			name:       "desired installed reinstalls with force",
			installed:  []string{"21.1.228"},
			force:      true,
			wantAction: "reinstall",
		},
		{
			name:       "old installed version updates without force",
			installed:  []string{"21.1.227"},
			wantAction: "update",
			wantOld:    []string{"21.1.227"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			for _, version := range tc.installed {
				if err := os.MkdirAll(installedNeoForgeVersionDir(root, version), 0o755); err != nil {
					t.Fatal(err)
				}
			}

			action, oldVersions, err := PlanNeoForgeLoader(root, desired, tc.force)
			if err != nil {
				t.Fatalf("PlanNeoForgeLoader() error = %v", err)
			}
			if action != tc.wantAction {
				t.Fatalf("action = %q, want %q", action, tc.wantAction)
			}
			if !slices.Equal(oldVersions, tc.wantOld) {
				t.Fatalf("oldVersions = %v, want %v", oldVersions, tc.wantOld)
			}
		})
	}
}

func TestManifestJSONTagsKeepRequiredFields(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(Manifest{})
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	for _, field := range []string{`"version"`, `"minecraft"`, `"java"`, `"loader"`, `"server_config"`, `"mods"`} {
		if !strings.Contains(got, field) {
			t.Fatalf("Marshal(Manifest{}) = %s, missing %s", got, field)
		}
	}

	raw, err = json.Marshal(ManifestMod{})
	if err != nil {
		t.Fatal(err)
	}
	got = string(raw)
	for _, field := range []string{`"name"`, `"url"`, `"website_url"`, `"sha1"`, `"size"`} {
		if !strings.Contains(got, field) {
			t.Fatalf("Marshal(ManifestMod{}) = %s, missing %s", got, field)
		}
	}
}

func TestRequireManifestJavaUsesRequestedMajor(t *testing.T) {
	t.Parallel()

	var gotJavaPath string
	deps := runtimeDeps{
		lookPath: func(javaPath string) (string, error) {
			gotJavaPath = javaPath
			return javaPath, nil
		},
		command: func(name string, args ...string) *exec.Cmd {
			if name != "custom-java" {
				t.Fatalf("command name = %q, want custom-java", name)
			}
			if len(args) != 1 || args[0] != "-version" {
				t.Fatalf("command args = %v, want -version", args)
			}
			return helperJavaCommand(t, `openjdk version "21.0.2"`)
		},
	}

	if err := requireJavaWithDeps(deps, "custom-java", 17); err != nil {
		t.Fatalf("requireManifestJava() error = %v", err)
	}
	if gotJavaPath != "custom-java" {
		t.Fatalf("requireJavaWithDeps() path = %q, want custom-java", gotJavaPath)
	}
}

func TestManifestCheckSummaryIncludesJavaMajor(t *testing.T) {
	t.Parallel()

	summary := manifestCheckSummary(validTestManifest())
	if !strings.Contains(summary, "java.major:") {
		t.Fatalf("manifestCheckSummary() missing java.major:\n%s", summary)
	}
}

func TestParseOptionsSupportsAliases(t *testing.T) {
	t.Parallel()

	opts, err := parseOptions([]string{"-d", "srv", "-f", "-v", "-c", "-m", "https://example.invalid/manifest.json", "-w", "3"})
	if err != nil {
		t.Fatalf("parseOptions() error = %v", err)
	}
	if opts.TargetDir != "srv" {
		t.Fatalf("parseOptions() TargetDir = %q, want %q", opts.TargetDir, "srv")
	}
	if !opts.Force {
		t.Fatalf("parseOptions() Force = false, want true")
	}
	if !opts.VersionOnly {
		t.Fatalf("parseOptions() VersionOnly = false, want true")
	}
	if !opts.CheckManifest {
		t.Fatalf("parseOptions() CheckManifest = false, want true")
	}
	if opts.ManifestSource != "https://example.invalid/manifest.json" {
		t.Fatalf("parseOptions() ManifestSource = %q, want %q", opts.ManifestSource, "https://example.invalid/manifest.json")
	}
	if opts.DownloadWorkers != 3 {
		t.Fatalf("parseOptions() DownloadWorkers = %d, want 3", opts.DownloadWorkers)
	}
}

func TestParseOptionsRejectsDeprecatedAliases(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"--manifest-url", "https://example.invalid/manifest.json"},
		{"--download-workers", "4"},
	} {
		if _, err := parseOptions(args); err == nil {
			t.Fatalf("parseOptions(%v) accepted deprecated alias", args)
		}
	}
}

func TestParseOptionsSupportsManifestAndWorkersLongFlags(t *testing.T) {
	t.Parallel()

	opts, err := parseOptions([]string{"--manifest", "https://example.invalid/manifest.json", "--workers", "5"})
	if err != nil {
		t.Fatalf("parseOptions() error = %v", err)
	}
	if opts.ManifestSource != "https://example.invalid/manifest.json" {
		t.Fatalf("parseOptions() ManifestSource = %q, want manifest source", opts.ManifestSource)
	}
	if opts.DownloadWorkers != 5 {
		t.Fatalf("parseOptions() DownloadWorkers = %d, want 5", opts.DownloadWorkers)
	}
}

func TestParseOptionsSupportsCheckManifestShortFlag(t *testing.T) {
	t.Parallel()

	opts, err := parseOptions([]string{"-c"})
	if err != nil {
		t.Fatalf("parseOptions() error = %v", err)
	}
	if !opts.CheckManifest {
		t.Fatalf("parseOptions() CheckManifest = false, want true")
	}
}

func TestParseOptionsSupportsDryRun(t *testing.T) {
	t.Parallel()

	opts, err := parseOptions([]string{"--dry-run"})
	if err != nil {
		t.Fatalf("parseOptions() error = %v", err)
	}
	if !opts.DryRun {
		t.Fatalf("parseOptions() DryRun = false, want true")
	}
}

func TestParseOptionsRejectsCheckManifestWithDryRun(t *testing.T) {
	t.Parallel()

	if _, err := parseOptions([]string{"--check-manifest", "--dry-run"}); err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("parseOptions() error = %v, want conflict", err)
	}
}

func TestInstallerUsageText(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	printInstallerUsage(&buf)
	got := buf.String()
	want := "Usage: blockforge [options]\n\nInstall or update a Minecraft server.\n\nOptions:\n  -m, --manifest SOURCE      Manifest source URL or local path (required on first install)\n  -d, --dir DIR              Server install directory (default: .)\n  -c, --check-manifest       Validate manifest and print a summary without changing files\n      --vanilla              Install/update latest recommended vanilla release\n      --dry-run              Show planned changes without modifying files\n  -f, --force                Re-download/reinstall files instead of keeping existing files\n  -w, --workers N            Concurrent mod download workers (default: 6, range: 1-16)\n  -j, --java PATH            Java executable path for vanilla and manifest installs (default: java)\n  -v, --version              Print installer version and exit\n  -h, --help                 Show this help text and exit\n\nFirst install requires --manifest SOURCE. Later runs reuse the saved source from .blockforge/manifest-url.\n"
	if got != want {
		t.Fatalf("usage text mismatch:\n--- got ---\n%s--- want ---\n%s", got, want)
	}
	if strings.Contains(got, "--check\n") {
		t.Fatalf("usage text unexpectedly mentions hidden --check alias\n%s", got)
	}
	if strings.Contains(got, "Default manifest URL") {
		t.Fatalf("usage text unexpectedly mentions default manifest URL\n%s", got)
	}
	if strings.Contains(got, "--manifest-url") || strings.Contains(got, "--download-workers") {
		t.Fatalf("usage text unexpectedly mentions deprecated aliases\n%s", got)
	}
}

func TestParseOptionsRejectsCheckAlias(t *testing.T) {
	t.Parallel()

	if _, err := parseOptions([]string{"--check"}); err == nil {
		t.Fatalf("parseOptions() accepted removed --check alias")
	}
}

func TestExtractZipFileRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(root, "traversal.zip")
	if err := writeTestZip(zipPath, map[string]string{"../evil.txt": "bad"}); err != nil {
		t.Fatal(err)
	}

	if err := extractZipFile(zipPath, root); err == nil || !strings.Contains(strings.ToLower(err.Error()), "escapes") {
		t.Fatalf("extractZipFile() error = %v, want traversal rejection", err)
	}
}

func TestExtractZipFileRejectsAbsolutePath(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(root, "absolute.zip")
	if err := writeTestZip(zipPath, map[string]string{"/evil.txt": "bad"}); err != nil {
		t.Fatal(err)
	}

	if err := extractZipFile(zipPath, root); err == nil || !strings.Contains(strings.ToLower(err.Error()), "absolute") {
		t.Fatalf("extractZipFile() error = %v, want absolute path rejection", err)
	}
}

func TestExtractZipFileRejectsTooManyFiles(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(root, "many.zip")
	if err := writeManyFileTestZip(zipPath, maxServerConfigFiles+1); err != nil {
		t.Fatal(err)
	}

	if err := extractZipFile(zipPath, root); err == nil || !strings.Contains(strings.ToLower(err.Error()), "too many files") {
		t.Fatalf("extractZipFile() error = %v, want file count limit", err)
	}
}

func TestExtractZipFileRejectsSingleFileLimit(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(root, "large-file.zip")
	if err := writeTestZip(zipPath, map[string]string{"config/large.txt": "123456"}); err != nil {
		t.Fatal(err)
	}

	withServerConfigZipLimits(t, zipExtractLimits{MaxFiles: maxServerConfigFiles, MaxSingleFileBytes: 5, MaxTotalBytes: maxServerConfigTotalBytes})

	if err := extractZipFile(zipPath, root); err == nil || !strings.Contains(strings.ToLower(err.Error()), "single-file limit") {
		t.Fatalf("extractZipFile() error = %v, want single-file limit", err)
	}
}

func TestExtractZipFileRejectsTotalBytesLimit(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(root, "total.zip")
	if err := writeTestZip(zipPath, map[string]string{"config/a.txt": "1234", "config/b.txt": "5678"}); err != nil {
		t.Fatal(err)
	}

	withServerConfigZipLimits(t, zipExtractLimits{MaxFiles: maxServerConfigFiles, MaxSingleFileBytes: maxServerConfigSingleFileBytes, MaxTotalBytes: 7})

	if err := extractZipFile(zipPath, root); err == nil || !strings.Contains(strings.ToLower(err.Error()), "total extracted limit") {
		t.Fatalf("extractZipFile() error = %v, want total limit", err)
	}
}

func TestExtractZipFileAllowsServerConfigRoots(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(root, "allowed.zip")
	if err := writeTestZip(zipPath, map[string]string{
		"config/example.toml":                       "a=1\n",
		"defaultconfigs/example.toml":               "b=2\n",
		"kubejs/server_scripts/example.js":          "console.log('ok')\n",
		"kubejs/data/example/advancement/root.json": "{}\n",
	}); err != nil {
		t.Fatal(err)
	}

	if err := extractZipFile(zipPath, root); err != nil {
		t.Fatalf("extractZipFile() error = %v, want allowlisted paths", err)
	}
	for _, path := range []string{
		filepath.Join(root, "config", "example.toml"),
		filepath.Join(root, "defaultconfigs", "example.toml"),
		filepath.Join(root, "kubejs", "server_scripts", "example.js"),
		filepath.Join(root, "kubejs", "data", "example", "advancement", "root.json"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s missing after extract: %v", path, err)
		}
	}
}

func TestExtractZipFileRejectsForbiddenServerConfigPaths(t *testing.T) {
	cases := []struct {
		name string
		path string
	}{
		{name: "blockforge", path: ".blockforge/manifest-url"},
		{name: "mods", path: "mods/example.jar"},
		{name: "libraries", path: "libraries/x"},
		{name: "server jar", path: "server.jar"},
		{name: "run sh", path: "run.sh"},
		{name: "run bat", path: "run.bat"},
		{name: "jvm args", path: "user_jvm_args.txt"},
		{name: "server properties", path: "server.properties"},
		{name: "world", path: "world/level.dat"},
		{name: "traversal", path: "../escape"},
		{name: "absolute", path: "/abs/path.txt"},
		{name: "backslash", path: "config\\bad.toml"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			zipPath := filepath.Join(root, tc.name+".zip")
			if err := writeTestZip(zipPath, map[string]string{tc.path: "bad"}); err != nil {
				t.Fatal(err)
			}

			err := extractZipFile(zipPath, root)
			if err == nil {
				t.Fatalf("extractZipFile() succeeded for %q", tc.path)
			}
			if !strings.Contains(strings.ToLower(err.Error()), "zip entry") && !strings.Contains(strings.ToLower(err.Error()), "escapes") {
				t.Fatalf("extractZipFile() error = %v, want path rejection", err)
			}
		})
	}
}

func TestExtractZipFileRejectsSymlinkEntry(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(root, "symlink.zip")
	file, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(file)
	header := &zip.FileHeader{Name: "config/link"}
	header.SetMode(os.ModeSymlink | 0o777)
	writer, err := zw.CreateHeader(header)
	if err != nil {
		_ = zw.Close()
		_ = file.Close()
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("target")); err != nil {
		_ = zw.Close()
		_ = file.Close()
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	if err := extractZipFile(zipPath, root); err == nil || !strings.Contains(strings.ToLower(err.Error()), "symlink") {
		t.Fatalf("extractZipFile() error = %v, want symlink rejection", err)
	}
}

func TestIsSafeModFilename(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		path string
		want bool
	}{
		{"simple", "mod.jar", true},
		{"double dots", "mod..name.jar", true},
		{"upper", "MOD.JAR", true},
		{"empty", "", false},
		{"dot", ".", false},
		{"dotdot", "..", false},
		{"traversal", "../mod.jar", false},
		{"slash", "dir/mod.jar", false},
		{"backslash", "dir\\mod.jar", false},
		{"absolute", "/abs/mod.jar", false},
		{"non jar", "mod.zip", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isSafeModFilename(tc.path); got != tc.want {
				t.Fatalf("isSafeModFilename(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestClassifyManagedFile(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(root, "current.jar")
	empty := filepath.Join(root, "empty.jar")
	sizeMismatch := filepath.Join(root, "size.jar")
	shaMismatch := filepath.Join(root, "sha.jar")
	dirPath := filepath.Join(root, "dir")

	if err := os.WriteFile(current, []byte("current"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sizeMismatch, []byte("1234"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shaMismatch, []byte("sha-old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatal(err)
	}

	currentSHA := sha1HexOfFile(t, current)

	cases := []struct {
		name    string
		path    string
		check   FileCheck
		force   bool
		want    FileAction
		wantErr bool
	}{
		{"missing", filepath.Join(root, "missing.jar"), FileCheck{}, false, FileActionMissing, false},
		{"empty", empty, FileCheck{}, false, FileActionReplace, false},
		{"current", current, FileCheck{SHA1: currentSHA, Size: int64(len("current"))}, false, FileActionCurrent, false},
		{"size mismatch", sizeMismatch, FileCheck{Size: 99}, false, FileActionReplace, false},
		{"sha mismatch", shaMismatch, FileCheck{SHA1: strings.Repeat("0", 40)}, false, FileActionReplace, false},
		{"force", current, FileCheck{SHA1: currentSHA}, true, FileActionReplace, false},
		{"dir", dirPath, FileCheck{}, false, "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := classifyManagedFile(tc.path, tc.check, tc.force)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("classifyManagedFile() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("classifyManagedFile() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("classifyManagedFile() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReconcileModsRemovesUnmanagedFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	modsDirPath := filepath.Join(root, "mods")
	if err := os.MkdirAll(modsDirPath, 0o755); err != nil {
		t.Fatal(err)
	}

	source := filepath.Join(root, "source.jar")
	if err := os.WriteFile(source, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(modsDirPath, "extra.jar"), []byte("extra"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modsDirPath, "shader.zip"), []byte("zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modsDirPath, "notes.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modsDirPath, ".hidden"), []byte("hidden"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(modsDirPath, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("payload"))
	}))
	defer server.Close()

	if err := ReconcileMods(root, []ManifestMod{{Name: "Source Mod", URL: server.URL + "/source.jar", WebsiteURL: "https://example.invalid", SHA1: sha1HexOfFile(t, source), Size: int64(len("payload"))}}, false, 6); err != nil {
		t.Fatalf("ReconcileMods() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(modsDirPath, "source.jar")); err != nil {
		t.Fatalf("source.jar missing after reconcile: %v", err)
	}
	if _, err := os.Stat(filepath.Join(modsDirPath, "extra.jar")); !os.IsNotExist(err) {
		t.Fatalf("extra.jar still present after reconcile: %v", err)
	}
	for _, name := range []string{"shader.zip", "notes.txt", ".hidden"} {
		if _, err := os.Stat(filepath.Join(modsDirPath, name)); !os.IsNotExist(err) {
			t.Fatalf("%s still present after reconcile: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(modsDirPath, "nested")); !os.IsNotExist(err) {
		t.Fatalf("nested directory still present after reconcile: %v", err)
	}
	if _, err := os.Stat(filepath.Join(modsDirPath, ".varda-desired-mods.txt")); !os.IsNotExist(err) {
		t.Fatalf(".varda-desired-mods.txt should not be created: %v", err)
	}
}

func TestReconcileModsUsesDecodedFilename(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	source := []byte("payload")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(source)
	}))
	defer server.Close()

	modURL := server.URL + "/Example%20Mod%2b%5b1%5d%27s.jar"
	if err := ReconcileMods(root, []ManifestMod{{Name: "Example Mod", URL: modURL, WebsiteURL: "https://example.invalid", SHA1: sha1HexOfBytes(source), Size: int64(len(source))}}, false, 2); err != nil {
		t.Fatalf("ReconcileMods() error = %v", err)
	}

	want := filepath.Join(root, "mods", "Example Mod+[1]'s.jar")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("decoded mod filename missing: %v", err)
	}
}

func TestReconcileModsRejectsUnsafeInferredFilenameBeforeWrite(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	err := ReconcileMods(root, []ManifestMod{{Name: "Bad Mod", URL: "https://example.invalid/mods/%2e%2e%2fevil.jar", WebsiteURL: "https://example.invalid", SHA1: strings.Repeat("1", 40), Size: 1}}, false, 2)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "unsafe") {
		t.Fatalf("ReconcileMods() error = %v, want unsafe filename rejection", err)
	}
	if _, err := os.Stat(filepath.Join(root, "mods")); !os.IsNotExist(err) {
		t.Fatalf("ReconcileMods() created mods dir for unsafe manifest: %v", err)
	}
}

func TestReconcileModsRejectsDuplicateFilenamesBeforeWrite(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	err := ReconcileMods(root, []ManifestMod{
		{Name: "First Mod", URL: "https://example.invalid/path/Example.jar", WebsiteURL: "https://example.invalid/first", SHA1: strings.Repeat("1", 40), Size: 1},
		{Name: "Second Mod", URL: "https://example.invalid/other/example.jar", WebsiteURL: "https://example.invalid/second", SHA1: strings.Repeat("2", 40), Size: 1},
	}, false, 2)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "duplicate mod filename") {
		t.Fatalf("ReconcileMods() error = %v, want duplicate filename rejection", err)
	}
	if _, err := os.Stat(filepath.Join(root, "mods")); !os.IsNotExist(err) {
		t.Fatalf("ReconcileMods() created mods dir for duplicate manifest: %v", err)
	}
}

func TestReconcileModsCancelsRemainingDownloadsAfterFailure(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	canceled := make(chan struct{}, 1)
	slowStarted := make(chan struct{})
	deps := runtimeDeps{
		httpClient: &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Path {
			case "/bad.jar":
				<-slowStarted
				return httpStatusResponse(t, http.StatusNotFound, "missing"), nil
			case "/slow.jar":
				close(slowStarted)
				<-req.Context().Done()
				select {
				case canceled <- struct{}{}:
				default:
				}
				return nil, req.Context().Err()
			default:
				t.Fatalf("unexpected request path %q", req.URL.Path)
				return nil, nil
			}
		})},
	}

	err := reconcileMods(context.Background(), deps, root, []ManifestMod{
		{Name: "Bad Mod", URL: "https://example.invalid/bad.jar", WebsiteURL: "https://example.invalid", SHA1: strings.Repeat("1", 40), Size: 1},
		{Name: "Slow Mod", URL: "https://example.invalid/slow.jar", WebsiteURL: "https://example.invalid", SHA1: strings.Repeat("2", 40), Size: 1},
	}, false, 2)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "bad mod") {
		t.Fatalf("reconcileMods() error = %v, want bad mod failure", err)
	}

	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("slow download was not canceled after first failure")
	}
}

func TestInstallOrUpdateNeoForgeUsesDesiredManifest(t *testing.T) {
	root := t.TempDir()
	desiredVersion := "21.1.228"
	installerPayload := []byte("fake installer")
	installerSHA1 := sha1HexOfBytes(installerPayload)
	sourceInstaller := filepath.Join(root, "source-installer.jar")
	if err := os.WriteFile(sourceInstaller, installerPayload, 0o644); err != nil {
		t.Fatal(err)
	}
	desired := LoaderManifest{
		Type:         "neoforge",
		Version:      desiredVersion,
		InstallerURL: "https://example.invalid/releases/net/neoforged/neoforge/21.1.999/neoforge-21.1.999-installer.jar",
		SHA1:         installerSHA1,
	}

	deps := runtimeDeps{
		httpClient: &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != desired.InstallerURL {
				t.Fatalf("unexpected URL %s", req.URL.String())
			}
			return httpResponse(t, installerPayload), nil
		})},
		lookPath: func(string) (string, error) { return "custom-java", nil },
		command: func(name string, args ...string) *exec.Cmd {
			if name != "custom-java" {
				t.Fatalf("java command name = %q, want custom-java", name)
			}
			switch {
			case len(args) == 1 && args[0] == "-version":
				return helperJavaCommand(t, `openjdk version "21.0.2"`)
			case len(args) == 3 && args[0] == "-jar" && args[2] == "--installServer":
				return helperNeoForgeInstallerCommand(t, desiredVersion)
			default:
				t.Fatalf("java command args = %v, want -version or -jar ... --installServer", args)
			}
			return nil
		},
		stat:   os.Stat,
		rename: os.Rename,
		remove: os.Remove,
	}

	got, err := installOrUpdateNeoForge(context.Background(), deps, root, desired, "custom-java", false)
	if err != nil {
		t.Fatalf("InstallOrUpdateNeoForge() error = %v", err)
	}
	if got != desiredVersion {
		t.Fatalf("InstallOrUpdateNeoForge() = %q, want %q", got, desiredVersion)
	}
}

func TestInstallOrUpdateLoaderRejectsRecognizedUnimplementedLoaders(t *testing.T) {
	t.Parallel()

	for _, loaderType := range []string{"forge", "fabric", "quilt"} {
		loaderType := loaderType
		t.Run(loaderType, func(t *testing.T) {
			t.Parallel()

			_, err := InstallOrUpdateLoader(t.TempDir(), LoaderManifest{Type: loaderType, Version: "1.0.0"}, "java", false)
			if err == nil || !strings.Contains(err.Error(), "recognized by the manifest schema but is not implemented") {
				t.Fatalf("InstallOrUpdateLoader() error = %v, want not implemented", err)
			}
		})
	}
}

func TestDownloadToFileRejectsSHA1Mismatch(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	source := filepath.Join(root, "source.jar")
	if err := os.WriteFile(source, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target.jar")
	original := []byte("original target content")
	if err := os.WriteFile(target, original, 0o644); err != nil {
		t.Fatal(err)
	}

	err := downloadToFile(fileURL(source), target, true, "artifact", DownloadChecks{SHA1: strings.Repeat("0", 40)})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "sha1 mismatch") {
		t.Fatalf("downloadToFile() error = %v, want sha1 mismatch", err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(original) {
		t.Fatalf("target changed on sha1 mismatch: %q", data)
	}
}

func TestReconcileModsKeepsMatchingSHA1(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	source := filepath.Join(root, "source.jar")
	payload := []byte("payload")
	if err := os.WriteFile(source, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	modsDirPath := filepath.Join(root, "mods")
	if err := os.MkdirAll(modsDirPath, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(modsDirPath, "source.jar")
	if err := os.WriteFile(target, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	if err := ReconcileMods(root, []ManifestMod{{Name: "Source Mod", URL: server.URL + "/source.jar", WebsiteURL: "https://example.invalid", SHA1: sha1HexOfBytes(payload), Size: int64(len(payload))}}, false, 2); err != nil {
		t.Fatalf("ReconcileMods() error = %v", err)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(payload) {
		t.Fatalf("target changed unexpectedly: %q", data)
	}
}

func TestReconcileModsRedownloadsMismatchedSHA1(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	source := filepath.Join(root, "source.jar")
	sourcePayload := []byte("new payload")
	if err := os.WriteFile(source, sourcePayload, 0o644); err != nil {
		t.Fatal(err)
	}

	modsDirPath := filepath.Join(root, "mods")
	if err := os.MkdirAll(modsDirPath, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(modsDirPath, "source.jar")
	if err := os.WriteFile(target, []byte("old payload"), 0o644); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(sourcePayload)
	}))
	defer server.Close()

	if err := ReconcileMods(root, []ManifestMod{{Name: "Source Mod", URL: server.URL + "/source.jar", WebsiteURL: "https://example.invalid", SHA1: sha1HexOfBytes(sourcePayload), Size: int64(len(sourcePayload))}}, false, 2); err != nil {
		t.Fatalf("ReconcileMods() error = %v", err)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(sourcePayload) {
		t.Fatalf("target not re-downloaded: %q", data)
	}
}

func TestPatchLaunchersIncludesNogui(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	version := "21.1.83"
	if err := PatchLaunchers(root, version); err != nil {
		t.Fatalf("PatchLaunchers() error = %v", err)
	}

	runSh, err := os.ReadFile(filepath.Join(root, "run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	runBat, err := os.ReadFile(filepath.Join(root, "run.bat"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(runSh), "nogui") || !strings.Contains(string(runBat), "nogui") {
		t.Fatalf("launchers missing nogui:\nrun.sh=%s\nrun.bat=%s", runSh, runBat)
	}
	if _, err := os.Stat(filepath.Join(root, "run.ps1")); !os.IsNotExist(err) {
		t.Fatalf("run.ps1 should not be generated: %v", err)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(root, "run.sh"))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode()&0o111 == 0 {
			t.Fatalf("run.sh not executable")
		}
	}
}

func TestPatchRunBatAcceptsRealWorldForwardSlashes(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	version := "21.1.228"
	content := strings.Join([]string{
		"@echo off",
		"REM Forge requires a configured set of both JVM and program arguments.",
		"REM Add custom JVM arguments to the user_jvm_args.txt",
		"REM Add custom program arguments {such as nogui} to this file in the next line before the %* or",
		"REM  pass them to this script directly",
		"java @user_jvm_args.txt @libraries/net/neoforged/neoforge/21.1.228/win_args.txt %*",
		"pause",
		"",
	}, "\r\n")
	if err := os.WriteFile(filepath.Join(root, "run.bat"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := patchRunBat(root, version); err != nil {
		t.Fatalf("patchRunBat() error = %v", err)
	}

	after, err := os.ReadFile(filepath.Join(root, "run.bat"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(after), "pause") {
		t.Fatalf("patched run.bat lost pause:\n%s", after)
	}
	if countLaunchNogui(strings.Split(string(after), "\r\n")) != 1 {
		t.Fatalf("expected exactly one nogui launch line:\n%s", after)
	}
	if !strings.Contains(string(after), "@libraries/net/neoforged/neoforge/21.1.228/win_args.txt nogui %*") {
		t.Fatalf("patched run.bat missing forward-slash launch line:\n%s", after)
	}

	if err := patchRunBat(root, version); err != nil {
		t.Fatalf("second patchRunBat() error = %v", err)
	}
	again, err := os.ReadFile(filepath.Join(root, "run.bat"))
	if err != nil {
		t.Fatal(err)
	}
	if countLaunchNogui(strings.Split(string(again), "\r\n")) != 1 {
		t.Fatalf("idempotency failed, nogui duplicated:\n%s", again)
	}
}

func TestPatchRunBatAcceptsBackslashes(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	version := "21.1.228"
	content := "@echo off\r\njava @user_jvm_args.txt @libraries\\net\\neoforged\\neoforge\\21.1.228\\win_args.txt %*\r\npause\r\n"
	if err := os.WriteFile(filepath.Join(root, "run.bat"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := patchRunBat(root, version); err != nil {
		t.Fatalf("patchRunBat() error = %v", err)
	}

	after, err := os.ReadFile(filepath.Join(root, "run.bat"))
	if err != nil {
		t.Fatal(err)
	}
	if countLaunchNogui(strings.Split(string(after), "\r\n")) != 1 {
		t.Fatalf("expected exactly one nogui launch line:\n%s", after)
	}
}

func TestCleanupNeoForgeInstallerArtifacts(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	files := []string{
		"neoforge-21.1.228-installer.jar",
		"neoforge-21.1.228-installer.jar.log",
		"installer.log",
	}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := cleanupNeoForgeInstallerArtifacts(root); err != nil {
		t.Fatalf("cleanupNeoForgeInstallerArtifacts() error = %v", err)
	}

	for _, name := range files {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("%s still present after cleanup: %v", name, err)
		}
	}
}

func TestReplaceFileRestoresExistingTarget(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "artifact.jar")
	temp := filepath.Join(root, "artifact.jar.tmp")

	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(temp, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := replaceFile(temp, target); err != nil {
		t.Fatalf("replaceFile() error = %v", err)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Fatalf("replaceFile() wrote %q, want %q", data, "new")
	}
	if _, err := os.Stat(target + ".bak"); !os.IsNotExist(err) {
		t.Fatalf("backup file still present: %v", err)
	}
}

func TestReplaceFileCreatesMissingTarget(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "artifact.jar")
	temp := filepath.Join(root, "artifact.jar.tmp")

	if err := os.WriteFile(temp, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := replaceFile(temp, target); err != nil {
		t.Fatalf("replaceFile() error = %v", err)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Fatalf("replaceFile() wrote %q, want %q", data, "new")
	}
}

func TestReplaceFileRestoresTargetWhenFinalRenameFails(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "artifact.jar")
	temp := filepath.Join(root, "artifact.jar.tmp")

	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(temp, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	deps := runtimeDeps{
		stat: func(string) (os.FileInfo, error) { return nil, nil },
		rename: func(oldpath, newpath string) error {
			if oldpath == temp && newpath == target {
				return os.ErrPermission
			}
			return os.Rename(oldpath, newpath)
		},
		remove: func(string) error { return nil },
	}

	if err := replaceFileWithDeps(deps, temp, target); !os.IsPermission(err) {
		t.Fatalf("replaceFile() error = %v, want permission error", err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "old" {
		t.Fatalf("target not restored: %q", data)
	}
}

func TestReplaceFileDoesNotClobberStaleFixedBackup(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "artifact.jar")
	temp := filepath.Join(root, "artifact.jar.tmp")
	staleBackup := target + ".bak"

	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(temp, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staleBackup, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := replaceFile(temp, target); err != nil {
		t.Fatalf("replaceFile() error = %v", err)
	}
	data, err := os.ReadFile(staleBackup)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "stale" {
		t.Fatalf("stale backup was clobbered: %q", data)
	}
}

func TestReplaceFilePropagatesStatErrors(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "artifact.jar")
	temp := filepath.Join(root, "artifact.jar.tmp")

	if err := os.WriteFile(temp, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	deps := runtimeDeps{
		stat: func(string) (os.FileInfo, error) {
			return nil, os.ErrPermission
		},
		rename: os.Rename,
		remove: os.Remove,
	}

	if err := replaceFileWithDeps(deps, temp, target); !os.IsPermission(err) {
		t.Fatalf("replaceFile() error = %v, want permission error", err)
	}

	if _, err := os.Stat(temp); err != nil {
		t.Fatalf("temp file should remain after stat failure cleanup path: %v", err)
	}
}

func TestDownloadURLToBytesRetriesRetryableFailures(t *testing.T) {
	t.Parallel()

	calls := 0
	deps := runtimeDeps{
		httpClient: &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return httpStatusResponse(t, http.StatusInternalServerError, "boom"), nil
			}
			return httpResponse(t, []byte("ok")), nil
		})},
	}

	got, err := downloadURLToBytesCtx(context.Background(), deps, "https://example.invalid/file.json")
	if err != nil {
		t.Fatalf("downloadURLToBytesCtx() error = %v", err)
	}
	if string(got) != "ok" {
		t.Fatalf("downloadURLToBytesCtx() = %q, want ok", got)
	}
	if calls != 2 {
		t.Fatalf("download attempts = %d, want 2", calls)
	}
}

func TestIsRetryableDownloadError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"server error", &downloadStatusError{StatusCode: http.StatusInternalServerError}, true},
		{"client error", &downloadStatusError{StatusCode: http.StatusNotFound}, false},
		{"network error", &net.DNSError{}, true},
		{"checksum mismatch", fmt.Errorf("checksum mismatch"), false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isRetryableDownloadError(tc.err); got != tc.want {
				t.Fatalf("isRetryableDownloadError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func httpStatusResponse(t *testing.T, status int, body string) *http.Response {
	t.Helper()

	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func TestParseOptionsDefaultWorkers(t *testing.T) {
	t.Parallel()

	opts, err := parseOptions(nil)
	if err != nil {
		t.Fatalf("parseOptions() error = %v", err)
	}
	if opts.DownloadWorkers != 6 {
		t.Fatalf("parseOptions() DownloadWorkers = %d, want 6", opts.DownloadWorkers)
	}
}

func TestParseOptionsRejectsInvalidWorkers(t *testing.T) {
	t.Parallel()

	if _, err := parseOptions([]string{"--workers", "0"}); err == nil {
		t.Fatalf("parseOptions() accepted 0 workers")
	}
	if _, err := parseOptions([]string{"--workers", "17"}); err == nil {
		t.Fatalf("parseOptions() accepted 17 workers")
	}
}

func validTestManifest() Manifest {
	return Manifest{
		SchemaVersion: 1,
		Pack:          "varda",
		Version:       "0.1.4",
		Minecraft:     "1.21.1",
		Java:          JavaManifest{Major: 21},
		Loader: LoaderManifest{
			Type:         "neoforge",
			Version:      "21.1.228",
			InstallerURL: "https://maven.neoforged.net/releases/net/neoforged/neoforge/21.1.228/neoforge-21.1.228-installer.jar",
			SHA1:         strings.Repeat("3", 40),
		},
		ServerConfig: &ServerConfigManifest{
			URL:  "https://example.invalid/server-config.zip",
			SHA1: strings.Repeat("2", 40),
		},
		Mods: []ManifestMod{
			{
				Name:       "Example Mod",
				URL:        "https://example.invalid/example.jar",
				WebsiteURL: "https://example.invalid",
				SHA1:       strings.Repeat("1", 40),
				Size:       1234,
			},
		},
	}
}

func validManifestJSON() string {
	return `{
  "schema_version": 1,
  "pack": "testpack",
  "version": "0.1.4",
  "minecraft": "1.21.1",
  "java": {
    "major": 21
  },
  "loader": {
    "type": "neoforge",
    "version": "21.1.228",
    "installer_url": "https://example.invalid/neoforge-21.1.228-installer.jar",
    "sha1": "0123456789abcdef0123456789abcdef01234567"
  },
  "server_config": {
    "url": "https://example.invalid/server-config.zip",
    "sha1": "0123456789abcdef0123456789abcdef01234567"
  },
  "mods": [
    {
      "name": "Example Mod",
      "url": "https://example.invalid/example.jar",
      "website_url": "https://example.invalid",
      "sha1": "0123456789abcdef0123456789abcdef01234567",
      "size": 1234
    }
  ]
}`
}

func writeTestZip(path string, entries map[string]string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	zw := zip.NewWriter(file)
	for name, content := range entries {
		writer, err := zw.Create(name)
		if err != nil {
			zw.Close()
			return err
		}
		if _, err := writer.Write([]byte(content)); err != nil {
			zw.Close()
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return nil
}

func writeManyFileTestZip(path string, count int) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	zw := zip.NewWriter(file)
	for i := 0; i < count; i++ {
		writer, err := zw.Create("config/file-" + strconv.Itoa(i) + ".txt")
		if err != nil {
			zw.Close()
			return err
		}
		if _, err := writer.Write([]byte("x")); err != nil {
			zw.Close()
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return nil
}

func withServerConfigZipLimits(t *testing.T, limits zipExtractLimits) {
	t.Helper()

	old := serverConfigZipLimits
	serverConfigZipLimits = limits
	t.Cleanup(func() {
		serverConfigZipLimits = old
	})
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	oldStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer

	out := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, reader)
		out <- buf.String()
	}()

	defer func() {
		os.Stdout = oldStdout
	}()
	fn()
	_ = writer.Close()
	output := <-out
	_ = reader.Close()
	return output
}

func fileURL(path string) string {
	slashes := filepath.ToSlash(path)
	if strings.HasPrefix(slashes, "/") {
		return "file://" + slashes
	}
	return "file:///" + slashes
}

func sha1HexOfBytes(data []byte) string {
	sum := sha1.Sum(data)
	return hex.EncodeToString(sum[:])
}

func sha1HexOfFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return sha1HexOfBytes(data)
}

func countLaunchNogui(lines []string) int {
	count := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(strings.ToLower(line), "java ") {
			continue
		}
		if strings.Count(strings.ToLower(line), " nogui ") == 1 {
			count++
		}
	}
	return count
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func httpResponse(t *testing.T, body []byte) *http.Response {
	t.Helper()

	return &http.Response{
		StatusCode: 200,
		Status:     "200 OK",
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
	}
}

func helperJavaCommand(t *testing.T, output string) *exec.Cmd {
	t.Helper()

	cmd := exec.Command(os.Args[0], "-test.run=TestHelperCommandProcess", "--", "java-version", output)
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	return cmd
}

func helperNeoForgeInstallerCommand(t *testing.T, version string) *exec.Cmd {
	t.Helper()

	cmd := exec.Command(os.Args[0], "-test.run=TestHelperCommandProcess", "--", "neoforge-installer", version)
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	return cmd
}

func TestHelperCommandProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	args := os.Args
	for i, arg := range args {
		if arg != "--" || i+1 >= len(args) {
			continue
		}
		switch args[i+1] {
		case "java-version":
			if i+2 >= len(args) {
				_, _ = os.Stderr.WriteString("missing helper java output")
				os.Exit(1)
			}
			_, _ = os.Stderr.WriteString(args[i+2])
			os.Exit(0)
		case "neoforge-installer":
			if i+2 >= len(args) {
				_, _ = os.Stderr.WriteString("missing helper version")
				os.Exit(1)
			}
			versionDir := filepath.Join("libraries", "net", "neoforged", "neoforge", args[i+2])
			if err := os.MkdirAll(versionDir, 0o755); err != nil {
				_, _ = os.Stderr.WriteString(err.Error())
				os.Exit(1)
			}
			os.Exit(0)
		default:
			_, _ = os.Stderr.WriteString("unknown helper mode")
			os.Exit(1)
		}
	}

	_, _ = os.Stderr.WriteString("missing helper mode")
	os.Exit(1)
}
