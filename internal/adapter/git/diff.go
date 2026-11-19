package git

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

type LineKind string

const (
	LineContext LineKind = "context"
	LineAdded   LineKind = "added"
	LineDeleted LineKind = "deleted"
)

type DiffLine struct {
	Kind    LineKind
	OldLine int // 0 for an added line
	NewLine int // 0 for a deleted line
	Text    string
}

type Hunk struct {
	Header string // the "@@ -a,b +c,d @@ …" line
	Lines  []DiffLine
}

type FileDiff struct {
	Path    string
	OldPath string // set on a rename
	Binary  bool
	Added   int
	Deleted int
	Hunks   []Hunk
}

func Diff(ctx context.Context, repoPath, base, tip string) ([]FileDiff, error) {
	out, err := git(ctx, repoPath,
		"-c", "core.quotepath=false", "diff", "--no-color", "--no-ext-diff", "--no-prefix", "-M", base+"..."+tip)
	if err != nil {
		return nil, fmt.Errorf("diff %s...%s: %w", base, tip, err)
	}
	files, err := parseDiff(string(out))
	if err != nil {
		return nil, fmt.Errorf("parse diff %s...%s: %w", base, tip, err)
	}
	return files, nil
}

func parseDiff(out string) ([]FileDiff, error) {
	var (
		files        []FileDiff
		oldNo, newNo int
	)
	for _, line := range strings.Split(out, "\n") {
		if rest, ok := strings.CutPrefix(line, "diff --git "); ok {
			files = append(files, FileDiff{Path: headerPath(rest)})
			continue
		}
		if len(files) == 0 {
			continue
		}
		f := &files[len(files)-1]
		inHunk := len(f.Hunks) > 0
		switch {
		case strings.HasPrefix(line, "@@ "):
			var err error
			if oldNo, newNo, err = hunkStarts(line); err != nil {
				return nil, err
			}
			f.Hunks = append(f.Hunks, Hunk{Header: line})
		case inHunk && line != "" && line[0] != '\\':
			h := &f.Hunks[len(f.Hunks)-1]
			text := line[1:]
			switch line[0] {
			case '+':
				h.Lines = append(h.Lines, DiffLine{Kind: LineAdded, NewLine: newNo, Text: text})
				newNo++
				f.Added++
			case '-':
				h.Lines = append(h.Lines, DiffLine{Kind: LineDeleted, OldLine: oldNo, Text: text})
				oldNo++
				f.Deleted++
			default:
				h.Lines = append(h.Lines, DiffLine{Kind: LineContext, OldLine: oldNo, NewLine: newNo, Text: text})
				oldNo++
				newNo++
			}
		case inHunk:
		case strings.HasPrefix(line, "rename from "):
			f.OldPath = strings.TrimPrefix(line, "rename from ")
		case strings.HasPrefix(line, "rename to "):
			f.Path = strings.TrimPrefix(line, "rename to ")
		case strings.HasPrefix(line, "Binary files "), line == "GIT binary patch":
			f.Binary = true
		}
	}
	return files, nil
}

func headerPath(rest string) string {
	if n := len(rest) / 2; len(rest)%2 == 1 && rest[:n] == rest[n+1:] {
		return rest[:n]
	}
	path, _, _ := strings.Cut(rest, " ")
	return path
}

func hunkStarts(header string) (oldStart, newStart int, err error) {
	fields := strings.Fields(header)
	if len(fields) < 3 {
		return 0, 0, fmt.Errorf("parse hunk header %q", header)
	}
	oldStart, err = rangeStart(fields[1], "-")
	if err != nil {
		return 0, 0, fmt.Errorf("parse hunk header %q: %w", header, err)
	}
	newStart, err = rangeStart(fields[2], "+")
	if err != nil {
		return 0, 0, fmt.Errorf("parse hunk header %q: %w", header, err)
	}
	return oldStart, newStart, nil
}

func rangeStart(field, sign string) (int, error) {
	start, _, _ := strings.Cut(strings.TrimPrefix(field, sign), ",")
	n, err := strconv.Atoi(start)
	if err != nil {
		return 0, fmt.Errorf("range %q: %w", field, err)
	}
	return n, nil
}
