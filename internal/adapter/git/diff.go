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

type FileKind string

const (
	FileAdded    FileKind = "added"
	FileModified FileKind = "modified"
	FileDeleted  FileKind = "deleted"
	FileRenamed  FileKind = "renamed"
)

type Hunk struct {
	Header             string // the "@@ -a,b +c,d @@ …" line
	OldStart, OldLines int
	NewStart, NewLines int
	Section            string // the text after the closing "@@", usually the enclosing function
	Lines              []DiffLine
}

type FileDiff struct {
	Path    string
	OldPath string // set on a rename
	Kind    FileKind
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
			files = append(files, FileDiff{Path: headerPath(rest), Kind: FileModified})
			continue
		}
		if len(files) == 0 {
			continue
		}
		f := &files[len(files)-1]
		inHunk := len(f.Hunks) > 0
		switch {
		case strings.HasPrefix(line, "@@ "):
			h, err := parseHunkHeader(line)
			if err != nil {
				return nil, err
			}
			oldNo, newNo = h.OldStart, h.NewStart
			f.Hunks = append(f.Hunks, h)
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
		case strings.HasPrefix(line, "new file mode "):
			f.Kind = FileAdded
		case strings.HasPrefix(line, "deleted file mode "):
			f.Kind = FileDeleted
		case strings.HasPrefix(line, "rename from "):
			f.Kind = FileRenamed
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

func parseHunkHeader(header string) (Hunk, error) {
	rest, ok := strings.CutPrefix(header, "@@ ")
	if !ok {
		return Hunk{}, fmt.Errorf("parse hunk header %q", header)
	}
	ranges, section, ok := strings.Cut(rest, " @@")
	fields := strings.Fields(ranges)
	if !ok || len(fields) != 2 {
		return Hunk{}, fmt.Errorf("parse hunk header %q", header)
	}
	h := Hunk{Header: header, Section: strings.TrimSpace(section)}
	var err error
	if h.OldStart, h.OldLines, err = parseRange(fields[0], "-"); err != nil {
		return Hunk{}, fmt.Errorf("parse hunk header %q: %w", header, err)
	}
	if h.NewStart, h.NewLines, err = parseRange(fields[1], "+"); err != nil {
		return Hunk{}, fmt.Errorf("parse hunk header %q: %w", header, err)
	}
	return h, nil
}

func parseRange(field, sign string) (start, lines int, err error) {
	body, ok := strings.CutPrefix(field, sign)
	if !ok {
		return 0, 0, fmt.Errorf("range %q: missing %q", field, sign)
	}
	first, count, hasCount := strings.Cut(body, ",")
	if start, err = strconv.Atoi(first); err != nil {
		return 0, 0, fmt.Errorf("range %q: %w", field, err)
	}
	if !hasCount {
		return start, 1, nil
	}
	if lines, err = strconv.Atoi(count); err != nil {
		return 0, 0, fmt.Errorf("range %q: %w", field, err)
	}
	return start, lines, nil
}
