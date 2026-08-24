package cc

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestComposePromptWithNoSeamsMatchesPlanCompose(t *testing.T) {
	t.Parallel()

	task := plan.Task{TicketURL: "sandbox://CC-1"}
	composed, _, ok := composePrompt(t.TempDir(), task)
	if !ok {
		t.Fatal("composePrompt refused a task with no seams")
	}
	if want := plan.Compose(task, nil); composed != want {
		t.Errorf("composed = %q, want %q", composed, want)
	}
}

func TestComposePromptJoinsSeamFilesInConfigOrder(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeSeam(t, root, "one", "seam one")
	writeSeam(t, root, "two", "seam two")

	task := plan.Task{TicketURL: "sandbox://CC-1", Seams: []string{"one", "two"}}
	composed, _, ok := composePrompt(root, task)
	if !ok {
		t.Fatal("composePrompt refused a task whose seams all exist")
	}
	if want := plan.Compose(task, []string{"seam one", "seam two"}); composed != want {
		t.Errorf("composed = %q, want %q", composed, want)
	}
}

func TestComposePromptRefusesAMissingSeam(t *testing.T) {
	t.Parallel()

	task := plan.Task{TicketURL: "sandbox://CC-1", Seams: []string{"ghost"}}
	_, refused, ok := composePrompt(t.TempDir(), task)
	if ok {
		t.Fatal("composePrompt did not refuse a missing seam")
	}
	if refused != "ghost" {
		t.Errorf("refused seam = %q, want %q", refused, "ghost")
	}
}

func TestComposePromptRefusesAnUnreadableSeamWithoutPanicking(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeSeam(t, root, "locked", "seam content")
	seamPath := filepath.Join(root, ".claude", "seams", "locked")
	if err := os.Chmod(seamPath, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(seamPath, 0o600) })

	task := plan.Task{TicketURL: "sandbox://CC-1", Seams: []string{"locked"}}
	_, refused, ok := composePrompt(root, task)
	if ok {
		t.Fatal("composePrompt did not refuse an unreadable seam")
	}
	if refused != "locked" {
		t.Errorf("refused seam = %q, want %q", refused, "locked")
	}
}

func writeSeam(t *testing.T, root, name, content string) {
	t.Helper()

	dir := filepath.Join(root, ".claude", "seams")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
