package serverinstaller

import (
	"fmt"
	"strings"
)

type VanillaDryRunPlan struct {
	Plan             InstallPlan
	MinecraftVersion string
	JavaMajorVersion int
	ServerJar        string
	Launchers        string
	JVMArgs          string
	State            string
}

func PlanVanillaDryRun(targetDir string, server VanillaServer, javaPath string, force bool) (VanillaDryRunPlan, error) {
	if err := validateVanillaTargetDir(targetDir); err != nil {
		return VanillaDryRunPlan{}, err
	}

	serverJar, err := planVanillaServerJar(targetDir, server.ServerSHA1, force)
	if err != nil {
		return VanillaDryRunPlan{}, err
	}
	launchers, err := planVanillaLaunchers(targetDir, javaPath)
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
		Plan: InstallPlan{
			Mode: "vanilla",
			Summary: []PlanSummaryItem{
				{Kind: "target dir", Value: targetDir},
				{Kind: "minecraft", Value: server.MinecraftVersion},
				{Kind: "java", Value: fmt.Sprintf("%d+", server.JavaMajorVersion)},
			},
		},
		MinecraftVersion: server.MinecraftVersion,
		JavaMajorVersion: server.JavaMajorVersion,
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
	fmt.Fprintf(&b, "  java:           %d+\n", plan.JavaMajorVersion)
	fmt.Fprintf(&b, "  server jar:     %s\n", plan.ServerJar)
	fmt.Fprintf(&b, "  launchers:      %s\n", plan.Launchers)
	fmt.Fprintf(&b, "  jvm args:       %s\n", plan.JVMArgs)
	fmt.Fprintf(&b, "  state:          %s\n", plan.State)
	fmt.Fprintf(&b, "No files changed.\n")
	return b.String()
}

func planVanillaServerJar(targetDir, expectedSHA1 string, force bool) (string, error) {
	action, err := classifyManagedFile(targetPath(targetDir, "server.jar"), FileCheck{SHA1: expectedSHA1}, force)
	if err != nil {
		return "", err
	}
	switch action {
	case FileActionMissing:
		return "missing", nil
	case FileActionCurrent:
		return "current", nil
	default:
		if force {
			return "would redownload due to --force", nil
		}
		return "would replace", nil
	}
}

func planVanillaLaunchers(targetDir, javaPath string) (string, error) {
	sh, err := classifyManagedContentFile(targetPath(targetDir, "run.sh"), vanillaRunSh(javaPath), false)
	if err != nil {
		return "", err
	}
	bat, err := classifyManagedContentFile(targetPath(targetDir, "run.bat"), vanillaRunBat(javaPath), false)
	if err != nil {
		return "", err
	}
	if sh == FileActionCurrent && bat == FileActionCurrent {
		return "current", nil
	}
	if sh == FileActionMissing || bat == FileActionMissing {
		return "would write", nil
	}
	return "would update", nil
}

func planVanillaJVMArgs(targetDir string, force bool) (string, error) {
	action, err := classifyManagedContentFile(targetPath(targetDir, "user_jvm_args.txt"), jvmArgsContent(), force)
	if err != nil {
		return "", err
	}
	switch action {
	case FileActionMissing:
		return "would write", nil
	case FileActionCurrent:
		return "current", nil
	default:
		if force {
			return "would update due to --force", nil
		}
		return "would update", nil
	}
}

func planVanillaState(targetDir string, server VanillaServer) (string, error) {
	expected := vanillaStateFiles(server)
	anyMissing := false
	for name, content := range expected {
		action, err := classifyManagedContentFile(stateFile(targetDir, name), content, false)
		if err != nil {
			return "", err
		}
		switch action {
		case FileActionMissing:
			anyMissing = true
		case FileActionReplace:
			return "would update", nil
		}
	}
	if anyMissing {
		return "would write", nil
	}
	return "current", nil
}
