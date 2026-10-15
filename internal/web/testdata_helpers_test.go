package web_test

import (
	"os"
	"path/filepath"
	"testing"
)

func mustReadTestdata(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("view", "testdata", name))
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", name, err)
	}
	return string(body)
}

func writeRunLog(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "run.jsonl")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
	return path
}
