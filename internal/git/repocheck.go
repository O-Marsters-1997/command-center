package git

import (
	"context"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

// CheckRepoOnGitHub reads fullName's merge settings and default branch from GitHub by name, so it
// runs before any clone. A repo it accepts answers an empty kind; a refusal answers a
// plan.Refusal* kind and the reason to show. A gh failure is an error, never a pass.
func CheckRepoOnGitHub(ctx context.Context, fullName string) (kind, reason string, err error) {
	settings, err := gh.ReadRepoSettings(ctx, fullName)
	if err != nil {
		return "", "", err
	}
	kind, reason = assertRepoSettings(fullName, settings)
	return kind, reason, nil
}

func assertRepoSettings(fullName string, settings gh.RepoSettings) (kind, reason string) {
	switch {
	case settings.AllowMergeCommit:
		return plan.RefusalMergeSettings, fmt.Sprintf("repo %s allows merge commits (allow_merge_commit=true): "+
			"command-centre requires squash-only merges", fullName)
	case settings.AllowRebaseMerge:
		return plan.RefusalMergeSettings, fmt.Sprintf("repo %s allows rebase merges (allow_rebase_merge=true): "+
			"command-centre requires squash-only merges", fullName)
	case settings.DefaultBranch != plan.DefaultBaseBranch:
		return plan.RefusalDefaultBranch, fmt.Sprintf("default branch is `%s`, not `%s`; rename it on GitHub, "+
			"then Track again.", settings.DefaultBranch, plan.DefaultBaseBranch)
	}
	return "", ""
}
