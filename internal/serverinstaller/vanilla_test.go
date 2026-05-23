package serverinstaller

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseOptionsSupportsVanilla(t *testing.T) {
	opts, err := parseOptions([]string{"--vanilla", "-d", "srv", "-j", "custom-java"})
	if err != nil {
		t.Fatalf("parseOptions() error = %v", err)
	}
	if !opts.Vanilla {
		t.Fatalf("parseOptions() Vanilla = false, want true")
	}
	if opts.TargetDir != "srv" {
		t.Fatalf("parseOptions() TargetDir = %q, want srv", opts.TargetDir)
	}
	if opts.JavaPath != "custom-java" {
		t.Fatalf("parseOptions() JavaPath = %q, want custom-java", opts.JavaPath)
	}
}

func TestParseOptionsSupportsJavaPathLong(t *testing.T) {
	opts, err := parseOptions([]string{"--vanilla", "--java", "C:\\Java\\bin\\java.exe"})
	if err != nil {
		t.Fatalf("parseOptions() error = %v", err)
	}
	if opts.JavaPath != "C:\\Java\\bin\\java.exe" {
		t.Fatalf("parseOptions() JavaPath = %q, want custom path", opts.JavaPath)
	}
}

func TestParseOptionsRejectsVanillaConflicts(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"manifest long", []string{"--vanilla", "--manifest", "https://example.invalid/manifest.json"}, "--manifest"},
		{"manifest short", []string{"--vanilla", "-m", "https://example.invalid/manifest.json"}, "--manifest"},
		{"check manifest long", []string{"--vanilla", "--check-manifest"}, "--check-manifest"},
		{"check manifest short", []string{"--vanilla", "-c"}, "--check-manifest"},
		{"workers", []string{"--vanilla", "--workers", "2"}, "--workers"},
		{"workers short", []string{"--vanilla", "-w", "2"}, "--workers"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseOptions(tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("parseOptions() error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestParseOptionsSupportsJavaWithoutVanilla(t *testing.T) {
	opts, err := parseOptions([]string{"--java", "custom-java", "--manifest", "https://example.invalid/manifest.json"})
	if err != nil {
		t.Fatalf("parseOptions() error = %v", err)
	}
	if opts.JavaPath != "custom-java" {
		t.Fatalf("parseOptions() JavaPath = %q, want custom-java", opts.JavaPath)
	}
}

func TestFetchLatestVanillaServerUsesLatestRelease(t *testing.T) {
	deps := vanillaFetchDeps(t, map[string]string{
		vanillaVersionManifestURL: `{
			"latest": {"release": "1.21.8", "snapshot": "25w01a"},
			"versions": [
				{"id": "25w01a", "url": "https://example.invalid/snapshot.json"},
				{"id": "1.21.8", "url": "https://example.invalid/release.json"}
			]
		}`,
		"https://example.invalid/release.json": vanillaVersionJSON("https://example.invalid/server.jar", strings.Repeat("a", 40), 6),
	})

	server, err := fetchLatestVanillaServer(context.Background(), deps)
	if err != nil {
		t.Fatalf("FetchLatestVanillaServer() error = %v", err)
	}
	if server.MinecraftVersion != "1.21.8" {
		t.Fatalf("MinecraftVersion = %q, want latest.release", server.MinecraftVersion)
	}
	if server.ServerURL != "https://example.invalid/server.jar" {
		t.Fatalf("ServerURL = %q, want release server URL", server.ServerURL)
	}
	if server.JavaMajorVersion != 25 {
		t.Fatalf("JavaMajorVersion = %d, want 25", server.JavaMajorVersion)
	}
}

func TestFetchLatestVanillaServerErrors(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    string
	}{
		{"missing java", fmt.Sprintf(`{ "downloads": { "server": { "url": "https://example.invalid/server.jar", "sha1": %q, "size": 1 } } }`, strings.Repeat("a", 40)), "missing javaVersion.majorVersion"},
		{"missing server", `{ "javaVersion": { "majorVersion": 21 }, "downloads": {} }`, "missing downloads.server"},
		{"missing url", fmt.Sprintf(`{ "javaVersion": { "majorVersion": 21 }, "downloads": { "server": { "sha1": %q, "size": 1 } } }`, strings.Repeat("a", 40)), "missing downloads.server.url"},
		{"missing sha1", `{ "javaVersion": { "majorVersion": 21 }, "downloads": { "server": { "url": "https://example.invalid/server.jar", "size": 1 } } }`, "missing downloads.server.sha1"},
		{"missing size", fmt.Sprintf(`{ "javaVersion": { "majorVersion": 21 }, "downloads": { "server": { "url": "https://example.invalid/server.jar", "sha1": %q } } }`, strings.Repeat("a", 40)), "missing downloads.server.size"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			deps := vanillaFetchDeps(t, map[string]string{
				vanillaVersionManifestURL: `{
					"latest": {"release": "1.21.8"},
					"versions": [{"id": "1.21.8", "url": "https://example.invalid/release.json"}]
				}`,
				"https://example.invalid/release.json": tc.version,
			})

			_, err := fetchLatestVanillaServer(context.Background(), deps)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("FetchLatestVanillaServer() error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestFetchLatestVanillaServerMetadataErrors(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		want     string
	}{
		{"invalid json", `{`, "parse vanilla version manifest"},
		{"missing latest release", `{ "latest": {}, "versions": [] }`, "missing latest.release"},
		{"latest release absent", `{ "latest": {"release": "1.21.8"}, "versions": [] }`, "not found"},
		{"missing version url", `{ "latest": {"release": "1.21.8"}, "versions": [{"id": "1.21.8"}] }`, "missing version URL"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			deps := vanillaFetchDeps(t, map[string]string{vanillaVersionManifestURL: tc.manifest})

			_, err := fetchLatestVanillaServer(context.Background(), deps)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("FetchLatestVanillaServer() error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestRunVanillaKeepsMatchingServerJar(t *testing.T) {
	root, server := vanillaFixture(t, "server")
	if err := os.WriteFile(filepath.Join(root, "server.jar"), []byte("server"), 0o644); err != nil {
		t.Fatal(err)
	}

	deps := vanillaRunDeps(t, server)

	if err := runVanilla(context.Background(), deps, Options{TargetDir: root}); err != nil {
		t.Fatalf("RunVanilla() error = %v", err)
	}
	if got := readTestFile(t, filepath.Join(root, "server.jar")); got != "server" {
		t.Fatalf("server.jar = %q, want kept content", got)
	}
}

func TestRunVanillaReplacesMismatchedServerJar(t *testing.T) {
	root, server := vanillaFixture(t, "new server")
	if err := os.WriteFile(filepath.Join(root, "server.jar"), []byte("old server"), 0o644); err != nil {
		t.Fatal(err)
	}

	deps := vanillaRunDeps(t, server)

	if err := runVanilla(context.Background(), deps, Options{TargetDir: root}); err != nil {
		t.Fatalf("RunVanilla() error = %v", err)
	}
	if got := readTestFile(t, filepath.Join(root, "server.jar")); got != "new server" {
		t.Fatalf("server.jar = %q, want replaced content", got)
	}
}

func TestRunVanillaForceRedownloadsMatchingServerJar(t *testing.T) {
	root, server := vanillaFixture(t, "server")
	if err := os.WriteFile(filepath.Join(root, "server.jar"), []byte("server"), 0o644); err != nil {
		t.Fatal(err)
	}

	deps := vanillaRunDeps(t, server)

	if err := runVanilla(context.Background(), deps, Options{TargetDir: root, Force: true}); err != nil {
		t.Fatalf("RunVanilla() error = %v", err)
	}
	if got := readTestFile(t, filepath.Join(root, "server.jar")); got != "server" {
		t.Fatalf("server.jar = %q, want server", got)
	}
}

func TestRunVanillaWritesLaunchersJVMArgsAndState(t *testing.T) {
	root, server := vanillaFixture(t, "server")
	deps := vanillaRunDeps(t, server)

	if err := runVanilla(context.Background(), deps, Options{TargetDir: root}); err != nil {
		t.Fatalf("RunVanilla() error = %v", err)
	}

	if got := readTestFile(t, filepath.Join(root, "run.sh")); got != vanillaRunSh(defaultJavaCommand) {
		t.Fatalf("run.sh = %q, want vanilla launcher", got)
	}
	if got := readTestFile(t, filepath.Join(root, "run.bat")); got != vanillaRunBat(defaultJavaCommand) {
		t.Fatalf("run.bat = %q, want vanilla launcher", got)
	}
	if _, err := os.Stat(filepath.Join(root, "user_jvm_args.txt")); err != nil {
		t.Fatalf("user_jvm_args.txt missing: %v", err)
	}
	if got := strings.TrimSpace(readTestFile(t, stateFile(root, "install-type"))); got != "vanilla" {
		t.Fatalf("install-type = %q, want vanilla", got)
	}
	if got := strings.TrimSpace(readTestFile(t, stateFile(root, "minecraft-version"))); got != server.MinecraftVersion {
		t.Fatalf("minecraft-version = %q, want %q", got, server.MinecraftVersion)
	}
	if got := strings.TrimSpace(readTestFile(t, stateFile(root, "server-jar-sha1"))); got != server.ServerSHA1 {
		t.Fatalf("server-jar-sha1 = %q, want %q", got, server.ServerSHA1)
	}
	if got := strings.TrimSpace(readTestFile(t, stateFile(root, "java-major-version"))); got != fmt.Sprint(server.JavaMajorVersion) {
		t.Fatalf("java-major-version = %q, want %d", got, server.JavaMajorVersion)
	}
	if got := strings.TrimSpace(readTestFile(t, stateFile(root, "installer-version.txt"))); got != Version {
		t.Fatalf("installer-version.txt = %q, want %q", got, Version)
	}
}

func TestVanillaStateFilesMatchWrite(t *testing.T) {
	root, server := vanillaFixture(t, "server")

	if err := WriteVanillaState(root, server); err != nil {
		t.Fatalf("WriteVanillaState() error = %v", err)
	}

	for name, want := range vanillaStateFiles(server) {
		if got := readTestFile(t, stateFile(root, name)); got != want {
			t.Fatalf("state file %s = %q, want %q", name, got, want)
		}
	}
}

func TestRunVanillaUsesManifestJavaVersionAndCustomJavaPath(t *testing.T) {
	root, server := vanillaFixture(t, "server")
	server.JavaMajorVersion = 25

	var gotJavaPath string
	javaPath := "C:\\Program Files\\Java\\jdk-25\\bin\\java.exe"
	deps := vanillaRunDeps(t, server)
	deps.lookPath = func(javaPath string) (string, error) {
		gotJavaPath = javaPath
		return javaPath, nil
	}
	deps.command = func(name string, args ...string) *exec.Cmd {
		if name != javaPath {
			t.Fatalf("command name = %q, want %q", name, javaPath)
		}
		if len(args) != 1 || args[0] != "-version" {
			t.Fatalf("command args = %v, want -version", args)
		}
		return helperJavaCommand(t, `openjdk version "25.0.1"`)
	}

	if err := runVanilla(context.Background(), deps, Options{TargetDir: root, JavaPath: javaPath}); err != nil {
		t.Fatalf("RunVanilla() error = %v", err)
	}
	if gotJavaPath != javaPath {
		t.Fatalf("RequireJava path = %q, want %q", gotJavaPath, javaPath)
	}
	if got := readTestFile(t, filepath.Join(root, "run.sh")); got != vanillaRunSh(javaPath) {
		t.Fatalf("run.sh = %q, want custom Java launcher", got)
	}
	if got := readTestFile(t, filepath.Join(root, "run.bat")); got != vanillaRunBat(javaPath) {
		t.Fatalf("run.bat = %q, want custom Java launcher", got)
	}
}

func TestRunVanillaDryRunWritesNoFiles(t *testing.T) {
	root, server := vanillaFixture(t, "server")
	deps := vanillaRunDeps(t, server)

	if err := runVanilla(context.Background(), deps, Options{TargetDir: root, DryRun: true}); err != nil {
		t.Fatalf("RunVanilla() error = %v", err)
	}
	if entries, err := os.ReadDir(root); err != nil {
		t.Fatal(err)
	} else if len(entries) != 0 {
		t.Fatalf("dry-run wrote files: %v", entries)
	}
}

func TestRunVanillaRejectsManifestManagedDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(stateDir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(savedManifestURLPath(root), []byte("https://example.invalid/manifest.json\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := runVanilla(context.Background(), defaultDeps, Options{TargetDir: root, DryRun: true})
	if err == nil || !strings.Contains(err.Error(), "manifest-managed") {
		t.Fatalf("RunVanilla() error = %v, want manifest-managed rejection", err)
	}
}

func TestRunVanillaRejectsNonVanillaInstallType(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(stateDir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(vanillaInstallTypePath(root), []byte("manifest\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := runVanilla(context.Background(), defaultDeps, Options{TargetDir: root, DryRun: true})
	if err == nil || !strings.Contains(err.Error(), "install-type") {
		t.Fatalf("RunVanilla() error = %v, want install-type rejection", err)
	}
}

func TestRunVanillaAllowsExistingVanillaInstallType(t *testing.T) {
	root, server := vanillaFixture(t, "server")
	if err := os.MkdirAll(stateDir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(vanillaInstallTypePath(root), []byte("vanilla\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deps := vanillaRunDeps(t, server)

	if err := runVanilla(context.Background(), deps, Options{TargetDir: root, DryRun: true}); err != nil {
		t.Fatalf("RunVanilla() error = %v", err)
	}
}

func vanillaFixture(t *testing.T, serverContent string) (string, VanillaServer) {
	t.Helper()
	root := t.TempDir()
	artifact := filepath.Join(t.TempDir(), "server.jar")
	if err := os.WriteFile(artifact, []byte(serverContent), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha1.Sum([]byte(serverContent))
	return root, VanillaServer{
		MinecraftVersion: "1.21.8",
		ServerURL:        pathToFileURL(artifact),
		ServerSHA1:       hex.EncodeToString(sum[:]),
		ServerSize:       int64(len(serverContent)),
		JavaMajorVersion: 21,
	}
}

func vanillaVersionJSON(serverURL, sha1 string, size int64) string {
	return vanillaVersionJSONWithJava(serverURL, sha1, size, 25)
}

func vanillaVersionJSONWithJava(serverURL, sha1 string, size int64, javaMajorVersion int) string {
	return fmt.Sprintf(`{
		"javaVersion": {
			"component": "java-runtime-test",
			"majorVersion": %d
		},
		"downloads": {
			"server": {
				"url": %q,
				"sha1": %q,
				"size": %d
			}
		}
	}`, javaMajorVersion, serverURL, sha1, size)
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func vanillaFetchDeps(t *testing.T, responses map[string]string) runtimeDeps {
	t.Helper()

	return runtimeDeps{
		httpClient: &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			body, ok := responses[req.URL.String()]
			if !ok {
				t.Fatalf("unexpected URL %s", req.URL.String())
			}
			return httpResponse(t, []byte(body)), nil
		})},
	}
}

func vanillaRunDeps(t *testing.T, server VanillaServer) runtimeDeps {
	t.Helper()

	deps := vanillaFetchDeps(t, map[string]string{
		vanillaVersionManifestURL: `{
			"latest": {"release": "1.21.8"},
			"versions": [{"id": "1.21.8", "url": "https://example.invalid/release.json"}]
		}`,
		"https://example.invalid/release.json": vanillaVersionJSONWithJava(server.ServerURL, server.ServerSHA1, server.ServerSize, server.JavaMajorVersion),
	})
	deps.lookPath = func(javaPath string) (string, error) {
		return javaPath, nil
	}
	deps.command = func(name string, args ...string) *exec.Cmd {
		if len(args) != 1 || args[0] != "-version" {
			t.Fatalf("command args = %v, want -version", args)
		}
		return helperJavaCommand(t, `openjdk version "25.0.1"`)
	}
	return deps
}
