package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
)

type response struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
	Exit   int    `json:"exit"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

func run(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	if logPath := getenv("CC_GH_LOG"); logPath != "" {
		if err := appendLine(logPath, strings.Join(args, " ")); err != nil {
			printf(stderr, "fakegh: %v\n", err)
			return 1
		}
	}

	path := getenv("CC_GH_FIXTURE")
	if path == "" {
		printf(stderr, "fakegh: CC_GH_FIXTURE is not set\n")
		return 1
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		printf(stderr, "fakegh: %v\n", err)
		return 1
	}
	var fixture map[string]response
	if err := json.Unmarshal(raw, &fixture); err != nil {
		printf(stderr, "fakegh: %s: %v\n", path, err)
		return 1
	}

	key := matchKey(fixture, slices.DeleteFunc(slices.Clone(args), func(a string) bool { return a == "--paginate" }))
	resp, ok := fixture[key]
	if !ok {
		printf(stderr, "fakegh: no fixture entry for %q\n", key)
		return 1
	}
	printf(stdout, "%s", resp.Stdout)
	printf(stderr, "%s", resp.Stderr)
	return resp.Exit
}

func appendLine(path, line string) (err error) {
	f, openErr := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if openErr != nil {
		return fmt.Errorf("open %s: %w", path, openErr)
	}
	defer func() { err = errors.Join(err, f.Close()) }()

	if _, writeErr := fmt.Fprintln(f, line); writeErr != nil {
		return fmt.Errorf("append to %s: %w", path, writeErr)
	}
	return nil
}

func matchKey(fixture map[string]response, args []string) string {
	joined := strings.Join(args, " ") + " "
	best := fixtureKey(args)
	for key := range fixture {
		if len(key) > len(best) && strings.HasPrefix(joined, key+" ") {
			best = key
		}
	}
	return best
}

func fixtureKey(args []string) string {
	if len(args) > 2 {
		args = args[:2]
	}
	return strings.Join(args, " ")
}

func printf(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}
