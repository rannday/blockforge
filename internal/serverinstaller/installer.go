// Package serverinstaller provides the Blockforge Minecraft server installer and updater.
package serverinstaller

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

type Options struct {
	Force           bool
	DryRun          bool
	Vanilla         bool
	DownloadWorkers int
	TargetDir       string
	ManifestSource  string
	CheckManifest   bool
	VersionOnly     bool
	Help            bool

	manifestSet      bool
	checkManifestSet bool
	workersSet       bool
}

func Run(args []string) error {
	opts, err := parseOptions(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	if opts.VersionOnly {
		fmt.Println(Version)
		return nil
	}

	if opts.TargetDir == "" {
		opts.TargetDir = "."
	}

	if opts.Vanilla {
		return RunVanilla(opts)
	}

	manifestSource, err := resolveManifestSource(opts.TargetDir, opts.ManifestSource)
	if err != nil {
		return err
	}

	manifest, err := FetchRemoteManifest(manifestSource)
	if err != nil {
		return err
	}
	if err := manifest.Validate(); err != nil {
		return err
	}
	if err := validateLoaderImplemented(manifest.Loader); err != nil {
		return err
	}

	if opts.CheckManifest {
		fmt.Print(manifestCheckSummary(manifest))
		fmt.Println("Check complete. No files changed.")
		return nil
	}

	if opts.DryRun {
		plan, err := PlanDryRun(opts.TargetDir, manifestSource, manifest, opts.Force)
		if err != nil {
			return err
		}
		fmt.Print(formatDryRunPlan(plan))
		return nil
	}

	if err := RequireJava21(); err != nil {
		return err
	}

	if err := os.MkdirAll(opts.TargetDir, 0o755); err != nil {
		return fmt.Errorf("create target dir: %w", err)
	}

	if opts.ManifestSource != "" {
		if err := saveManifestURL(opts.TargetDir, manifestSource); err != nil {
			return err
		}
	}

	previousVersion, err := readSavedManifestVersion(opts.TargetDir)
	if err != nil {
		return err
	}
	applyServerConfig := shouldApplyServerConfig(opts.Force, previousVersion, manifest.Version)
	if applyServerConfig {
		if err := installServerConfig(opts.TargetDir, manifest); err != nil {
			return err
		}
	} else {
		fmt.Printf("Server config already applied for manifest version %s; skipping.\n", manifest.Version)
	}

	loaderVersion, err := InstallOrUpdateLoader(opts.TargetDir, manifest.Loader, opts.Force)
	if err != nil {
		return err
	}

	if err := WriteJvmArgs(opts.TargetDir, opts.Force); err != nil {
		return err
	}
	if err := PatchLaunchers(opts.TargetDir, loaderVersion); err != nil {
		return err
	}
	if err := cleanupNeoForgeInstallerArtifacts(opts.TargetDir); err != nil {
		return err
	}

	if err := ReconcileMods(opts.TargetDir, manifest.Mods, opts.Force, opts.DownloadWorkers); err != nil {
		return err
	}

	if err := WriteInstallDiagnostics(opts.TargetDir, manifest); err != nil {
		return err
	}
	if err := writeSavedManifestVersion(opts.TargetDir, manifest.Version); err != nil {
		return err
	}

	fmt.Println("Setup complete.")
	return nil
}

func parseOptions(args []string) (Options, error) {
	var opts Options

	fs := flag.NewFlagSet("blockforge", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {
		printInstallerUsage(os.Stdout)
	}
	fs.BoolVar(&opts.Help, "help", false, "print help")
	fs.BoolVar(&opts.Help, "h", false, "print help")
	fs.BoolVar(&opts.VersionOnly, "version", false, "print installer version and exit")
	fs.BoolVar(&opts.VersionOnly, "v", false, "print installer version and exit")
	fs.BoolVar(&opts.CheckManifest, "check-manifest", false, "validate the manifest and print a summary without changing files")
	fs.BoolVar(&opts.CheckManifest, "c", false, "validate the manifest and print a summary without changing files")
	fs.BoolVar(&opts.Vanilla, "vanilla", false, "install or update latest vanilla Minecraft server")
	fs.BoolVar(&opts.DryRun, "dry-run", false, "show planned changes without modifying files")
	fs.StringVar(&opts.TargetDir, "dir", ".", "server install directory")
	fs.StringVar(&opts.TargetDir, "d", ".", "server install directory")
	fs.BoolVar(&opts.Force, "force", false, "re-download/reinstall files")
	fs.BoolVar(&opts.Force, "f", false, "re-download/reinstall files")
	fs.StringVar(&opts.ManifestSource, "manifest", "", "manifest source URL or local path")
	fs.StringVar(&opts.ManifestSource, "m", "", "manifest source URL or local path")
	fs.StringVar(&opts.ManifestSource, "manifest-url", "", "deprecated manifest URL alias")
	fs.IntVar(&opts.DownloadWorkers, "workers", 6, "mod download worker count")
	fs.IntVar(&opts.DownloadWorkers, "w", 6, "mod download worker count")
	fs.IntVar(&opts.DownloadWorkers, "download-workers", 6, "mod download worker count")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.Usage()
			return opts, flag.ErrHelp
		}
		return opts, err
	}

	if opts.Help {
		fs.Usage()
		return opts, flag.ErrHelp
	}

	opts.manifestSet = flagWasSupplied(args, "manifest", "m", "manifest-url")
	opts.checkManifestSet = flagWasSupplied(args, "check-manifest", "c")
	opts.workersSet = flagWasSupplied(args, "workers", "w", "download-workers")

	if opts.Vanilla {
		if opts.manifestSet {
			return opts, fmt.Errorf("--vanilla cannot be combined with --manifest")
		}
		if opts.checkManifestSet {
			return opts, fmt.Errorf("--vanilla cannot be combined with --check-manifest")
		}
		if opts.workersSet {
			return opts, fmt.Errorf("--vanilla cannot be combined with --workers")
		}
	}
	if opts.DownloadWorkers < 1 || opts.DownloadWorkers > 16 {
		return opts, fmt.Errorf("--workers must be between 1 and 16")
	}
	if opts.CheckManifest && opts.DryRun {
		return opts, fmt.Errorf("--check-manifest and --dry-run cannot be combined")
	}

	return opts, nil
}

func flagWasSupplied(args []string, names ...string) bool {
	want := make(map[string]struct{}, len(names))
	for _, name := range names {
		want["-"+name] = struct{}{}
		want["--"+name] = struct{}{}
	}
	for _, arg := range args {
		if i := strings.IndexByte(arg, '='); i >= 0 {
			arg = arg[:i]
		}
		if _, ok := want[arg]; ok {
			return true
		}
	}
	return false
}

func printInstallerUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: blockforge [options]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Install or update a Minecraft server.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Options:")
	fmt.Fprintln(w, "  -m, --manifest SOURCE      Manifest source URL or local path (required on first install)")
	fmt.Fprintln(w, "  -d, --dir DIR              Server install directory (default: .)")
	fmt.Fprintln(w, "  -c, --check-manifest       Validate manifest and print a summary without changing files")
	fmt.Fprintln(w, "      --vanilla              Install/update latest recommended vanilla release")
	fmt.Fprintln(w, "      --dry-run              Show planned changes without modifying files")
	fmt.Fprintln(w, "  -f, --force                Re-download/reinstall files instead of keeping existing files")
	fmt.Fprintln(w, "  -w, --workers N            Concurrent mod download workers (default: 6, range: 1-16)")
	fmt.Fprintln(w, "  -v, --version              Print installer version and exit")
	fmt.Fprintln(w, "  -h, --help                 Show this help text and exit")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "First install requires --manifest. Later runs reuse the saved source from .blockforge/manifest-url.")
}
