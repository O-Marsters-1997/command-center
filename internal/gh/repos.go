package gh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// RepoSummary is one repository the gh identity can push to.
type RepoSummary struct {
	FullName      string
	SSHURL        string
	DefaultBranch string
	Archived      bool
}

// PushableRepos lists the unarchived repositories the gh identity can push to.
func PushableRepos(ctx context.Context) ([]RepoSummary, error) {
	out, err := run(ctx, "", "api", "--paginate",
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

// decodePushable reads the concatenated JSON arrays that gh api --paginate prints, one per page.
func decodePushable(raw []byte) ([]RepoSummary, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	var repos []RepoSummary
	for {
		var page []rawRepo
		err := dec.Decode(&page)
		if errors.Is(err, io.EOF) {
			return repos, nil
		}
		if err != nil {
			return nil, err
		}
		for _, r := range page {
			if !r.Permissions.Push || r.Archived {
				continue
			}
			repos = append(repos, RepoSummary{
				FullName: r.FullName, SSHURL: r.SSHURL, DefaultBranch: r.DefaultBranch,
			})
		}
	}
}
