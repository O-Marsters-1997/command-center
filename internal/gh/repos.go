package gh

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
)

// RepoSummary is one repo the box's gh identity can push to, read from user/repos.
type RepoSummary struct {
	FullName      string
	SSHURL        string
	DefaultBranch string
	Archived      bool
}

// PushableRepos reads every repo the box's gh identity owns, collaborates on, or belongs to
// through an organisation, keeping only the ones it can push to and dropping archived repos. It
// needs no checkout: user/repos is an account-wide read, not a per-repo one.
func PushableRepos(ctx context.Context) ([]RepoSummary, error) {
	out, err := run(ctx, "", "api", "--paginate",
		"user/repos?affiliation=owner,collaborator,organization_member&per_page=100")
	if err != nil {
		return nil, err
	}
	pages, err := decodeRepoPages(out)
	if err != nil {
		return nil, fmt.Errorf("decode pushable repos: %w", err)
	}
	return pushableFrom(pages), nil
}

// rawRepo mirrors user/repos' JSON exactly. Nothing outside this file may read these field names.
type rawRepo struct {
	FullName      string             `json:"full_name"`
	SSHURL        string             `json:"ssh_url"`
	DefaultBranch string             `json:"default_branch"`
	Archived      bool               `json:"archived"`
	Permissions   rawRepoPermissions `json:"permissions"`
}

type rawRepoPermissions struct {
	Push bool `json:"push"`
}

// decodeRepoPages reads gh api --paginate's own output: one JSON array per page, concatenated
// back to back rather than merged into a single array, so this decodes a stream of arrays rather
// than making one Decode call the way decode (gh.go) does for a single response.
func decodeRepoPages(raw []byte) ([]rawRepo, error) {
	var all []rawRepo
	dec := json.NewDecoder(bytes.NewReader(raw))
	for dec.More() {
		var page []rawRepo
		if err := dec.Decode(&page); err != nil {
			return nil, fmt.Errorf("unmarshal repo page: %w", err)
		}
		all = append(all, page...)
	}
	return all, nil
}

// pushableFrom keeps the repos the box's gh identity can push to, dropping archived ones.
func pushableFrom(pages []rawRepo) []RepoSummary {
	repos := make([]RepoSummary, 0, len(pages))
	for _, r := range pages {
		if !r.Permissions.Push || r.Archived {
			continue
		}
		repos = append(repos, RepoSummary{
			FullName: r.FullName, SSHURL: r.SSHURL, DefaultBranch: r.DefaultBranch, Archived: r.Archived,
		})
	}
	return repos
}
