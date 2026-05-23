package serverinstaller

import (
	"crypto/sha1"
	"fmt"
	"os"
	"strings"
)

type FileAction string

const (
	FileActionMissing FileAction = "missing"
	FileActionCurrent FileAction = "current"
	FileActionReplace FileAction = "replace"
)

type FileCheck struct {
	SHA1 string
	Size int64
}

func classifyManagedFile(path string, check FileCheck, force bool) (FileAction, error) {
	return classifyManagedFileWithStat(path, check, force, os.Stat)
}

func classifyManagedFileWithStat(path string, check FileCheck, force bool, stat func(string) (os.FileInfo, error)) (FileAction, error) {
	info, err := stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return FileActionMissing, nil
		}
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("target path is a directory: %s", path)
	}
	if force {
		return FileActionReplace, nil
	}
	if check.Size > 0 && info.Size() != check.Size {
		return FileActionReplace, nil
	}
	if check.SHA1 != "" {
		actual, err := sha1File(path)
		if err != nil {
			return "", err
		}
		if strings.EqualFold(actual, check.SHA1) {
			return FileActionCurrent, nil
		}
		return FileActionReplace, nil
	}
	if info.Size() > 0 {
		return FileActionCurrent, nil
	}
	return FileActionReplace, nil
}

func classifyManagedContentFile(path, expected string, force bool) (FileAction, error) {
	return classifyManagedFile(path, FileCheck{
		SHA1: sha1Bytes([]byte(expected)),
		Size: int64(len(expected)),
	}, force)
}

func sha1Bytes(data []byte) string {
	sum := sha1.Sum(data)
	return fmt.Sprintf("%x", sum[:])
}
