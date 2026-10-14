package agentlog

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
)

// LoadRequests reads every request from every *.jsonl transcript under dir, subagent transcripts
// included, since Claude Code writes those as ordinary sibling files. A dir that does not exist
// yet yields none rather than erroring.
func LoadRequests(dir string) ([]RequestUsage, error) {
	var requests []RequestUsage
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("walk %s: %w", path, err)
		}
		if entry.IsDir() || filepath.Ext(path) != ".jsonl" {
			return nil
		}
		found, err := ParseTranscriptUsage(path)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("parse transcript %s: %w", path, err)
		}
		requests = append(requests, found...)
		return nil
	})
	if err != nil {
		return requests, fmt.Errorf("load requests from %s: %w", dir, err)
	}
	return requests, nil
}
