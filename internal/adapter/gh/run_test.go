package gh_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/adapter/command"
	"github.com/O-Marsters-1997/command-center/internal/adapter/gh"
)

func TestCLI_Retry(t *testing.T) {
	t.Parallel()
	timeout := &command.Error{Cmd: "gh", Stderr: "dial tcp 140.82.112.5:443: connect: operation timed out", Err: errGH}
	notFound := &command.Error{Cmd: "gh", Stderr: "HTTP 404: Not Found", Err: errGH}
	readTitles := func(c gh.CLI) error {
		_, err := c.IssueTitles(context.Background(), "/repo")
		return err
	}
	closeIssue := func(c gh.CLI) error {
		return c.CloseIssue(context.Background(), "/repo", "https://github.com/o/r/issues/1")
	}

	t.Run("read recovers after timeouts", func(t *testing.T) {
		t.Parallel()
		calls := 0
		exec := command.ExecutorFunc(func(context.Context, time.Duration, string, string, ...string) ([]byte, error) {
			calls++
			if calls < 3 {
				return nil, timeout
			}
			return []byte("[]"), nil
		})

		if err := readTitles(gh.CLI{Exec: exec, Backoff: time.Nanosecond}); err != nil {
			t.Errorf("IssueTitles() error = %v, want nil", err)
		}
		if calls != 3 {
			t.Errorf("gh calls = %d, want 3", calls)
		}
	})

	t.Run("read stops retrying when the context is cancelled", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		calls := 0
		exec := command.ExecutorFunc(func(context.Context, time.Duration, string, string, ...string) ([]byte, error) {
			calls++
			cancel()
			return nil, timeout
		})

		_, err := gh.CLI{Exec: exec, Backoff: time.Hour}.IssueTitles(ctx, "/repo")

		if !errors.Is(err, context.Canceled) {
			t.Errorf("IssueTitles() error = %v, want context.Canceled", err)
		}
		if calls != 1 {
			t.Errorf("gh calls = %d, want 1", calls)
		}
	})

	failing := []struct {
		name      string
		failure   error
		call      func(gh.CLI) error
		wantCalls int
	}{
		{name: "read gives up after three attempts", failure: timeout, call: readTitles, wantCalls: 3},
		{name: "read does not retry a real gh error", failure: notFound, call: readTitles, wantCalls: 1},
		{name: "write does not retry a timeout", failure: timeout, call: closeIssue, wantCalls: 1},
	}
	for _, tt := range failing {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			exec := command.ExecutorFunc(func(context.Context, time.Duration, string, string, ...string) ([]byte, error) {
				calls++
				return nil, tt.failure
			})

			err := tt.call(gh.CLI{Exec: exec, Backoff: time.Nanosecond})

			if !errors.Is(err, errGH) {
				t.Errorf("error = %v, want %v", err, errGH)
			}
			if calls != tt.wantCalls {
				t.Errorf("gh calls = %d, want %d", calls, tt.wantCalls)
			}
		})
	}
}

func TestDecode(t *testing.T) {
	t.Parallel()

	if _, err := gh.Decode[gh.RepoSettings]([]byte("not json")); err == nil {
		t.Error("Decode(not json) error = nil, want one")
	}
}

func TestDecodeStream(t *testing.T) {
	t.Parallel()

	t.Run("concatenates pages", func(t *testing.T) {
		t.Parallel()
		got, err := gh.DecodeStream[int]([]byte(`[1,2][3]`))
		if err != nil {
			t.Fatalf("DecodeStream() error = %v", err)
		}
		if diff := cmp.Diff([]int{1, 2, 3}, got); diff != "" {
			t.Errorf("DecodeStream([1,2][3]) mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("empty output", func(t *testing.T) {
		t.Parallel()
		if _, err := gh.DecodeStream[int](nil); err == nil {
			t.Error("DecodeStream(nil) error = nil, want one")
		}
	})
}
