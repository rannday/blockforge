package serverinstaller

import (
	"archive/zip"
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
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
			output: `openjdk version "21.0.4" 2024-07-16`,
			want:   21,
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
	if _, err := resolveManifestSource(root, ""); err == nil || !strings.Contains(err.Error(), "manifest URL required on first install") {
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

func TestParseOptionsSupportsDeprecatedAliases(t *testing.T) {
	t.Parallel()

	opts, err := parseOptions([]string{"--manifest-url", "https://example.invalid/manifest.json", "--download-workers", "4"})
	if err != nil {
		t.Fatalf("parseOptions() error = %v", err)
	}
	if opts.ManifestSource != "https://example.invalid/manifest.json" {
		t.Fatalf("parseOptions() ManifestSource = %q, want deprecated manifest alias", opts.ManifestSource)
	}
	if opts.DownloadWorkers != 4 {
		t.Fatalf("parseOptions() DownloadWorkers = %d, want 4", opts.DownloadWorkers)
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

func TestInstallerUsageText(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	printInstallerUsage(&buf)
	got := buf.String()
	want := "Usage: blockforge [options]\n\nInstall or update a modded Minecraft server from a manifest.\n\nOptions:\n  -m, --manifest SOURCE      Manifest source URL or local path (required on first install)\n  -d, --dir DIR              Server install directory (default: .)\n  -c, --check-manifest       Validate manifest and print a summary without changing files\n  -f, --force                Re-download/reinstall files instead of keeping existing files\n  -w, --workers N            Concurrent mod download workers (default: 6, range: 1-16)\n  -v, --version              Print installer version and exit\n  -h, --help                 Show this help text and exit\n\nFirst install requires --manifest. Later runs reuse the saved source from .blockforge/manifest-url.\n"
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
	if err := writeTestZip(zipPath, map[string]string{"large.txt": "123456"}); err != nil {
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
	if err := writeTestZip(zipPath, map[string]string{"a.txt": "1234", "b.txt": "5678"}); err != nil {
		t.Fatal(err)
	}

	withServerConfigZipLimits(t, zipExtractLimits{MaxFiles: maxServerConfigFiles, MaxSingleFileBytes: maxServerConfigSingleFileBytes, MaxTotalBytes: 7})

	if err := extractZipFile(zipPath, root); err == nil || !strings.Contains(strings.ToLower(err.Error()), "total extracted limit") {
		t.Fatalf("extractZipFile() error = %v, want total limit", err)
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

	oldJavaCommand := javaCommand
	oldDownloadFile := downloadFile
	javaCommand = func(name string, args ...string) *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=TestHelperJavaInstallerProcess", "--", filepath.Join(root, "libraries", "net", "neoforged", "neoforge", desiredVersion))
		cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
		return cmd
	}
	downloadFile = func(rawURL, targetPath string, force bool, label string, checks ...DownloadChecks) error {
		if strings.Contains(rawURL, ".sha1") {
			t.Fatalf("downloadFile() unexpectedly fetched sha1 URL: %s", rawURL)
		}
		if rawURL != desired.InstallerURL {
			t.Fatalf("downloadFile() rawURL = %q, want %q", rawURL, desired.InstallerURL)
		}
		if len(checks) != 1 || checks[0].SHA1 != installerSHA1 {
			t.Fatalf("downloadFile() checks = %#v, want one sha1 check", checks)
		}
		return downloadToFile(fileURL(sourceInstaller), targetPath, force, label, checks...)
	}
	defer func() {
		javaCommand = oldJavaCommand
		downloadFile = oldDownloadFile
	}()

	got, err := InstallOrUpdateNeoForge(root, desired, false)
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

			_, err := InstallOrUpdateLoader(t.TempDir(), LoaderManifest{Type: loaderType, Version: "1.0.0"}, false)
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

func TestReplaceFilePropagatesStatErrors(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "artifact.jar")
	temp := filepath.Join(root, "artifact.jar.tmp")

	if err := os.WriteFile(temp, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	oldStat := statFile
	statFile = func(string) (os.FileInfo, error) {
		return nil, os.ErrPermission
	}
	defer func() { statFile = oldStat }()

	if err := replaceFile(temp, target); !os.IsPermission(err) {
		t.Fatalf("replaceFile() error = %v, want permission error", err)
	}

	if _, err := os.Stat(temp); err != nil {
		t.Fatalf("temp file should remain after stat failure cleanup path: %v", err)
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
		writer, err := zw.Create("file-" + strconv.Itoa(i) + ".txt")
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

func TestHelperJavaInstallerProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	args := os.Args
	for i, arg := range args {
		if arg != "--" || i+1 >= len(args) {
			continue
		}
		if err := os.MkdirAll(args[i+1], 0o755); err != nil {
			_, _ = os.Stderr.WriteString(err.Error())
			os.Exit(1)
		}
		os.Exit(0)
	}

	_, _ = os.Stderr.WriteString("missing helper target dir")
	os.Exit(1)
}
