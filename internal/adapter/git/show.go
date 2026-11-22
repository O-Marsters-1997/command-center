package git

import (
	"context"
	"fmt"
	"strings"
)

func FileLines(ctx context.Context, repoPath, rev, path string, from, to int) ([]string, error) {
	if from < 1 || to < from {
		return nil, fmt.Errorf("lines %d..%d of %s: invalid range", from, to, path)
	}
	out, err := git(ctx, repoPath, "show", rev+":"+path)
	if err != nil {
		return nil, fmt.Errorf("show %s:%s: %w", rev, path, err)
	}
	all := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if from > len(all) {
		return nil, nil
	}
	return all[from-1 : min(to, len(all))], nil
}
