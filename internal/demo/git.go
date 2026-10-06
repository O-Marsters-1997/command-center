package demo

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	authorName  = "demo"
	authorEmail = "demo@example.com"
)

func git(dir string, args ...string) (string, error) {
	full := append([]string{
		"-C", dir,
		"-c", "user.name=" + authorName, "-c", "user.email=" + authorEmail,
		"-c", "commit.gpgsign=false",
	}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s in %s: %w: %s", strings.Join(args, " "), dir, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func writeFiles(dir string, files map[string]string) error {
	for name, contents := range files {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			return err
		}
		if err := os.WriteFile(full, []byte(contents), 0o600); err != nil {
			return err
		}
	}
	return nil
}

func commitAll(dir, message string, files map[string]string) error {
	if err := writeFiles(dir, files); err != nil {
		return err
	}
	if _, err := git(dir, "add", "-A"); err != nil {
		return err
	}
	_, err := git(dir, "commit", "-q", "-m", message)
	return err
}
