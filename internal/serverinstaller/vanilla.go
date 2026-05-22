package serverinstaller

import (
	"fmt"
	"os"
	"strings"
)

var requireJava = RequireJava
var downloadVanillaFile = downloadToFile

func RunVanilla(opts Options) error {
	if err := validateVanillaTargetDir(opts.TargetDir); err != nil {
		return err
	}

	server, err := FetchLatestVanillaServer()
	if err != nil {
		return err
	}

	if opts.DryRun {
		plan, err := PlanVanillaDryRun(opts.TargetDir, server, opts.JavaPath, opts.Force)
		if err != nil {
			return err
		}
		fmt.Print(formatVanillaDryRunPlan(plan))
		return nil
	}

	if err := requireJava(opts.JavaPath, server.JavaMajorVersion); err != nil {
		return err
	}

	if err := os.MkdirAll(opts.TargetDir, 0o755); err != nil {
		return fmt.Errorf("create target dir: %w", err)
	}

	serverJar := targetPath(opts.TargetDir, "server.jar")
	if err := downloadVanillaFile(
		server.ServerURL,
		serverJar,
		opts.Force,
		"vanilla server jar",
		DownloadChecks{SHA1: server.ServerSHA1, Size: server.ServerSize},
	); err != nil {
		return err
	}

	if err := WriteVanillaLaunchers(opts.TargetDir, opts.JavaPath); err != nil {
		return err
	}
	if err := WriteJvmArgs(opts.TargetDir, opts.Force); err != nil {
		return err
	}
	if err := WriteVanillaState(opts.TargetDir, server); err != nil {
		return err
	}

	fmt.Println("Setup complete.")
	return nil
}

func validateVanillaTargetDir(targetDir string) error {
	if _, err := os.Stat(savedManifestURLPath(targetDir)); err == nil {
		return fmt.Errorf("target directory appears to be manifest-managed/modded; --vanilla cannot be used there")
	} else if !os.IsNotExist(err) {
		return err
	}

	data, err := os.ReadFile(vanillaInstallTypePath(targetDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if strings.TrimSpace(string(data)) != "vanilla" {
		return fmt.Errorf("target directory install-type is %q; --vanilla can only update vanilla installs", strings.TrimSpace(string(data)))
	}
	return nil
}

func WriteVanillaState(targetDir string, server VanillaServer) error {
	if err := os.MkdirAll(stateDir(targetDir), 0o755); err != nil {
		return fmt.Errorf("create blockforge state dir: %w", err)
	}
	files := map[string]string{
		"install-type":          "vanilla\n",
		"minecraft-version":     server.MinecraftVersion + "\n",
		"java-major-version":    fmt.Sprintf("%d\n", server.JavaMajorVersion),
		"server-jar-sha1":       server.ServerSHA1 + "\n",
		"installer-version.txt": Version + "\n",
	}
	for name, content := range files {
		if err := os.WriteFile(stateFile(targetDir, name), []byte(content), 0o644); err != nil {
			return fmt.Errorf("write vanilla state %s: %w", name, err)
		}
	}
	return nil
}

func vanillaInstallTypePath(targetDir string) string {
	return stateFile(targetDir, "install-type")
}
