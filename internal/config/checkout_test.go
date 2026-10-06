package config_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/config"
)

func TestCheckoutPathRefusesBothFormsOrNeither(t *testing.T) {
	t.Parallel()

	both := "[[repo]]\nname = \"r\"\nremote = \"git@github.com:o/r.git\"\npath = \"r\"\n"
	if _, err := config.LoadConfig(writeConfig(t, both)); err == nil ||
		!strings.Contains(err.Error(), "r") || !strings.Contains(err.Error(), "both") {
		t.Errorf("error = %v, want one naming the repo that sets both remote and path", err)
	}

	neither := "[[repo]]\nname = \"r\"\n"
	if _, err := config.LoadConfig(writeConfig(t, neither)); err == nil || !strings.Contains(err.Error(), "neither") {
		t.Errorf("error = %v, want one naming the repo that sets neither", err)
	}
}

func TestLoadConfigPutsARemoteRepoUnderTheDataDir(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("CC_DATA_DIR", dataDir)

	got, err := config.LoadConfig(writeConfig(t,
		"[[repo]]\nname = \"command-center\"\nremote = \"git@github.com:o/command-center.git\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dataDir, "repos", "command-center"); got.Repos[0].Checkout != want {
		t.Errorf("checkout = %q, want %q", got.Repos[0].Checkout, want)
	}
}

func TestLoadConfigRefusesARemoteRepoNamedLikeAPath(t *testing.T) {
	t.Setenv("CC_DATA_DIR", t.TempDir())

	_, err := config.LoadConfig(writeConfig(t,
		"[[repo]]\nname = \"../escape\"\nremote = \"git@github.com:o/r.git\"\n"))
	if err == nil || !strings.Contains(err.Error(), "escape") {
		t.Errorf("error = %v, want one refusing a repo name that is not a single directory", err)
	}
}
