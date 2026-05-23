package serverinstaller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	maxManifestSize    = 5 << 20
	maxDownloadTries   = 2
	httpErrorBodyLimit = 1 << 20
)

type downloadStatusError struct {
	StatusCode int
	Status     string
	Body       string
}

func (e *downloadStatusError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("download failed: HTTP %d %s", e.StatusCode, e.Status)
	}
	return fmt.Sprintf("download failed: HTTP %d %s\n%s", e.StatusCode, e.Status, e.Body)
}

type DownloadChecks struct {
	SHA1 string
	Size int64
}

func downloadURLToBytes(rawURL string) ([]byte, error) {
	return downloadURLToBytesCtx(context.Background(), defaultDeps, rawURL)
}

func downloadURLToBytesCtx(ctx context.Context, deps runtimeDeps, rawURL string) ([]byte, error) {
	deps = deps.withDefaults()
	data, err := downloadWithRetry(ctx, deps, rawURL, func(source io.ReadCloser) ([]byte, error) {
		defer source.Close()

		limited := io.LimitReader(source, maxManifestSize+1)
		data, err := io.ReadAll(limited)
		if err != nil {
			return nil, err
		}
		if len(data) > maxManifestSize {
			return nil, fmt.Errorf("manifest exceeds maximum size of %d bytes", maxManifestSize)
		}
		return data, nil
	})
	return data, err
}

func downloadToFile(rawURL, targetPath string, force bool, label string, checks ...DownloadChecks) error {
	return downloadToFileCtx(context.Background(), defaultDeps, rawURL, targetPath, force, label, checks...)
}

func downloadToFileCtx(ctx context.Context, deps runtimeDeps, rawURL, targetPath string, force bool, label string, checks ...DownloadChecks) error {
	deps = deps.withDefaults()
	var check DownloadChecks
	if len(checks) > 0 {
		check = checks[0]
	}

	action, err := classifyManagedFile(targetPath, FileCheck{SHA1: check.SHA1, Size: check.Size}, force)
	if err != nil {
		return err
	}
	if action == FileActionCurrent {
		fmt.Printf("Keeping current %s: %s\n", label, targetPath)
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(targetPath), filepath.Base(targetPath)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	fmt.Printf("Downloading %s...\n", label)

	_, writeErr := downloadWithRetry(ctx, deps, rawURL, func(source io.ReadCloser) ([]byte, error) {
		defer source.Close()

		if _, err := io.Copy(tmp, source); err != nil {
			return nil, err
		}
		if check.Size > 0 {
			info, err := tmp.Stat()
			if err != nil {
				return nil, err
			}
			if info.Size() != check.Size {
				return nil, fmt.Errorf("%s size mismatch: got %d want %d", label, info.Size(), check.Size)
			}
		}
		return nil, nil
	})
	if writeErr != nil {
		tmp.Close()
		return writeErr
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if check.SHA1 != "" {
		if err := verifyFileSHA1(tmpName, check.SHA1, label); err != nil {
			return err
		}
	}

	return replaceFileWithDeps(deps, tmpName, targetPath)
}

func downloadWithRetry[T any](ctx context.Context, deps runtimeDeps, rawURL string, fn func(io.ReadCloser) (T, error)) (T, error) {
	var zero T
	var lastErr error
	for attempt := 1; attempt <= maxDownloadTries; attempt++ {
		if ctx.Err() != nil {
			return zero, ctx.Err()
		}
		source, err := openDownload(ctx, deps, rawURL)
		if err != nil {
			lastErr = err
			if attempt < maxDownloadTries && isRetryableDownloadError(err) {
				continue
			}
			return zero, err
		}

		result, err := fn(source)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if attempt < maxDownloadTries && isRetryableDownloadError(err) {
			continue
		}
		return zero, err
	}
	return zero, lastErr
}

func isRetryableDownloadError(err error) bool {
	var statusErr *downloadStatusError
	if errors.As(err, &statusErr) {
		return statusErr.StatusCode >= 500
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	return false
}

func replaceFile(tempPath, targetPath string) error {
	return replaceFileWithDeps(defaultDeps, tempPath, targetPath)
}

func replaceFileWithDeps(deps runtimeDeps, tempPath, targetPath string) error {
	deps = deps.withDefaults()
	if _, err := deps.stat(targetPath); err == nil {
		backupPath := fmt.Sprintf("%s.bak-%d-%d", targetPath, os.Getpid(), time.Now().UnixNano())

		if err := deps.rename(targetPath, backupPath); err != nil {
			return err
		}

		if err := deps.rename(tempPath, targetPath); err != nil {
			restoreErr := deps.rename(backupPath, targetPath)
			if restoreErr != nil {
				return fmt.Errorf("%w; restore failed: %v", err, restoreErr)
			}
			return err
		}

		_ = deps.remove(backupPath)
		return nil
	} else if os.IsNotExist(err) {
		if err := deps.rename(tempPath, targetPath); err != nil {
			return err
		}
		return nil
	} else {
		return err
	}
}

func openDownload(ctx context.Context, deps runtimeDeps, rawURL string) (io.ReadCloser, error) {
	deps = deps.withDefaults()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}

	switch parsed.Scheme {
	case "file":
		sourcePath := parsed.Path
		if runtime.GOOS == "windows" && strings.HasPrefix(sourcePath, "/") && len(sourcePath) > 2 && sourcePath[2] == ':' {
			sourcePath = sourcePath[1:]
		}
		sourcePath = filepath.FromSlash(sourcePath)
		if sourcePath == "" {
			return nil, fmt.Errorf("file URL has no path: %s", rawURL)
		}
		return os.Open(sourcePath)

	case "http", "https":
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, err
		}
		resp, err := deps.httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, httpErrorBodyLimit))
			resp.Body.Close()
			return nil, &downloadStatusError{
				StatusCode: resp.StatusCode,
				Status:     resp.Status,
				Body:       strings.TrimSpace(string(body)),
			}
		}
		return resp.Body, nil

	default:
		return nil, fmt.Errorf("unsupported URL scheme: %s", parsed.Scheme)
	}
}
