package plan_test

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestSnapshot_Preview(t *testing.T) {
	t.Parallel()

	type wantRow struct {
		label plan.PreviewLabel
		base  string
	}

	first := ticket("1", "cc-1-first")
	second := ticket("2", "cc-2-second", first.URL)
	third := ticket("3", "cc-3-third", "sandbox://CC-9")
	tickets := []plan.Ticket{first, second, third, ticket("9", "cc-9-ninth")}

	tests := []struct {
		name      string
		rules     plan.Rules
		in        plan.Input
		selection []plan.TicketURL
		want      map[plan.TicketURL]wantRow
	}{
		{
			name:      "a root starts now and a blocker in the slice starts on unlock, both off main",
			in:        plan.Input{Tickets: tickets},
			selection: []plan.TicketURL{first.URL, second.URL},
			want: map[plan.TicketURL]wantRow{
				first.URL:  {plan.Now, "main"},
				second.URL: {plan.OnUnlock, "main"},
			},
		},
		{
			name:      "a blocker outside the slice is refused",
			in:        plan.Input{Tickets: tickets},
			selection: []plan.TicketURL{third.URL},
			want:      map[plan.TicketURL]wantRow{third.URL: {plan.Refused, "main"}},
		},
		{
			name:      "stacking cuts an on-unlock row from its blocker's branch",
			rules:     plan.Rules{Stacking: map[string]bool{"r": true}},
			in:        plan.Input{Tickets: tickets},
			selection: []plan.TicketURL{first.URL, second.URL},
			want: map[plan.TicketURL]wantRow{
				first.URL:  {plan.Now, "main"},
				second.URL: {plan.OnUnlock, first.Branch},
			},
		},
		{
			name: "an already authorised ticket is refused",
			in: plan.Input{
				Tickets:     tickets,
				Memberships: map[plan.TicketURL]plan.LaunchMembership{first.URL: {LaunchID: 7}},
			},
			selection: []plan.TicketURL{first.URL},
			want:      map[plan.TicketURL]wantRow{first.URL: {plan.Refused, "main"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rows, err := derive(tt.rules, tt.in).Preview(tt.selection, "")
			if err != nil {
				t.Fatalf("Preview(%v) error = %v", tt.selection, err)
			}
			got := map[plan.TicketURL]wantRow{}
			for _, row := range rows {
				got[row.Ticket.URL] = wantRow{label: row.Label, base: row.Base}
				if row.Prompt != plan.Compose(row.Ticket) || row.PromptHash != plan.Hash(row.Prompt) {
					t.Errorf("Preview(%v) row %s prompt and hash do not follow Compose", tt.selection, row.Ticket.URL)
				}
			}
			if diff := cmp.Diff(tt.want, got, cmp.AllowUnexported(wantRow{})); diff != "" {
				t.Errorf("Preview(%v) mismatch (-want +got):\n%s", tt.selection, diff)
			}
		})
	}

	t.Run("an unknown ticket", func(t *testing.T) {
		t.Parallel()
		snap := derive(plan.Rules{}, plan.Input{Tickets: []plan.Ticket{ticket("1", "cc-1-first")}})
		rows, err := snap.Preview([]plan.TicketURL{"sandbox://CC-404"}, "")
		if !errors.Is(err, plan.ErrUnknownTicket) || rows != nil {
			t.Errorf("Preview(sandbox://CC-404) = %v, %v, want nil, ErrUnknownTicket", rows, err)
		}
	})

	t.Run("a stacked row carries its base ticket's run", func(t *testing.T) {
		t.Parallel()
		first := ticket("1", "cc-1-first")
		second := ticket("2", "cc-2-second", first.URL)
		snap := derive(plan.Rules{Stacking: map[string]bool{"r": true}}, plan.Input{
			Tickets: []plan.Ticket{first, second},
			Runs:    map[plan.TicketURL]plan.RunSummary{first.URL: {ID: 1}},
		})

		rows, err := snap.Preview([]plan.TicketURL{second.URL}, "")
		if err != nil {
			t.Fatalf("Preview(%s) error = %v", second.URL, err)
		}
		if base, _ := snap.Entry(first.URL); rows[0].BaseRun.IsNone() || base.Run.IsNone() {
			t.Errorf("Preview(%s).BaseRun = %v, want the base ticket's run %v", second.URL, rows[0].BaseRun, base.Run)
		}

		rows, err = snap.Preview([]plan.TicketURL{first.URL}, "")
		if err != nil {
			t.Fatalf("Preview(%s) error = %v", first.URL, err)
		}
		if rows[0].BaseRun.IsSome() {
			t.Errorf("Preview(%s).BaseRun = %v, want None for a main-based row", first.URL, rows[0].BaseRun)
		}
	})
}
