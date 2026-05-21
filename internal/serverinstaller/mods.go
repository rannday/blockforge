package serverinstaller

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type ModSpec struct {
	Name     string
	FileName string
	URL      string
	SHA1     string
	Size     int64
}

type ModReconcilePlan struct {
	ModsToDownload    []ModSpec
	ModsToKeep        []ModSpec
	UnmanagedModPaths []string
}

func ReconcileMods(targetDir string, mods []ManifestMod, force bool, workerCount int) error {
	if workerCount < 1 || workerCount > 16 {
		return fmt.Errorf("download worker count must be between 1 and 16")
	}

	plan, err := PlanModReconciliation(targetDir, mods, force)
	if err != nil {
		return err
	}

	modsPath := modsDir(targetDir)
	if err := os.MkdirAll(modsPath, 0o755); err != nil {
		return err
	}

	for _, mod := range plan.ModsToKeep {
		fmt.Printf("Keeping current mod: %s\n", modLabel(mod))
	}

	if err := downloadMods(targetDir, plan.ModsToDownload, workerCount); err != nil {
		return err
	}

	for _, path := range plan.UnmanagedModPaths {
		name := filepath.Base(path)
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if info.IsDir() {
			fmt.Printf("Removing unmanaged directory from mods/: %s\n", name)
			if err := os.RemoveAll(path); err != nil {
				return err
			}
			continue
		}

		fmt.Printf("Removing unmanaged file from mods/: %s\n", name)
		if err := os.Remove(path); err != nil {
			return err
		}
	}

	return nil
}

func PlanModReconciliation(targetDir string, mods []ManifestMod, force bool) (ModReconcilePlan, error) {
	if len(mods) == 0 {
		return ModReconcilePlan{}, fmt.Errorf("manifest does not contain any server mod jars")
	}

	specs := make([]ModSpec, 0, len(mods))
	seen := make(map[string]string, len(mods))
	for _, mod := range mods {
		spec, err := modSpecFromManifestMod(mod)
		if err != nil {
			return ModReconcilePlan{}, err
		}
		key := strings.ToLower(spec.FileName)
		if existing, ok := seen[key]; ok {
			return ModReconcilePlan{}, fmt.Errorf("duplicate mod filename in manifest: %s (%s and %s)", spec.FileName, existing, modLabel(spec))
		}
		seen[key] = modLabel(spec)
		specs = append(specs, spec)
	}

	sort.Slice(specs, func(i, j int) bool {
		if strings.EqualFold(specs[i].FileName, specs[j].FileName) {
			return specs[i].URL < specs[j].URL
		}
		return strings.ToLower(specs[i].FileName) < strings.ToLower(specs[j].FileName)
	})

	modsPath := modsDir(targetDir)
	desired := make(map[string]ModSpec, len(specs))
	var plan ModReconcilePlan
	var downloads []ModSpec
	for _, mod := range specs {
		desired[strings.ToLower(mod.FileName)] = mod
		target := filepath.Join(modsPath, mod.FileName)
		info, err := os.Stat(target)
		if err == nil {
			if info.IsDir() {
				return ModReconcilePlan{}, fmt.Errorf("target mod path is a directory: %s", target)
			}
			if force {
				downloads = append(downloads, mod)
				continue
			}
			if info.Size() == 0 {
				downloads = append(downloads, mod)
				continue
			}
			if mod.Size > 0 && info.Size() != mod.Size {
				downloads = append(downloads, mod)
				continue
			}
			actualSHA1, err := sha1File(target)
			if err != nil {
				return ModReconcilePlan{}, err
			}
			if strings.EqualFold(actualSHA1, mod.SHA1) {
				plan.ModsToKeep = append(plan.ModsToKeep, mod)
				continue
			}
		}
		if err != nil && !os.IsNotExist(err) {
			return ModReconcilePlan{}, err
		}
		downloads = append(downloads, mod)
	}
	plan.ModsToDownload = downloads

	entries, err := os.ReadDir(modsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return plan, nil
		}
		return ModReconcilePlan{}, err
	}

	known := make(map[string]struct{}, len(desired))
	for key := range desired {
		known[key] = struct{}{}
	}

	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(modsPath, name)
		if _, ok := known[strings.ToLower(name)]; ok {
			continue
		}
		plan.UnmanagedModPaths = append(plan.UnmanagedModPaths, path)
	}
	sort.Strings(plan.UnmanagedModPaths)

	return plan, nil
}

func downloadMods(targetDir string, mods []ModSpec, workerCount int) error {
	if len(mods) == 0 {
		return nil
	}

	modsPath := modsDir(targetDir)
	jobs := make(chan ModSpec)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []error

	worker := func() {
		defer wg.Done()
		for mod := range jobs {
			target := filepath.Join(modsPath, mod.FileName)
			if err := downloadToFile(mod.URL, target, true, modLabel(mod), DownloadChecks{SHA1: mod.SHA1, Size: mod.Size}); err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("%s: %w", modLabel(mod), err))
				mu.Unlock()
			}
		}
	}

	wg.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go worker()
	}

	for _, mod := range mods {
		jobs <- mod
	}
	close(jobs)
	wg.Wait()

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

func modLabel(mod ModSpec) string {
	if mod.Name != "" {
		return mod.Name
	}
	return mod.FileName
}

func modSpecFromManifestMod(mod ManifestMod) (ModSpec, error) {
	label := strings.TrimSpace(mod.Name)
	if label == "" {
		label = strings.TrimSpace(mod.URL)
		if label == "" {
			label = "manifest mod"
		}
	}
	if strings.TrimSpace(mod.Name) == "" {
		return ModSpec{}, fmt.Errorf("%s: manifest mod name must be non-empty", label)
	}
	if strings.TrimSpace(mod.WebsiteURL) == "" {
		return ModSpec{}, fmt.Errorf("%s: manifest mod website_url must be non-empty", label)
	}
	if mod.Size <= 0 {
		return ModSpec{}, fmt.Errorf("%s: manifest mod size must be positive when present", label)
	}
	if err := validateURLScheme(mod.URL, "manifest mod url", "http", "https"); err != nil {
		return ModSpec{}, fmt.Errorf("%s: %w", label, err)
	}
	filename, err := inferFilenameFromURL(mod.URL)
	if err != nil {
		return ModSpec{}, fmt.Errorf("%s: %w", label, err)
	}
	if !isSafeModFilename(filename) {
		return ModSpec{}, fmt.Errorf("%s: unsafe or non-jar mod filename in manifest: %s", label, filename)
	}
	if !strings.HasSuffix(strings.ToLower(filename), ".jar") {
		return ModSpec{}, fmt.Errorf("%s: manifest mod url must end with .jar: %s", label, filename)
	}
	if err := validateURLScheme(mod.WebsiteURL, "manifest mod website_url", "http", "https"); err != nil {
		return ModSpec{}, fmt.Errorf("%s: %w", label, err)
	}
	if mod.SHA1 == "" {
		return ModSpec{}, fmt.Errorf("%s: manifest mod sha1 must be non-empty", label)
	}
	if err := validateSHA1Hex(mod.SHA1, fmt.Sprintf("manifest mod sha1 for %s", label)); err != nil {
		return ModSpec{}, err
	}
	return ModSpec{Name: mod.Name, FileName: filename, URL: mod.URL, SHA1: mod.SHA1, Size: mod.Size}, nil
}
