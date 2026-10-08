package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

	imports := make([]store.RepoImport, 0, len(cfg.LegacyRepos))
	for _, legacy := range cfg.LegacyRepos {
		fullName, err := git.FullName(legacy.Remote)
		if err != nil {
			return fmt.Errorf("import [[repo]] %s: %w", legacy.Name, err)
		}
		if err := moveCheckout(cfg.DataDir, legacy.Name, fullName); err != nil {
			return err
		}
		imports = append(imports, store.RepoImport{
			ShortName: legacy.Name,
			Repo:      store.Repo{Name: fullName, Remote: legacy.Remote, State: store.RepoReady, TrackedAt: now},
		})
	}
	return st.ImportRepos(ctx, imports)
}

func moveCheckout(dataDir, shortName, fullName string) error {
	from := filepath.Join(dataDir, "repos", shortName)
	to := config.CheckoutPath(dataDir, fullName)
	info, err := os.Lstat(from)
	if errors.Is(err, os.ErrNotExist) {
		return nil
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
