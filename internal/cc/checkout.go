package cc

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/git"
)

// CheckoutPath answers where a repo's working copy is. A remote repo's checkout is one the app
// makes and names, at <dataDir>/repos/<name>; a path repo's is one the operator made, absolute
// or relative to configDir. Exactly one of the two forms is allowed.
func (r Repo) CheckoutPath(dataDir, configDir string) (string, error) {
	switch {
	case r.Remote != "" && r.Path != "":
		return "", fmt.Errorf("repo %s sets both remote and path: pick one", r.Name)
	case r.Remote == "" && r.Path == "":
		return "", fmt.Errorf("repo %s sets neither remote nor path", r.Name)
	case r.Remote != "":
		if err := validRepoName(r.Name); err != nil {
			return "", err
		}
		return filepath.Join(dataDir, "repos", r.Name), nil
	}

	path, err := expandHome(r.Path)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(configDir, path)
	}
	return filepath.Clean(path), nil
}

func validRepoName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("repo name %q is not a single directory name", name)
	}
	return nil
}

// RepoNameForDir answers which configured repo dir belongs to, using dir's git origin normalised
// and compared the same way git.EnsureCheckout compares an existing checkout's origin. No origin, or
// no configured repo matching it, answers ok=false rather than an error.
func RepoNameForDir(ctx context.Context, dir string, repos []Repo) (name string, ok bool) {
	origin, err := git.OriginURL(ctx, dir)
	if err != nil {
		return "", false
	}
	for _, r := range repos {
		if r.Remote != "" && git.SameRemote(origin, r.Remote) {
			return r.Name, true
		}
	}
	return "", false
}
