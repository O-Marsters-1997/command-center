package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stageFixture(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("staging fixture: %v", err)
	}
	return path
}

func env(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

func TestAnswersFromFixture(t *testing.T) {
	t.Parallel()

	fixture := stageFixture(t, `{"pr list": {"stdout": "[]"}}`)
	args := []string{"pr", "list", "--json", "number"}

	var stdout, stderr bytes.Buffer
	code := run(args, env(map[string]string{"CC_GH_FIXTURE": fixture}), &stdout, &stderr)

	if code != 0 {
		t.Errorf("exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	if got := stdout.String(); got != "[]" {
		t.Errorf("stdout = %q, want %q", got, "[]")
	}
}

func TestKeyedOnFirstTwoWords(t *testing.T) {
	t.Parallel()

	fixture := stageFixture(t, `{
		"pr list":   {"stdout": "[]"},
		"pr create": {"stdout": "https://github.com/o/r/pull/1\n"},
		"pr close":  {"stdout": "closed\n"},
		"pr edit":   {"stdout": "edited\n"}
	}`)

	tests := []struct {
		name     string
		args     []string
		wantOut  string
		wantCode int
	}{
		{"list", []string{"pr", "list", "--json", "number,state"}, "[]", 0},
		{"create", []string{"pr", "create", "--base", "main", "--label", "keep-open"},
			"https://github.com/o/r/pull/1\n", 0},
		{"close", []string{"pr", "close", "1"}, "closed\n", 0},
		{"edit", []string{"pr", "edit", "1", "--add-label", "keep-open"}, "edited\n", 0},
		{"unknown verb", []string{"pr", "merge", "1"}, "", 1},
		{"no args", nil, "", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			code := run(tt.args, env(map[string]string{"CC_GH_FIXTURE": fixture}), &stdout, &stderr)

			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d (stderr: %q)", code, tt.wantCode, stderr.String())
			}
			if stdout.String() != tt.wantOut {
				t.Errorf("stdout = %q, want %q", stdout.String(), tt.wantOut)
			}
			if tt.wantCode != 0 && !strings.Contains(stderr.String(), fixtureKey(tt.args)) {
				t.Errorf("stderr = %q, want it to name the missing key", stderr.String())
			}
		})
	}
}

func TestMissingFixtureEnvFails(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	code := run([]string{"pr", "list"}, env(nil), &stdout, &stderr)

	if code == 0 {
		t.Error("exit code = 0, want non-zero when CC_GH_FIXTURE is unset")
	}
	if stderr.Len() == 0 {
		t.Error("stderr is empty, want an explanation")
	}
}

func TestForcedFailureExitsWithFixtureCode(t *testing.T) {
	t.Parallel()

	fixture := stageFixture(t, `{"pr edit": {"exit": 1, "stderr": "boom\n"}}`)

	var stdout, stderr bytes.Buffer
	code := run([]string{"pr", "edit", "7"}, env(map[string]string{"CC_GH_FIXTURE": fixture}), &stdout, &stderr)

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if got := stderr.String(); got != "boom\n" {
		t.Errorf("stderr = %q, want %q", got, "boom\n")
	}
}

func TestLogsArgvWhenLogSet(t *testing.T) {
	t.Parallel()

	fixture := stageFixture(t, `{"pr list": {"stdout": "[]"}, "pr create": {"stdout": "url\n"}}`)
	logPath := filepath.Join(t.TempDir(), "gh.log")
	getenv := env(map[string]string{"CC_GH_FIXTURE": fixture, "CC_GH_LOG": logPath})

	var stdout, stderr bytes.Buffer
	run([]string{"pr", "list", "--json", "number"}, getenv, &stdout, &stderr)
	run([]string{"pr", "create", "--base", "main"}, getenv, &stdout, &stderr)

	logged, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading log: %v", err)
	}
	want := "pr list --json number\npr create --base main\n"
	if string(logged) != want {
		t.Errorf("log = %q, want %q", logged, want)
	}
}

func TestLongestMatchingKeyWins(t *testing.T) {
	t.Parallel()

	fixture := stageFixture(t, `{
		"pr list":                             {"stdout": "bulk\n"},
		"pr list --state all --head feat-x":   {"stdout": "head-scoped\n"}
	}`)

	tests := []struct {
		name     string
		args     []string
		wantOut  string
		wantCode int
	}{
		{"bulk read falls back to the two-word key",
			[]string{"pr", "list", "--state", "open", "--limit", "100", "--json", "number"},
			"bulk\n", 0},
		{"head-scoped read matches the longer key",
			[]string{"pr", "list", "--state", "all", "--head", "feat-x", "--json", "number,state"},
			"head-scoped\n", 0},
		{"a longer key is not matched by a shorter argv",
			[]string{"pr", "list", "--state", "all", "--head", "feat-xyz"},
			"bulk\n", 0},
		{"unknown key still fails naming it",
			[]string{"pr", "merge", "1"},
			"", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			code := run(tt.args, env(map[string]string{"CC_GH_FIXTURE": fixture}), &stdout, &stderr)

			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d (stderr: %q)", code, tt.wantCode, stderr.String())
			}
			if stdout.String() != tt.wantOut {
				t.Errorf("stdout = %q, want %q", stdout.String(), tt.wantOut)
			}
			if tt.wantCode != 0 && !strings.Contains(stderr.String(), "pr merge") {
				t.Errorf("stderr = %q, want it to name the missing key", stderr.String())
			}
		})
	}
}
