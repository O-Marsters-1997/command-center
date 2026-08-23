// Command fakegh stands in for the gh CLI in end-to-end tests. It answers from a JSON fixture
// staged at $CC_GH_FIXTURE, keyed on the first two argv words ("pr list", "pr create", …). A
// fixture may also key on a longer argv prefix ("pr list --state all --head feat-x") to answer one
// shape of a call differently from the rest; the longest matching key wins.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// response is one fixture entry: what gh would have printed and exited with.
type response struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
	Exit   int    `json:"exit"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

func run(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	// Logged before the fixture is consulted so an unanswerable call still shows up in the log.
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

	key := matchKey(fixture, args)
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
		return openErr
	}
	defer func() { err = errors.Join(err, f.Close()) }()

	_, err = fmt.Fprintln(f, line)
	return err
}

// matchKey returns the longest fixture key that is a whole-word prefix of the argv, falling back
// to the two-word key when none is.
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

// printf writes to w, discarding the write error: these are diagnostics on a test fake.
func printf(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}
