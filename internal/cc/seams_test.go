package cc

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestComposePromptWithNoSeamsMatchesPlanCompose(t *testing.T) {
	t.Parallel()

	task := plan.Task{TicketURL: "sandbox://CC-1"}
	composed, _, ok := composePrompt(context.Background(), t.TempDir(), task, nil)
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
	composed, _, ok := composePrompt(context.Background(), root, task, nil)
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
	_, refused, ok := composePrompt(context.Background(), t.TempDir(), task, nil)
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
	_, refused, ok := composePrompt(context.Background(), root, task, nil)
	if ok {
		t.Fatal("composePrompt did not refuse an unreadable seam")
	}
	if refused != "locked" {
		t.Errorf("refused seam = %q, want %q", refused, "locked")
	}
}

// TestComposePromptPastesLandsAtOnceRetiredAndDiffersFromTheSeamFile covers issue #58's AC1: a
// retired seam pastes its lands_at content instead of the seam file, and the two compositions
// differ, which is what makes an already-authorised hash go stale on retirement.
func TestComposePromptPastesLandsAtOnceRetiredAndDiffersFromTheSeamFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeSeam(t, root, "gql", "stale seam text")
	producerRepo := gitRepoWithOriginMain(t, map[string]string{"schema.graphql": "type Query {}"})

	task := plan.Task{TicketURL: "sandbox://CC-1", Seams: []string{"gql"}}

	preMerge, _, ok := composePrompt(context.Background(), root, task, nil)
	if !ok {
		t.Fatal("composePrompt refused the pre-merge (seam-file) composition")
	}
	if want := plan.Compose(task, []string{"stale seam text"}); preMerge != want {
		t.Errorf("pre-merge composed = %q, want %q", preMerge, want)
	}

	retirements := map[string]retirement{"gql": {repoPath: producerRepo, landsAt: []string{"schema.graphql"}}}
	postMerge, _, ok := composePrompt(context.Background(), root, task, retirements)
	if !ok {
		t.Fatal("composePrompt refused a retired seam with a valid lands_at path")
	}
	if want := plan.Compose(task, []string{"type Query {}"}); postMerge != want {
		t.Errorf("post-merge composed = %q, want %q", postMerge, want)
	}
	if preMerge == postMerge {
		t.Error("pre- and post-merge compositions must differ once the seam retires")
	}
}

// TestComposePromptRefusesALandsAtPathAbsentFromMain covers issue #58's AC3: a retired seam
// whose lands_at path does not exist on the producer's origin/main is a refusal naming the
// path, never a silent fallback to the (now stale) seam file.
func TestComposePromptRefusesALandsAtPathAbsentFromMain(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeSeam(t, root, "gql", "seam text")
	producerRepo := gitRepoWithOriginMain(t, map[string]string{"schema.graphql": "type Query {}"})

	task := plan.Task{TicketURL: "sandbox://CC-1", Seams: []string{"gql"}}
	retirements := map[string]retirement{"gql": {repoPath: producerRepo, landsAt: []string{"missing.graphql"}}}

	_, refused, ok := composePrompt(context.Background(), root, task, retirements)
	if ok {
		t.Fatal("composePrompt did not refuse a lands_at path absent from origin/main")
	}
	if refused != "missing.graphql" {
		t.Errorf("refused = %q, want %q", refused, "missing.graphql")
	}
}

// TestComposePromptPastesMultipleLandsAtPathsInDeclaredOrder covers issue #58's AC5.
func TestComposePromptPastesMultipleLandsAtPathsInDeclaredOrder(t *testing.T) {
	t.Parallel()

	producerRepo := gitRepoWithOriginMain(t, map[string]string{"a.txt": "A", "b.txt": "B"})

	task := plan.Task{TicketURL: "sandbox://CC-1", Seams: []string{"multi"}}
	retirements := map[string]retirement{"multi": {repoPath: producerRepo, landsAt: []string{"a.txt", "b.txt"}}}

	composed, _, ok := composePrompt(context.Background(), t.TempDir(), task, retirements)
	if !ok {
		t.Fatal("composePrompt refused")
	}
	if want := plan.Compose(task, []string{"A", "B"}); composed != want {
		t.Errorf("composed = %q, want %q (declared order a.txt then b.txt)", composed, want)
	}

	reversed := map[string]retirement{"multi": {repoPath: producerRepo, landsAt: []string{"b.txt", "a.txt"}}}
	composedReversed, _, ok := composePrompt(context.Background(), t.TempDir(), task, reversed)
	if !ok {
		t.Fatal("composePrompt refused")
	}
	if composedReversed == composed {
		t.Error("declared order must be honoured: reversing lands_at must change the composition")
	}
}

func TestRetirementsByNameRequiresEveryProducerMerged(t *testing.T) {
	t.Parallel()

	byURL := map[string]plan.Task{
		"t1": {TicketURL: "t1", Branch: "b1"},
		"t2": {TicketURL: "t2", Branch: "b2"},
	}
	seam := Seam{Name: "gql", Repo: "r", Producers: []string{"t1", "t2"}, LandsAt: []string{"schema.graphql"}}
	repoPaths := map[string]string{"r": "/repo"}

	prs := map[string]plan.PRState{"b1": plan.Merged, "b2": plan.Open}
	if _, retired := retirementsByName([]Seam{seam}, byURL, prs, repoPaths)["gql"]; retired {
		t.Error("retirement present with a producer still open")
	}

	prs["b2"] = plan.Merged
	r, retired := retirementsByName([]Seam{seam}, byURL, prs, repoPaths)["gql"]
	if !retired {
		t.Fatal("retirement missing once every producer merged")
	}
	if r.repoPath != "/repo" || !slices.Equal(r.landsAt, []string{"schema.graphql"}) {
		t.Errorf("retirement = %+v", r)
	}
}

// TestRetirementsByNameSkipsASeamWithEmptyLandsAt covers issue #58's AC4: lands_at is optional,
// and its absence means "no retirement" -- forever, not just until some producer merges.
func TestRetirementsByNameSkipsASeamWithEmptyLandsAt(t *testing.T) {
	t.Parallel()

	byURL := map[string]plan.Task{"t1": {TicketURL: "t1", Branch: "b1"}}
	prs := map[string]plan.PRState{"b1": plan.Merged}
	seam := Seam{Name: "gql", Repo: "r", Producers: []string{"t1"}}

	if _, retired := retirementsByName([]Seam{seam}, byURL, prs, map[string]string{"r": "/repo"})["gql"]; retired {
		t.Error("a seam with no lands_at must never retire")
	}
}

// TestRetirementsByNameSkipsANameWithNoSeamBlock covers issue #58's AC4's other half: a seam
// name a task consumes with no [[seam]] block at all simply never appears in the result.
func TestRetirementsByNameSkipsANameWithNoSeamBlock(t *testing.T) {
	t.Parallel()

	if got := retirementsByName(nil, nil, nil, nil); len(got) != 0 {
		t.Errorf("retirements = %+v, want none", got)
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

// gitRepoWithOriginMain creates a real repo with files committed and pushed to a bare "origin"
// remote's main branch -- the shape ShowFile's `origin/main:<path>` needs to resolve, without a
// working-tree checkout of the content.
func gitRepoWithOriginMain(t *testing.T, files map[string]string) string {
	t.Helper()

	dir := t.TempDir()
	repoPath := filepath.Join(dir, "repo")
	remote := filepath.Join(dir, "remote.git")

	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}

	run("init", "-q", "-b", "main", repoPath)
	for path, content := range files {
		full := filepath.Join(repoPath, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		run("-C", repoPath, "add", path)
	}
	run("-C", repoPath, "commit", "-q", "-m", "initial")
	run("init", "-q", "-b", "main", "--bare", remote)
	run("-C", repoPath, "remote", "add", "origin", remote)
	run("-C", repoPath, "push", "-q", "-u", "origin", "main")
	return repoPath
}
