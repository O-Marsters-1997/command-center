package plan_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestSnapshotPreview(t *testing.T) {
	t.Parallel()

	first := ticket("1", "cc-1-first")
	second := ticket("2", "cc-2-second", first.URL)
	third := ticket("3", "cc-3-third", "sandbox://CC-9")
	tickets := []plan.Ticket{first, second, third, ticket("9", "cc-9-ninth")}

	tests := []struct {
		name      string
		rules     plan.Rules
		in        plan.Input
		selection []string
		want      map[string]struct {
			label plan.PreviewLabel
			base  string
		}
		wantErr bool
	}{
		{
			name:      "a root starts now and a blocker in the slice starts on unlock, both off main",
			in:        plan.Input{Tickets: tickets},
			selection: []string{first.URL, second.URL},
			want: map[string]struct {
				label plan.PreviewLabel
				base  string
			}{
				first.URL:  {plan.Now, "main"},
				second.URL: {plan.OnUnlock, "main"},
			},
		},
		{
			name:      "a blocker outside the slice is refused",
			in:        plan.Input{Tickets: tickets},
			selection: []string{third.URL},
			want: map[string]struct {
				label plan.PreviewLabel
				base  string
			}{third.URL: {plan.Refused, "main"}},
		},
		{
			name:      "stacking cuts an on-unlock row from its blocker's branch",
			rules:     plan.Rules{Stacking: map[string]bool{"r": true}},
			in:        plan.Input{Tickets: tickets},
			selection: []string{first.URL, second.URL},
			want: map[string]struct {
				label plan.PreviewLabel
				base  string
			}{
				first.URL:  {plan.Now, "main"},
				second.URL: {plan.OnUnlock, first.Branch},
			},
		},
		{
			name: "an already authorised ticket is refused",
			in: plan.Input{
				Tickets:     tickets,
				Memberships: map[string]plan.LaunchMembership{first.URL: {LaunchID: 7}},
			},
			selection: []string{first.URL},
			want: map[string]struct {
				label plan.PreviewLabel
				base  string
			}{first.URL: {plan.Refused, "main"}},
		},
		{
			name:      "an unknown ticket is an error",
			in:        plan.Input{Tickets: tickets},
			selection: []string{"sandbox://CC-404"},
			wantErr:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rows, err := derive(tt.rules, tt.in).Preview(tt.selection)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Preview error = %v, wantErr %v", err, tt.wantErr)
			}
			if len(rows) != len(tt.want) {
				t.Fatalf("Preview returned %d rows, want %d", len(rows), len(tt.want))
			}
			for _, row := range rows {
				want := tt.want[row.Ticket.URL]
				if row.Label != want.label || row.Base != want.base {
					t.Errorf("%s = %v on %q, want %v on %q",
						row.Ticket.URL, row.Label, row.Base, want.label, want.base)
				}
				if row.Prompt != plan.Compose(row.Ticket) || row.PromptHash != plan.Hash(row.Prompt) {
					t.Errorf("%s prompt and hash do not follow Compose", row.Ticket.URL)
				}
			}
		})
	}
}

func TestSnapshotPreviewCarriesTheBaseTicketsRunForAStackedRow(t *testing.T) {
	t.Parallel()

	first := ticket("1", "cc-1-first")
	second := ticket("2", "cc-2-second", first.URL)
	snap := derive(plan.Rules{Stacking: map[string]bool{"r": true}}, plan.Input{
		Tickets: []plan.Ticket{first, second},
		Runs:    map[string]plan.RunSummary{first.URL: {ID: 1}},
	})

	rows, err := snap.Preview([]string{second.URL})
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := snap.Entry(first.URL); rows[0].BaseRun == nil || want.Run == nil {
		t.Fatalf("BaseRun = %v, want the base ticket's run %v", rows[0].BaseRun, want.Run)
	}

	rows, err = snap.Preview([]string{first.URL})
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].BaseRun != nil {
		t.Errorf("a main-based row has BaseRun %v, want nil", rows[0].BaseRun)
	}
}
