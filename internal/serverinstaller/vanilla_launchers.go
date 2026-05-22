package serverinstaller

import (
	"fmt"
	"os"
	"strings"
)

const defaultJavaCommand = "java"

func WriteVanillaLaunchers(targetDir, javaPath string) error {
	if err := writeFileIfChanged(targetPath(targetDir, "run.sh"), vanillaRunSh(javaPath), 0o755); err != nil {
		return err
	}
	if err := ensureExecutable(targetPath(targetDir, "run.sh")); err != nil {
		return err
	}
	return writeFileIfChanged(targetPath(targetDir, "run.bat"), vanillaRunBat(javaPath), 0o644)
}

func vanillaRunSh(javaPath string) string {
	return "#!/usr/bin/env sh\n" + shellQuote(javaCommandName(javaPath)) + " @user_jvm_args.txt -jar server.jar nogui \"$@\"\n"
}

func vanillaRunBat(javaPath string) string {
	return "@echo off\r\n\"" + strings.ReplaceAll(javaCommandName(javaPath), `"`, `""`) + "\" @user_jvm_args.txt -jar server.jar nogui %*\r\npause\r\n"
}

func javaCommandName(javaPath string) string {
	if javaPath == "" {
		return defaultJavaCommand
	}
	return javaPath
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
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
