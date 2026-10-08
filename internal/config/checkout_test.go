package config_test

import (
	"path/filepath"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/config"
)

func TestCheckoutPathNestsOwnerAndName(t *testing.T) {
	t.Parallel()

	got := config.CheckoutPath("/data", "O-Marsters-1997/command-center")
	if want := filepath.Join("/data", "repos", "O-Marsters-1997", "command-center"); got != want {
		t.Errorf("CheckoutPath = %q, want %q", got, want)
	}
}
