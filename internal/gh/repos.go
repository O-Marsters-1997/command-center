package gh

import (
	"context"
	"fmt"
)

// RepoSummary is one repository the gh identity can push to.
type RepoSummary struct {
	FullName      string
	SSHURL        string
	DefaultBranch string
	Archived      bool
}

type RepoSettings struct {
	AllowMergeCommit bool   `json:"allow_merge_commit"`
	AllowRebaseMerge bool   `json:"allow_rebase_merge"`
	DefaultBranch    string `json:"default_branch"`
}

func ReadRepoSettings(ctx context.Context, fullName string) (RepoSettings, error) {
	out, err := Run(ctx, "", "api", "repos/"+fullName,
		"--jq", "{allow_merge_commit, allow_rebase_merge, default_branch}")
	if err != nil {
		return RepoSettings{}, fmt.Errorf("repo settings for %s: %w", fullName, err)
	}
	settings, err := Decode[RepoSettings](out)
	if err != nil {
		return RepoSettings{}, fmt.Errorf("repo settings for %s: %w", fullName, err)
	}
	return settings, nil
}

func RepoRemote(ctx context.Context, fullName string) (string, error) {
	out, err := Run(ctx, "", "api", "repos/"+fullName, "--jq", "{ssh_url}")
	if err != nil {
		return "", fmt.Errorf("remote of %s: %w", fullName, err)
	}
	repo, err := Decode[rawRepo](out)
	if err != nil {
		return "", fmt.Errorf("remote of %s: %w", fullName, err)
	}
	if repo.SSHURL == "" {
		return "", fmt.Errorf("remote of %s: GitHub returned no ssh_url", fullName)
	}
	return repo.SSHURL, nil
}

// PushableRepos lists the unarchived repositories the gh identity can push to.
func PushableRepos(ctx context.Context) ([]RepoSummary, error) {
	out, err := Run(ctx, "", "api", "--paginate",
		"user/repos?affiliation=owner,collaborator,organization_member&per_page=100")
	if err != nil {
		return nil, err
	}
	repos, err := decodePushable(out)
	if err != nil {
		return nil, fmt.Errorf("decode user repos: %w", err)
	}
	return repos, nil
}

type rawRepo struct {
	FullName      string `json:"full_name"`
	SSHURL        string `json:"ssh_url"`
	DefaultBranch string `json:"default_branch"`
	Archived      bool   `json:"archived"`
	Permissions   struct {
		Push bool `json:"push"`
	} `json:"permissions"`
}

func decodePushable(raw []byte) ([]RepoSummary, error) {
	pages, err := DecodeStream[rawRepo](raw)
	if err != nil {
		return nil, err
	}
	var repos []RepoSummary
	for _, r := range pages {
		if !r.Permissions.Push || r.Archived {
			continue
		}
		repos = append(repos, RepoSummary{
			FullName: r.FullName, SSHURL: r.SSHURL, DefaultBranch: r.DefaultBranch,
		})
	}
	return repos, nil
}
