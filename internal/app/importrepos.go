package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/git"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

func importLegacyRepos(
	ctx context.Context, st *store.Store, configPath string, cfg config.Config, now time.Time,
) error {
	if len(cfg.LegacyRepos) == 0 {
		return nil
	}
	tracked, err := st.Repos(ctx)
	if err != nil {
		return err
	}
	if len(tracked) > 0 {
		return fmt.Errorf("%s still has [[repo]] blocks; repos are tracked in the database now — delete them", configPath)
	}

	imports, err := legacyImports(cfg.LegacyRepos, now)
	if err != nil {
		return err
	}
	for _, imp := range imports {
		if err := moveCheckout(cfg.DataDir, imp.ShortName, imp.Repo.Name); err != nil {
			return err
		}
	}
	return st.ImportRepos(ctx, imports)
}

func legacyImports(legacy []config.LegacyRepo, now time.Time) ([]store.RepoImport, error) {
	imports := make([]store.RepoImport, 0, len(legacy))
	owners := map[string]string{}
	for _, block := range legacy {
		if block.Remote == "" {
			return nil, fmt.Errorf("import [[repo]] %s: it has no remote; a repo located by path is no longer "+
				"supported, so give it the remote its checkout's origin names", block.Name)
		}
		fullName, err := git.FullName(block.Remote)
		if err != nil {
			return nil, fmt.Errorf("import [[repo]] %s: %w", block.Name, err)
		}
		owner, _, _ := strings.Cut(fullName, "/")
		owners[owner] = fullName
		imports = append(imports, store.RepoImport{
			ShortName: block.Name,
			Repo:      store.Repo{Name: fullName, Remote: block.Remote, State: store.RepoCloning, TrackedAt: now},
		})
	}
	for _, imp := range imports {
		if fullName, clash := owners[imp.ShortName]; clash {
			return nil, fmt.Errorf("import [[repo]] %s: repos/%s is both its checkout and the owner directory of %s; "+
				"rename the checkout and the [[repo]] name to something else first", imp.ShortName, imp.ShortName, fullName)
		}
	}
	return imports, nil
}

func moveCheckout(dataDir, shortName, fullName string) error {
	from := filepath.Join(dataDir, "repos", shortName)
	to := config.CheckoutPath(dataDir, fullName)
	info, err := os.Lstat(from)
	if errors.Is(err, os.ErrNotExist) {
		return linkBack(from, to)
	}
	if err != nil {
		return fmt.Errorf("stat %s: %w", from, err)
	}
	isLink := info.Mode()&os.ModeSymlink != 0
	if _, err := os.Lstat(to); err == nil {
		if isLink {
			return nil
		}
		return fmt.Errorf("move %s to %s: destination already exists", from, to)
	}

	if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(to), err)
	}
	if err := relocate(from, to, isLink); err != nil {
		return fmt.Errorf("move %s to %s: %w", from, to, err)
	}
	return linkBack(from, to)
}

func linkBack(from, to string) error {
	if _, err := os.Stat(to); err != nil {
		return nil
	}
	target, err := filepath.Rel(filepath.Dir(from), to)
	if err != nil {
		return err
	}
	if err := os.Symlink(target, from); err != nil {
		return fmt.Errorf("link %s to %s: %w", from, to, err)
	}
	return nil
}

func relocate(from, to string, isLink bool) error {
	if !isLink {
		return os.Rename(from, to)
	}
	target, err := os.Readlink(from)
	if err != nil {
		return err
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(from), target)
	}
	if err := os.Symlink(target, to); err != nil {
		return err
	}
	return os.Remove(from)
}
