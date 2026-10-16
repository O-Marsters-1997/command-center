package agentlog

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
)

// LoadRequests reads usage from all JSONL transcripts under dir.
// Claude Code writes subagent transcripts alongside the main transcript.
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
		return requests, fmt.Errorf("load transcript requests: %w", err)
	}
	return requests, nil
}
