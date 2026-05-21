package serverinstaller

import (
	"fmt"
	"os"
	"strings"
)

type VanillaDryRunPlan struct {
	MinecraftVersion string
	ServerJar        string
	Launchers        string
	JVMArgs          string
	State            string
}

func PlanVanillaDryRun(targetDir string, server VanillaServer, force bool) (VanillaDryRunPlan, error) {
	if err := validateVanillaTargetDir(targetDir); err != nil {
		return VanillaDryRunPlan{}, err
	}

	serverJar, err := planVanillaServerJar(targetDir, server.ServerSHA1, force)
	if err != nil {
		return VanillaDryRunPlan{}, err
	}
	launchers, err := planVanillaLaunchers(targetDir)
	if err != nil {
		return VanillaDryRunPlan{}, err
	}
	jvmArgs, err := planVanillaJVMArgs(targetDir, force)
	if err != nil {
		return VanillaDryRunPlan{}, err
	}
	state, err := planVanillaState(targetDir, server)
	if err != nil {
		return VanillaDryRunPlan{}, err
	}

	return VanillaDryRunPlan{
		MinecraftVersion: server.MinecraftVersion,
		ServerJar:        serverJar,
		Launchers:        launchers,
		JVMArgs:          jvmArgs,
		State:            state,
	}, nil
}

func formatVanillaDryRunPlan(plan VanillaDryRunPlan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Vanilla dry run:\n")
	fmt.Fprintf(&b, "  minecraft:      %s\n", plan.MinecraftVersion)
	fmt.Fprintf(&b, "  server jar:     %s\n", plan.ServerJar)
	fmt.Fprintf(&b, "  launchers:      %s\n", plan.Launchers)
	fmt.Fprintf(&b, "  jvm args:       %s\n", plan.JVMArgs)
	fmt.Fprintf(&b, "  state:          %s\n", plan.State)
	fmt.Fprintf(&b, "No files changed.\n")
	return b.String()
}

func planVanillaServerJar(targetDir, expectedSHA1 string, force bool) (string, error) {
	path := targetPath(targetDir, "server.jar")
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "missing", nil
		}
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("target path is a directory: %s", path)
	}
	if force {
		return "would redownload due to --force", nil
	}
	actual, err := sha1File(path)
	if err != nil {
		return "", err
	}
	if strings.EqualFold(actual, expectedSHA1) {
		return "current", nil
	}
	return "would replace", nil
}

func planVanillaLaunchers(targetDir string) (string, error) {
	sh, err := fileContentStatus(targetPath(targetDir, "run.sh"), vanillaRunSh)
	if err != nil {
		return "", err
	}
	bat, err := fileContentStatus(targetPath(targetDir, "run.bat"), vanillaRunBat)
	if err != nil {
		return "", err
	}
	if sh == "current" && bat == "current" {
		return "current", nil
	}
	if sh == "missing" || bat == "missing" {
		return "would write", nil
	}
	return "would update", nil
}

func planVanillaJVMArgs(targetDir string, force bool) (string, error) {
	path := targetPath(targetDir, "user_jvm_args.txt")
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "would write", nil
		}
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("target path is a directory: %s", path)
	}
	if force {
		return "would update due to --force", nil
	}
	return "current", nil
}

func planVanillaState(targetDir string, server VanillaServer) (string, error) {
	expected := map[string]string{
		"install-type":          "vanilla\n",
		"minecraft-version":     server.MinecraftVersion + "\n",
		"server-jar-sha1":       server.ServerSHA1 + "\n",
		"installer-version.txt": Version + "\n",
	}
	anyMissing := false
	for name, content := range expected {
		status, err := fileContentStatus(stateFile(targetDir, name), content)
		if err != nil {
			return "", err
		}
		switch status {
		case "missing":
			anyMissing = true
		case "different":
			return "would update", nil
		}
	}
	if anyMissing {
		return "would write", nil
	}
	return "current", nil
}

func fileContentStatus(path, expected string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "missing", nil
		}
		return "", err
	}
	if string(data) == expected {
		return "current", nil
	}
	return "different", nil
}
