package cc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
)

// repoSettings is the subset of `gh api repos/{owner}/{repo}` this check reads.
type repoSettings struct {
	AllowMergeCommit bool `json:"allow_merge_commit"`
	AllowRebaseMerge bool `json:"allow_rebase_merge"`
}

// assertSquashOnly decodes a repo's settings and refuses anything but squash-only: the whole
// merge-don't-rebase design (docs/designs/command-centre-design.md §4a) rests on GitHub flattening every
// merge, and that only holds if merge commits and rebase merges are both disabled server-side.
func assertSquashOnly(repoName string, raw []byte) error {
	var settings repoSettings
	if err := json.Unmarshal(raw, &settings); err != nil {
		return fmt.Errorf("decode repo settings for %s: %w", repoName, err)
	}
	switch {
	case settings.AllowMergeCommit:
		return fmt.Errorf("repo %s allows merge commits (allow_merge_commit=true): "+
			"command-centre requires squash-only merges, refusing to start", repoName)
	case settings.AllowRebaseMerge:
		return fmt.Errorf("repo %s allows rebase merges (allow_rebase_merge=true): "+
			"command-centre requires squash-only merges, refusing to start", repoName)
	}
	return nil
}

// checkRepoSquashOnly reads one repo's settings via gh and asserts them. A gh failure (non-zero
// exit or malformed JSON) is fail-closed: refuse to start rather than assume squash-only holds.
func checkRepoSquashOnly(ctx context.Context, repoPath, repoName string) error {
	cmd := exec.CommandContext(ctx, "gh", "api", "repos/{owner}/{repo}",
		"--jq", "{allow_merge_commit, allow_rebase_merge}")
	cmd.Dir = repoPath
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("gh api repo settings for %s: %w: %s", repoName, err, bytes.TrimSpace(stderr.Bytes()))
	}
	return assertSquashOnly(repoName, out)
}

// AssertReposSquashOnly checks every configured repo's merge settings, once each, before the
// tick loop starts. It mirrors OpenStore's schema_version check: a precondition the design
// depends on, checked loudly rather than assumed (docs/designs/command-centre-design.md §11.6).
func AssertReposSquashOnly(ctx context.Context, ws Workspace, repos []Repo) error {
	for _, r := range repos {
		if err := checkRepoSquashOnly(ctx, filepath.Join(ws.Root, r.Path), r.Name); err != nil {
			return err
		}
	}
	return nil
}
