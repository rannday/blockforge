package serverinstaller

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
)

var javaVersionPattern = regexp.MustCompile(`version "([^"]+)"`)

func RequireJava21() error {
	return RequireJava("java", 21)
}

func RequireJava(javaPath string, majorVersion int) error {
	if javaPath == "" {
		javaPath = "java"
	}
	if _, err := exec.LookPath(javaPath); err != nil {
		return fmt.Errorf("%s was not found; Java %d+ is required", javaPath, majorVersion)
	}

	out, err := exec.Command(javaPath, "-version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s -version failed: %w\n%s", javaPath, err, bytes.TrimSpace(out))
	}

	version, err := ParseJavaVersion(string(out))
	if err != nil {
		return err
	}
	if version < majorVersion {
		return fmt.Errorf("Java %d+ is required; found Java %d", majorVersion, version)
	}

	return nil
}

func ParseJavaVersion(output string) (int, error) {
	match := javaVersionPattern.FindStringSubmatch(output)
	if len(match) < 2 {
		return 0, fmt.Errorf("could not parse Java version")
	}

	parts := bytes.Split([]byte(match[1]), []byte("."))
	var majorText string
	if len(parts) > 0 && bytes.Equal(parts[0], []byte("1")) && len(parts) > 1 {
		majorText = string(parts[1])
	} else {
		majorText = string(parts[0])
	}

	major, err := strconv.Atoi(majorText)
	if err != nil {
		return 0, fmt.Errorf("could not parse Java version: %s", match[1])
	}

	return major, nil
}
