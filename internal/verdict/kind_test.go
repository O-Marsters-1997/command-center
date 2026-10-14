package verdict_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/verdict"
)

func TestEveryKindHasAWireName(t *testing.T) {
	t.Parallel()

	for k := verdict.Checking; k <= verdict.WaitingOnProducerDeploy; k++ {
		if k.String() == "" {
			t.Errorf("Kind(%d) has no wire name", k)
		}
	}
}
