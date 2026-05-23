package serverinstaller

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func packVersionPath(targetDir string) string {
	return stateFile(targetDir, "pack-version.txt")
}

func installerVersionPath(targetDir string) string {
	return stateFile(targetDir, "installer-version.txt")
}

func javaMajorVersionPath(targetDir string) string {
	return stateFile(targetDir, "java-major-version")
}

func cacheDir(targetDir string) string {
	return targetPath(stateDir(targetDir), "cache")
}

func stateDir(targetDir string) string {
	return targetPath(targetDir, ".blockforge")
}

func stateFile(targetDir, name string) string {
	return targetPath(stateDir(targetDir), name)
}

func savedManifestURLPath(targetDir string) string {
	return stateFile(targetDir, "manifest-url")
}

func savedManifestVersionPath(targetDir string) string {
	return stateFile(targetDir, "manifest-version")
}

func resolveManifestSource(targetDir, cliSource string) (string, error) {
	cliSource = strings.TrimSpace(cliSource)
	if cliSource != "" {
		return normalizeManifestSource(cliSource)
	}

	data, err := os.ReadFile(savedManifestURLPath(targetDir))
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("manifest source required on first install; pass --manifest SOURCE")
		}
		return "", fmt.Errorf("read saved manifest source: %w", err)
	}

	saved := strings.TrimSpace(string(data))
	if saved == "" {
		return "", fmt.Errorf("manifest source required on first install; pass --manifest SOURCE")
	}
	return normalizeManifestSource(saved)
}

func normalizeManifestSource(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}

	parsed, err := url.Parse(raw)
	if err == nil && parsed.Scheme != "" {
		switch strings.ToLower(parsed.Scheme) {
		case "http", "https", "file":
			return raw, nil
		}
		if len(parsed.Scheme) != 1 {
			return "", fmt.Errorf("unsupported manifest source scheme: %s", parsed.Scheme)
		}
	}

	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", fmt.Errorf("resolve manifest path: %w", err)
	}
	return pathToFileURL(abs), nil
}

func pathToFileURL(path string) string {
	slashes := filepath.ToSlash(path)
	if len(slashes) >= 2 && slashes[1] == ':' {
		slashes = "/" + slashes
	}
	return (&url.URL{
		Scheme: "file",
		Path:   slashes,
	}).String()
}

func saveManifestURL(targetDir, manifestSource string) error {
	manifestSource = strings.TrimSpace(manifestSource)
	if manifestSource == "" {
		return nil
	}
	if err := os.MkdirAll(stateDir(targetDir), 0o755); err != nil {
		return fmt.Errorf("create blockforge state dir: %w", err)
	}
	if err := os.WriteFile(savedManifestURLPath(targetDir), []byte(manifestSource+"\n"), 0o644); err != nil {
		return fmt.Errorf("write saved manifest source: %w", err)
	}
	return nil
}

func readSavedManifestVersion(targetDir string) (string, error) {
	data, err := os.ReadFile(savedManifestVersionPath(targetDir))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read saved manifest version: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

func writeSavedManifestVersion(targetDir, version string) error {
	version = strings.TrimSpace(version)
	if err := os.MkdirAll(stateDir(targetDir), 0o755); err != nil {
		return fmt.Errorf("create blockforge state dir: %w", err)
	}
	if err := os.WriteFile(savedManifestVersionPath(targetDir), []byte(version+"\n"), 0o644); err != nil {
		return fmt.Errorf("write saved manifest version: %w", err)
	}
	return nil
}

func shouldApplyServerConfig(force bool, savedVersion, manifestVersion string) bool {
	return force || strings.TrimSpace(savedVersion) != strings.TrimSpace(manifestVersion)
}

func WriteInstallDiagnostics(targetDir string, manifest Manifest) error {
	if err := os.MkdirAll(stateDir(targetDir), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(packVersionPath(targetDir), []byte(manifest.Version+"\n"), 0o644); err != nil {
		return fmt.Errorf("write pack version: %w", err)
	}
	if err := os.WriteFile(javaMajorVersionPath(targetDir), []byte(fmt.Sprintf("%d\n", manifest.Java.Major)), 0o644); err != nil {
		return fmt.Errorf("write java major version: %w", err)
	}
	if err := os.WriteFile(installerVersionPath(targetDir), []byte(Version+"\n"), 0o644); err != nil {
		return fmt.Errorf("write installer version: %w", err)
	}
	return nil
}

func FetchRemoteManifest(rawURL string) (Manifest, error) {
	return fetchRemoteManifest(context.Background(), defaultDeps, rawURL)
}

func fetchRemoteManifest(ctx context.Context, deps runtimeDeps, rawURL string) (Manifest, error) {
	deps = deps.withDefaults()
	raw, err := downloadURLToBytesCtx(ctx, deps, rawURL)
	if err != nil {
		return Manifest{}, fmt.Errorf("fetch manifest: %w", err)
	}

	manifest, err := LoadManifestFromBytes(raw)
	if err != nil {
		return Manifest{}, err
	}

	return manifest, nil
}

func manifestCheckSummary(manifest Manifest) string {
	return fmt.Sprintf(
		"Manifest check:\n  schema:         %d\n  pack:           %s\n  version:        %s\n  minecraft:      %s\n  java.major:     %d\n  loader:         %s\n  mods:           %d (sha1)\n  server config:  %s\n",
		manifest.SchemaVersion,
		packSummary(manifest.Pack),
		manifest.Version,
		manifest.Minecraft,
		manifest.Java.Major,
		loaderSummary(manifest.Loader),
		len(manifest.Mods),
		serverConfigSummary(manifest.ServerConfig),
	)
}

func packSummary(pack string) string {
	if pack == "" {
		return "(none)"
	}
	return pack
}

func loaderSummary(loader LoaderManifest) string {
	if loader.Type == "" {
		return loader.Version
	}
	return loader.Type + " " + loader.Version
}

func serverConfigSummary(cfg *ServerConfigManifest) string {
	if cfg != nil && cfg.URL != "" {
		if cfg.SHA1 != "" {
			return "present (sha1)"
		}
		return "present"
	}
	return "absent"
}
