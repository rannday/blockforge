package serverinstaller

import (
	"fmt"
	"path/filepath"
	"strings"
)

type DryRunPlan struct {
	TargetDir           string
	ManifestSource      string
	Manifest            Manifest
	PreviousVersion     string
	ApplyServerConfig   bool
	ServerConfigReason  string
	LoaderAction        string
	LoaderVersion       string
	OldNeoForgeVersions []string
	ModsToDownload      []ModSpec
	ModsToKeep          []ModSpec
	UnmanagedModPaths   []string
}

func PlanDryRun(targetDir, manifestSource string, manifest Manifest, force bool) (DryRunPlan, error) {
	previousVersion, err := readSavedManifestVersion(targetDir)
	if err != nil {
		return DryRunPlan{}, err
	}
	applyServerConfig := shouldApplyServerConfig(force, previousVersion, manifest.Version)

	loaderAction, oldVersions, err := PlanNeoForgeLoader(targetDir, manifest.Loader, force)
	if err != nil {
		return DryRunPlan{}, err
	}

	modPlan, err := PlanModReconciliation(targetDir, manifest.Mods, force)
	if err != nil {
		return DryRunPlan{}, err
	}

	return DryRunPlan{
		TargetDir:           targetDir,
		ManifestSource:      manifestSource,
		Manifest:            manifest,
		PreviousVersion:     previousVersion,
		ApplyServerConfig:   applyServerConfig,
		ServerConfigReason:  serverConfigPlanReason(force, previousVersion, manifest.Version),
		LoaderAction:        loaderAction,
		LoaderVersion:       manifest.Loader.Version,
		OldNeoForgeVersions: oldVersions,
		ModsToDownload:      modPlan.ModsToDownload,
		ModsToKeep:          modPlan.ModsToKeep,
		UnmanagedModPaths:   modPlan.UnmanagedModPaths,
	}, nil
}

func PlanNeoForgeLoader(targetDir string, desired LoaderManifest, force bool) (string, []string, error) {
	if err := validateLoaderImplemented(desired); err != nil {
		return "", nil, err
	}
	if desired.Type != "neoforge" {
		return "", nil, fmt.Errorf("unsupported manifest loader.type %q", desired.Type)
	}
	if desired.Version == "" {
		return "", nil, fmt.Errorf("manifest loader.version must be non-empty")
	}
	if desired.InstallerURL == "" {
		return "", nil, fmt.Errorf("manifest loader.installer_url must be non-empty")
	}

	installed, err := installedNeoForgeVersions(targetDir)
	if err != nil {
		return "", nil, err
	}

	var oldVersions []string
	desiredInstalled := false
	for _, version := range installed {
		if version == desired.Version {
			desiredInstalled = true
			continue
		}
		oldVersions = append(oldVersions, version)
	}

	switch {
	case len(installed) == 0:
		return "install", oldVersions, nil
	case force:
		return "reinstall", oldVersions, nil
	case desiredInstalled:
		return "skip", oldVersions, nil
	default:
		return "update", oldVersions, nil
	}
}

func formatDryRunPlan(plan DryRunPlan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Dry run:\n")
	fmt.Fprintf(&b, "  target dir:      %s\n", plan.TargetDir)
	fmt.Fprintf(&b, "  manifest source: %s\n", plan.ManifestSource)
	fmt.Fprintf(&b, "  pack:            %s\n", packSummary(plan.Manifest.Pack))
	fmt.Fprintf(&b, "  version:         %s\n", plan.Manifest.Version)
	fmt.Fprintf(&b, "  minecraft:       %s\n", plan.Manifest.Minecraft)
	fmt.Fprintf(&b, "  java.major:      %d\n", plan.Manifest.Java.Major)
	fmt.Fprintf(&b, "  loader:          %s\n", loaderSummary(plan.Manifest.Loader))

	if plan.ApplyServerConfig {
		fmt.Fprintf(&b, "  server config:   apply (%s)\n", plan.ServerConfigReason)
	} else {
		fmt.Fprintf(&b, "  server config:   skip (%s)\n", plan.ServerConfigReason)
	}

	fmt.Fprintf(&b, "  neoforge:        %s %s\n", plan.LoaderAction, plan.LoaderVersion)
	if len(plan.OldNeoForgeVersions) > 0 {
		fmt.Fprintf(&b, "  neoforge cleanup: %s\n", strings.Join(plan.OldNeoForgeVersions, ", "))
	} else {
		fmt.Fprintf(&b, "  neoforge cleanup: none\n")
	}

	fmt.Fprintf(&b, "  mods kept:       %d\n", len(plan.ModsToKeep))
	fmt.Fprintf(&b, "  mods download:   %d\n", len(plan.ModsToDownload))
	fmt.Fprintf(&b, "  mods removals:   %d\n", len(plan.UnmanagedModPaths))

	for _, mod := range plan.ModsToKeep {
		fmt.Fprintf(&b, "    keep: %s (%s)\n", modLabel(mod), mod.FileName)
	}
	for _, mod := range plan.ModsToDownload {
		fmt.Fprintf(&b, "    download/replace: %s (%s)\n", modLabel(mod), mod.FileName)
	}
	for _, path := range plan.UnmanagedModPaths {
		fmt.Fprintf(&b, "    remove: %s\n", filepath.ToSlash(path))
	}

	fmt.Fprintf(&b, "Dry run complete. No files changed.\n")
	return b.String()
}

func serverConfigPlanReason(force bool, previousVersion, manifestVersion string) string {
	if force {
		return "--force"
	}
	if strings.TrimSpace(previousVersion) == "" {
		return "no saved manifest version"
	}
	if strings.TrimSpace(previousVersion) != strings.TrimSpace(manifestVersion) {
		return fmt.Sprintf("saved version %s -> %s", strings.TrimSpace(previousVersion), strings.TrimSpace(manifestVersion))
	}
	return fmt.Sprintf("saved version %s matches", strings.TrimSpace(manifestVersion))
}
