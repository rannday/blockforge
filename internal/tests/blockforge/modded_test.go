//go:build integration

package blockforge_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/rannday/blockforge/internal/serverinstaller"
)

func TestInstallAndUpdateFromPortableManifestFixture(t *testing.T) {
	fixtureDir := fixtureDataDir(t)
	manifestPath := patchPortableManifest(t, fixtureDir, integrationTempDir(t, "modded", "manifest"))
	installDir := integrationTempDir(t, "modded", "server")

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
