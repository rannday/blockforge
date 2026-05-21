package serverinstaller

import (
	"fmt"
	"os"
)

const vanillaRunSh = "#!/usr/bin/env sh\njava @user_jvm_args.txt -jar server.jar nogui \"$@\"\n"
const vanillaRunBat = "@echo off\r\njava @user_jvm_args.txt -jar server.jar nogui %*\r\npause\r\n"

func WriteVanillaLaunchers(targetDir string) error {
	if err := writeFileIfChanged(targetPath(targetDir, "run.sh"), vanillaRunSh, 0o755); err != nil {
		return err
	}
	if err := ensureExecutable(targetPath(targetDir, "run.sh")); err != nil {
		return err
	}
	return writeFileIfChanged(targetPath(targetDir, "run.bat"), vanillaRunBat, 0o644)
}

func writeFileIfChanged(path, content string, mode os.FileMode) error {
	current, err := os.ReadFile(path)
	if err == nil && string(current) == content {
		return nil
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
