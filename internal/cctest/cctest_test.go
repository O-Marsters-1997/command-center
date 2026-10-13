package cctest

import (
	"context"
	"strings"
	"testing"
)

func TestStartContainerGivingUpNamesTheServerVariable(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := startContainer(ctx)
	if err == nil {
		t.Fatal("startContainer with a cancelled context = nil error, want one")
	}
	if !strings.Contains(err.Error(), serverEnv) {
		t.Errorf("startContainer error %q does not name %s", err, serverEnv)
	}
}
