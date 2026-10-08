package agentlog

import (
	"fmt"
	"strings"
)

type DiffOp int

const (
	Keep DiffOp = iota
	Add
	Remove
)

func (op DiffOp) String() string {
	switch op {
	case Add:
		return "add"
	case Remove:
		return "del"
	default:
		return "ctx"
	}
}

type DiffLine struct {
	Op   DiffOp
	Text string
}

type Diff struct {
	Lines          []DiffLine
	Added, Removed int
}

const (
	diffContext  = 2
	maxDiffLines = 80
)

func lineDiff(before, after string) Diff {
	if before == after {
		return Diff{}
	}
	old := strings.Split(before, "\n")
	next := strings.Split(after, "\n")

	prefix := 0
	for prefix < len(old) && prefix < len(next) && old[prefix] == next[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(old)-prefix && suffix < len(next)-prefix &&
		old[len(old)-1-suffix] == next[len(next)-1-suffix] {
		suffix++
	}

	removed := old[prefix : len(old)-suffix]
	added := next[prefix : len(next)-suffix]
	diff := Diff{Added: len(added), Removed: len(removed)}

	diff.Lines = appendRows(diff.Lines, Keep, old[max(0, prefix-diffContext):prefix])
	diff.Lines = appendRows(diff.Lines, Remove, removed)
	diff.Lines = appendRows(diff.Lines, Add, added)
	diff.Lines = appendRows(diff.Lines, Keep, old[len(old)-suffix:min(len(old), len(old)-suffix+diffContext)])
	if cut := len(diff.Lines) - maxDiffLines; cut > 0 {
		diff.Lines = append(diff.Lines[:maxDiffLines], DiffLine{Op: Keep, Text: fmt.Sprintf("… %d more lines", cut)})
	}
	return diff
}

func appendRows(rows []DiffLine, op DiffOp, lines []string) []DiffLine {
	for _, line := range lines {
		rows = append(rows, DiffLine{Op: op, Text: line})
	}
	return rows
}
