package loop

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/adapter/git"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

const maxCommandHandoffBytes = 4000

type start struct {
	workflow plan.Workflow
	step     plan.Step
	verify   []string
}

func (l *Loop) handoffPath(id plan.RunID) string {
	return filepath.Join(l.ws.RunsDir, fmt.Sprintf("%d.handoff.md", id))
}

func (l *Loop) readHandoff(id plan.RunID) option.Option[string] {
	data, err := os.ReadFile(l.handoffPath(id))
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			l.logger.Warn("read handoff", "run", id, "err", err)
		}
		return option.None[string]()
	}
	return option.Some(string(data))
}

func (l *Loop) systemPromptFor(runID int64, spec spawnSpec) (string, error) {
	if spec.step.Prompt == "" || spec.step.Command {
		return l.ws.SystemPromptPath, nil
	}
	path := filepath.Join(l.ws.RunsDir, fmt.Sprintf("%d.system.md", runID))
	if err := os.WriteFile(path, []byte(agentSystemPrompt+"\n"+spec.step.Prompt), 0o600); err != nil {
		return "", fmt.Errorf("write system prompt for run %d: %w", runID, err)
	}
	return path, nil
}

func (l *Loop) workflowByHash(hash string) (plan.Workflow, bool) {
	if hash == "" {
		return plan.Workflow{}, false
	}
	for _, w := range l.workflows {
		if w.Hash == hash {
			return w, true
		}
	}
	return plan.Workflow{}, false
}

func (l *Loop) stepOf(run store.PendingRun) (plan.Workflow, plan.Step, bool) {
	w, ok := l.workflowByHash(run.WorkflowHash)
	if !ok {
		return plan.Workflow{}, plan.Step{}, false
	}
	i := slices.IndexFunc(w.Steps, func(s plan.Step) bool { return s.Name == run.Step })
	if i < 0 {
		return plan.Workflow{}, plan.Step{}, false
	}
	return w, w.Steps[i], true
}

func finalStep(w plan.Workflow, s plan.Step) bool { return w.Steps[len(w.Steps)-1].Name == s.Name }

func (l *Loop) startOf(entry plan.Entry, obs plan.Observation) start {
	w, ok := l.workflows[entry.Workflow]
	if !ok {
		w = l.workflows[config.DefaultWorkflow]
	}
	verify := obs.Settings[entry.Ticket.Repo].VerifyCommand
	return start{workflow: w, step: w.First(len(verify) > 0).UnwrapOr(w.Steps[0]), verify: verify}
}

func stepPrompt(ticket store.Ticket, launched plan.Ticket, step plan.Step, pass []plan.StepRun) string {
	if step.Prompt == "" {
		return agentPrompt(ticket, launched)
	}
	step.Prompt = ""
	prompt := strings.TrimSpace(plan.ComposeStep(launched, step, pass))
	if ticket.Body != "" {
		prompt += "\n\n## Ticket body\n\n" + ticket.Body
	}
	return prompt
}

func (l *Loop) advancePass(
	ctx context.Context, t *tick, ticket store.Ticket, entry plan.Entry,
) error {
	url := plan.TicketURL(ticket.URL)
	latest, err := l.store.LatestRunsByTicket(ctx)
	if err != nil {
		return fmt.Errorf("latest runs: %w", err)
	}
	summary := latest[url]
	w, ok := l.workflowByHash(summary.WorkflowHash)
	if !ok {
		return nil
	}
	worktree, ok := t.obs.Worktrees[ticket.Ref()]
	if !ok {
		return l.event(ctx, ticket.URL, store.EventSpawnFailed, "no worktree to continue the workflow in")
	}
	verify := t.obs.Settings[ticket.Repo].VerifyCommand
	passes, err := l.store.PassRuns(ctx)
	if err != nil {
		return fmt.Errorf("pass runs: %w", err)
	}
	pass := w.Pass(passes[url], len(verify) > 0)
	for i := range pass {
		pass[i].Handoff = l.readHandoff(pass[i].ID)
	}
	baseline, err := git.BranchTip(ctx, l.checkout(ticket.Repo), ticket.Branch)
	if err != nil {
		return fmt.Errorf("read baseline for %s: %w", ticket.URL, err)
	}
	step := entry.Next.Step.Unwrap()
	return l.spawnRun(ctx, t, spawnSpec{
		ticket: ticket, worktree: worktree, baseline: baseline, hash: summary.PromptHash,
		kind: plan.RunKindAgent, prompt: stepPrompt(ticket, entry.Ticket, step, pass), choice: entry.Ticket.Choice,
		workflow: w, step: step, verify: verify,
	})
}

func (l *Loop) passBaseline(
	ctx context.Context, w plan.Workflow, run store.PendingRun, hasVerify bool,
) (string, error) {
	passes, err := l.store.PassRuns(ctx)
	if err != nil {
		return "", fmt.Errorf("pass runs: %w", err)
	}
	pass := w.Pass(passes[plan.TicketURL(run.TicketID)], hasVerify)
	if len(pass) == 0 {
		return run.BaselineSHA, nil
	}
	baseline, err := l.store.RunBaseline(ctx, pass[0].ID)
	if err != nil {
		return "", fmt.Errorf("pass baseline: %w", err)
	}
	return baseline, nil
}

func (l *Loop) writeHandoff(run store.PendingRun, step plan.Step, lastMessage option.Option[string], failed bool) {
	var body string
	switch {
	case step.Writes:
		body = lastMessage.UnwrapOr("")
	case step.Command && failed:
		body = "The " + string(step.Name) + " command failed. Its output ended:\n\n" + l.tailOf(run.LogPath)
	}
	if strings.TrimSpace(body) == "" {
		return
	}
	if err := os.WriteFile(l.handoffPath(plan.RunID(run.ID)), []byte(body), 0o600); err != nil {
		l.logger.Warn("write handoff", "run", run.ID, "err", err)
	}
}

func (l *Loop) tailOf(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	buf := make([]byte, maxCommandHandoffBytes)
	n := 0
	if info, err := f.Stat(); err == nil {
		n, err = f.ReadAt(buf, max(0, info.Size()-maxCommandHandoffBytes))
		if err != nil && !errors.Is(err, io.EOF) {
			n = 0
		}
	}
	if err := f.Close(); err != nil {
		l.logger.Warn("close command log", "path", path, "err", err)
	}
	return strings.ToValidUTF8(string(buf[:n]), "")
}

func (l *Loop) prBody(ctx context.Context, url plan.TicketURL, stacked string) string {
	latest, err := l.store.LatestRunsByTicket(ctx)
	if err != nil {
		l.logger.Warn("read latest run for pr body", "ticket", url, "err", err)
		return stacked
	}
	handoff := strings.TrimSpace(l.readHandoff(latest[url].ID).UnwrapOr(""))
	switch {
	case handoff == "":
		return stacked
	case stacked == "":
		return handoff
	default:
		return stacked + "\n\n" + handoff
	}
}
