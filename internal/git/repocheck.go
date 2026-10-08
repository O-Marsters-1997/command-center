package git

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/command"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

type repoSettings struct {
	AllowMergeCommit bool   `json:"allow_merge_commit"`
	AllowRebaseMerge bool   `json:"allow_rebase_merge"`
	DefaultBranch    string `json:"default_branch"`
}

// CheckRepoOnGitHub reads fullName's merge settings and default branch from GitHub by name, so it
// runs before any clone. A repo it accepts answers an empty kind; a refusal answers a
// plan.Refusal* kind and the reason to show. A gh failure is an error, never a pass.
func CheckRepoOnGitHub(ctx context.Context, fullName string) (kind, reason string, err error) {
	out, err := command.Output(ctx, "", "gh", "api", "repos/"+fullName,
		"--jq", "{allow_merge_commit, allow_rebase_merge, default_branch}")
	if err != nil {
		return "", "", fmt.Errorf("repo settings for %s: %w", fullName, err)
	}
	return assertRepoSettings(fullName, out)
}

func assertRepoSettings(fullName string, raw []byte) (kind, reason string, err error) {
	var settings repoSettings
	if err := json.Unmarshal(raw, &settings); err != nil {
		return "", "", fmt.Errorf("decode repo settings for %s: %w", fullName, err)
	}
	switch {
	case settings.AllowMergeCommit:
		return plan.RefusalMergeSettings, fmt.Sprintf("repo %s allows merge commits (allow_merge_commit=true): "+
			"command-centre requires squash-only merges", fullName), nil
	case settings.AllowRebaseMerge:
		return plan.RefusalMergeSettings, fmt.Sprintf("repo %s allows rebase merges (allow_rebase_merge=true): "+
			"command-centre requires squash-only merges", fullName), nil
	case settings.DefaultBranch != plan.DefaultBaseBranch:
		return plan.RefusalDefaultBranch, fmt.Sprintf("default branch is `%s`, not `%s`; rename it on GitHub, "+
			"then Track again.", settings.DefaultBranch, plan.DefaultBaseBranch), nil
	}
	return "", "", nil
}

// RepoRemote reads fullName's SSH clone URL from GitHub.
func RepoRemote(ctx context.Context, fullName string) (string, error) {
	out, err := command.Output(ctx, "", "gh", "api", "repos/"+fullName, "--jq", ".ssh_url")
	if err != nil {
		return "", fmt.Errorf("remote of %s: %w", fullName, err)
	}
	remote := strings.TrimSpace(string(out))
	if remote == "" {
		return "", fmt.Errorf("remote of %s: GitHub returned no ssh_url", fullName)
	}
	return remote, nil
}
