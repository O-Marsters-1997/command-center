package agentlog

import (
	"io/fs"
	"path/filepath"
)

// LoadRequests reads every request from every *.jsonl transcript under dir, subagent transcripts
// included, since Claude Code writes those as ordinary sibling files. A dir that does not exist
// yet yields none rather than erroring.
func LoadRequests(dir string) ([]RequestUsage, error) {
	var requests []RequestUsage
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // pedantic here: a missing or unreadable dir weighs zero, not a bug to surface
		}
		if entry.IsDir() || filepath.Ext(path) != ".jsonl" {
			return nil
		}
		found, err := ParseTranscriptUsage(path)
		if err != nil {
			return nil //nolint:nilerr // pedantic here: an unreadable transcript weighs zero, not a bug to surface
		}
		requests = append(requests, found...)
		return nil
	})
	return requests, err
}
