package serverinstaller

import (
	"fmt"
	"os"
)

func WriteJvmArgs(targetDir string, force bool) error {
	path := targetPath(targetDir, "user_jvm_args.txt")
	action, err := classifyManagedContentFile(path, jvmArgsContent(), force)
	if err != nil {
		return err
	}
	if action == FileActionCurrent {
		fmt.Printf("Keeping current JVM args: %s\n", path)
		return nil
	}

	return os.WriteFile(path, []byte(jvmArgsContent()), 0o644)
}

func jvmArgsContent() string {
	return "# JVM memory settings for this server.\n" +
		"# Adjust these based on available server RAM.\n" +
		"-Xms4G\n" +
		"-Xmx6G\n"
}
