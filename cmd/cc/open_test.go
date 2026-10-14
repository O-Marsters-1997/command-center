//go:build !e2e

package main

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/config"
)

func TestOpenWritesThroughInjectedWriters(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(t.TempDir(), "config.toml")
	body := fmt.Sprintf("port = %d\ndata_dir = %q\ndatabase_url = %q\n",
		port, t.TempDir(), "postgres://cc@127.0.0.1:1/cc?sslmode=disable&connect_timeout=1")
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	env := config.Env{Home: t.TempDir()}
	if err := open(t.Context(), configPath, env, &stdout, &stderr); err != nil {
		t.Fatalf("open: %v", err)
	}

	if want := fmt.Sprintf("http://127.0.0.1:%d/\n", port); stdout.String() != want {
		t.Errorf("stdout = %q, want %q", stdout.String(), want)
	}
	for _, want := range []string{"cc open: cannot read tracked repos", "no daemon listening on port"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr = %q, want it to contain %q", stderr.String(), want)
		}
	}
}
