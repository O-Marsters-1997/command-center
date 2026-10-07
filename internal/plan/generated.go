package plan

import (
	"path/filepath"
	"strings"
)

type GeneratedPolicy struct {
	Paths        []string
	BuildCommand []string
}

// AllGenerated reports whether every one of conflictedPaths is a path this repo's build
// regenerates. A repo that named no build command has opted out.
func AllGenerated(conflictedPaths []string, policy GeneratedPolicy) bool {
	if len(conflictedPaths) == 0 || len(policy.BuildCommand) == 0 {
		return false
	}
	for _, path := range conflictedPaths {
		if !generatedMatch(policy.Paths, path) {
			return false
		}
	}
	return true
}

func generatedMatch(patterns []string, path string) bool {
	for _, pattern := range patterns {
		if dir, ok := strings.CutSuffix(pattern, "/**"); ok {
			if path == dir || strings.HasPrefix(path, dir+"/") {
				return true
			}
			continue
		}
		if ok, _ := filepath.Match(pattern, path); ok {
			return true
		}
	}
	return false
}
