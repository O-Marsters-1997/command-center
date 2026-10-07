package git

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/command"
)

type repoSettings struct {
	AllowMergeCommit bool `json:"allow_merge_commit"`
	AllowRebaseMerge bool `json:"allow_rebase_merge"`
}

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

// CheckSquashOnly refuses any repo that allows merge commits or rebase merges. A gh failure is
// fail-closed.
func CheckSquashOnly(ctx context.Context, repoPath, repoName string) error {
	out, err := command.Output(ctx, repoPath, "gh", "api", "repos/{owner}/{repo}",
		"--jq", "{allow_merge_commit, allow_rebase_merge}")
	if err != nil {
		return fmt.Errorf("repo settings for %s: %w", repoName, err)
	}
	return assertSquashOnly(repoName, out)
}
