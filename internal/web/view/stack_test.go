package view

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestBuildStackCountsMembersWhoseOwnPRMerged(t *testing.T) {
	t.Parallel()

	merged := Row{URL: "sandbox://1", Feature: "billing", state: plan.PRMerged}
	open := Row{URL: "sandbox://2", Feature: "billing", state: plan.ReviewMe}
	elsewhere := Row{URL: "sandbox://3", Feature: "other", state: plan.PRMerged}
	board := Board{Groups: []Group{{Children: []Row{merged, open, elsewhere}}}}

	got := buildStack(board, open)

	if got.Merged != 1 || got.Total != 2 {
		t.Errorf("buildStack(open) = {Merged: %d, Total: %d}, want {Merged: 1, Total: 2}", got.Merged, got.Total)
	}
}
