//go:build integration

package blockforge_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rannday/blockforge/internal/serverinstaller"
)

func TestInstallAndUpdateVanillaServer(t *testing.T) {
	server, err := serverinstaller.FetchLatestVanillaServer()
	if err != nil {
		t.Fatalf("FetchLatestVanillaServer() error = %v", err)
	}

	installDir := integrationTempDir(t, "vanilla", "server")
	if err := serverinstaller.Run([]string{"--vanilla", "--dir", installDir}); err != nil {
		requiredJava := fmt.Sprintf("Java %d+", server.JavaMajorVersion)
		if strings.Contains(err.Error(), requiredJava) || strings.Contains(err.Error(), "java was not found") {
			t.Skipf("%s unavailable: %v", requiredJava, err)
		}
		t.Fatalf("first Run() error = %v", err)
	}

	for _, path := range []string{
		filepath.Join(installDir, "server.jar"),
		filepath.Join(installDir, "run.sh"),
		filepath.Join(installDir, "run.bat"),
		filepath.Join(installDir, "user_jvm_args.txt"),
		filepath.Join(installDir, ".blockforge", "install-type"),
		filepath.Join(installDir, ".blockforge", "minecraft-version"),
		filepath.Join(installDir, ".blockforge", "server-jar-sha1"),
		filepath.Join(installDir, ".blockforge", "java-major-version"),
	} {
		assertPathExists(t, path)
	}

	assertFileSHA1(t, filepath.Join(installDir, "server.jar"), server.ServerSHA1)
	if got := strings.TrimSpace(readFile(t, filepath.Join(installDir, ".blockforge", "install-type"))); got != "vanilla" {
		t.Fatalf("install-type = %q, want vanilla", got)
	}
	if got := strings.TrimSpace(readFile(t, filepath.Join(installDir, ".blockforge", "minecraft-version"))); got != server.MinecraftVersion {
		t.Fatalf("minecraft-version = %q, want %q", got, server.MinecraftVersion)
	}
	if got := strings.TrimSpace(readFile(t, filepath.Join(installDir, ".blockforge", "server-jar-sha1"))); got != server.ServerSHA1 {
		t.Fatalf("server-jar-sha1 = %q, want %q", got, server.ServerSHA1)
	}
	if got := strings.TrimSpace(readFile(t, filepath.Join(installDir, ".blockforge", "java-major-version"))); got != fmt.Sprint(server.JavaMajorVersion) {
		t.Fatalf("java-major-version = %q, want %d", got, server.JavaMajorVersion)
	}

	before, err := os.Stat(filepath.Join(installDir, "server.jar"))
	if err != nil {
		t.Fatal(err)
	}
	if err := serverinstaller.Run([]string{"--vanilla", "--dir", installDir}); err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	after, err := os.Stat(filepath.Join(installDir, "server.jar"))
	if err != nil {
		t.Fatal(err)
	}
	if after.Size() != before.Size() {
		t.Fatalf("server.jar size changed on idempotent update: got %d want %d", after.Size(), before.Size())
	}
	assertFileSHA1(t, filepath.Join(installDir, "server.jar"), server.ServerSHA1)
}
