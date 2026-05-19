package serverinstaller

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	maxServerConfigFiles           = 10000
	maxServerConfigSingleFileBytes = 256 << 20
	maxServerConfigTotalBytes      = 2 << 30
)

type zipExtractLimits struct {
	MaxFiles           int
	MaxSingleFileBytes int64
	MaxTotalBytes      int64
}

var serverConfigZipLimits = zipExtractLimits{
	MaxFiles:           maxServerConfigFiles,
	MaxSingleFileBytes: maxServerConfigSingleFileBytes,
	MaxTotalBytes:      maxServerConfigTotalBytes,
}

func installServerConfig(targetDir string, manifest Manifest) error {
	if manifest.ServerConfig == nil || manifest.ServerConfig.URL == "" {
		return nil
	}

	if err := os.MkdirAll(cacheDir(targetDir), 0o755); err != nil {
		return err
	}

	cachePath := filepath.Join(cacheDir(targetDir), "server-config.zip")
	if err := downloadToFile(manifest.ServerConfig.URL, cachePath, true, "server config", DownloadChecks{SHA1: manifest.ServerConfig.SHA1}); err != nil {
		return err
	}

	return extractZipFile(cachePath, targetDir)
}

func extractZipFile(zipPath, targetDir string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer reader.Close()

	rootAbs, err := filepath.Abs(targetDir)
	if err != nil {
		return err
	}
	rootAbs = filepath.Clean(rootAbs)

	var fileCount int
	var totalBytes int64
	for _, entry := range reader.File {
		if !entry.FileInfo().IsDir() {
			fileCount++
			if fileCount > serverConfigZipLimits.MaxFiles {
				return fmt.Errorf("server config zip contains too many files: %d > %d", fileCount, serverConfigZipLimits.MaxFiles)
			}
		}
		if err := extractZipEntry(entry, rootAbs, &totalBytes, serverConfigZipLimits); err != nil {
			return err
		}
	}

	return nil
}

func extractZipEntry(entry *zip.File, rootAbs string, totalBytes *int64, limits zipExtractLimits) error {
	name := entry.Name
	if name == "" {
		return nil
	}
	if strings.Contains(name, "\\") {
		return fmt.Errorf("zip entry uses backslash path separators: %s", name)
	}
	if path.IsAbs(name) {
		return fmt.Errorf("zip entry is absolute: %s", name)
	}
	if filepath.IsAbs(filepath.FromSlash(name)) {
		return fmt.Errorf("zip entry is absolute: %s", name)
	}

	cleaned := path.Clean(name)
	if cleaned == "." {
		return nil
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return fmt.Errorf("zip entry escapes target dir: %s", name)
	}

	target := filepath.Join(rootAbs, filepath.FromSlash(cleaned))
	rel, err := filepath.Rel(rootAbs, target)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("zip entry escapes target dir: %s", name)
	}

	if entry.FileInfo().Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("zip entry is a symlink: %s", name)
	}

	if entry.FileInfo().IsDir() {
		return os.MkdirAll(target, 0o755)
	}
	if entry.UncompressedSize64 > uint64(limits.MaxSingleFileBytes) {
		return fmt.Errorf("zip entry exceeds single-file limit of %d bytes: %s", limits.MaxSingleFileBytes, name)
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}

	src, err := entry.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	tmp, err := os.CreateTemp(filepath.Dir(target), filepath.Base(target)+".zip-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := copyZipEntryWithLimits(tmp, src, name, totalBytes, limits); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	return replaceFile(tmpName, target)
}

func copyZipEntryWithLimits(dst io.Writer, src io.Reader, name string, totalBytes *int64, limits zipExtractLimits) error {
	buf := make([]byte, 32*1024)
	var entryBytes int64
	for {
		n, readErr := src.Read(buf)
		if n > 0 {
			nextEntryBytes := entryBytes + int64(n)
			if nextEntryBytes > limits.MaxSingleFileBytes {
				return fmt.Errorf("zip entry exceeds single-file limit of %d bytes: %s", limits.MaxSingleFileBytes, name)
			}
			nextTotalBytes := *totalBytes + int64(n)
			if nextTotalBytes > limits.MaxTotalBytes {
				return fmt.Errorf("server config zip exceeds total extracted limit of %d bytes", limits.MaxTotalBytes)
			}
			if _, err := dst.Write(buf[:n]); err != nil {
				return err
			}
			entryBytes = nextEntryBytes
			*totalBytes = nextTotalBytes
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}
