package loop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/O-Marsters-1997/treepad"

	"github.com/O-Marsters-1997/command-center/internal/adapter/git"
	"github.com/O-Marsters-1997/command-center/internal/adapter/runner"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

type spawnSpec struct {
	ticket                           store.Ticket
	worktree, baseline, hash, prompt string
	kind                             plan.RunKind
	choice                           plan.AgentChoice
	resumeSessionID, message         string
	workflow                         plan.Workflow
	step                             plan.Step
	verify                           []string
	cause                            plan.FollowUpCause
	review                           []plan.ReviewComment
}

func (spec spawnSpec) session() store.RunSession {
	switch {
	case spec.resumeSessionID != "":
		return store.RunSession{JoinID: spec.resumeSessionID, Message: spec.message}
	case spec.kind.StartsSession():
		return store.RunSession{StartNew: true, Message: spec.message}
	default:
		return store.RunSession{}
	}
}

func (s start) spec(
	ticket store.Ticket, launched plan.Ticket, worktree, baseline, hash string, pass []plan.StepRun,
) spawnSpec {
	return spawnSpec{
		ticket: ticket, worktree: worktree, baseline: baseline, hash: hash,
		kind: plan.RunKindAgent, prompt: stepPrompt(ticket, launched, s.step, pass), choice: launched.Choice,
		workflow: s.workflow, step: s.step, verify: s.verify,
	}
}

func agentPrompt(ticket store.Ticket, launched plan.Ticket) string {
	prompt := plan.Compose(launched)
	if ticket.Body != "" {
		prompt += "\n\n## Ticket\n\n" + ticket.Body
	}
	return prompt
}

func (l *Loop) launchEligible(
	ctx context.Context, snap plan.Snapshot, t *tick, byTicket map[plan.TicketURL]store.Ticket,
) error {
	for _, ticketURL := range snap.LaunchAfter(t.spawned) {
		if slices.Contains(t.spawned, ticketURL) {
			continue
		}
		entry, _ := snap.Entry(ticketURL)
		var err error
		if entry.Run.IsSome() {
			err = l.advancePass(ctx, t, byTicket[ticketURL], entry)
		} else {
			_, err = l.cutAndSpawn(ctx, t, byTicket[ticketURL], entry.Ticket, entry.Unlock.BaseBranch, entry.PromptHash,
				l.startOf(entry, t.obs))
		}
		if err != nil {
			return fmt.Errorf("%s: %w", ticketURL, err)
		}
	}
	return nil
}

func (l *Loop) cutAndSpawn(
	ctx context.Context, t *tick, ticket store.Ticket, launched plan.Ticket, baseBranch, promptHash string,
	begin start,
) (plan.RunID, error) {
	branch := ticket.Branch
	repoPath := l.checkout(ticket.Repo)

	err := l.worktrees.New(ctx, repoPath, branch, "origin/"+baseBranch)
	if errors.Is(err, treepad.ErrPostHook) {
		l.logger.Warn("post hook failed after cutting worktree", "branch", branch, "err", err)
		err = nil
	}
	if err != nil {
		return l.recordCutFailure(ctx, ticket, promptHash, begin, err)
	}

	baselineSHA, err := l.baseline(ctx, ticket)
	if err != nil {
		return 0, err
	}

	worktrees, err := git.WorktreePaths(ctx, repoPath)
	if err != nil {
		return 0, fmt.Errorf("list worktrees after cutting %s: %w", branch, err)
	}
	worktreePath, ok := worktrees[branch]
	if !ok {
		return 0, fmt.Errorf("cut of %s reported success but git worktree list does not show it", branch)
	}

	runID, err := l.spawnRun(ctx, t, begin.spec(ticket, launched, worktreePath, baselineSHA, promptHash, nil))
	if err != nil {
		return 0, fmt.Errorf("spawn agent in %s: %w", worktreePath, err)
	}
	return runID, nil
}

func (l *Loop) recordCutFailure(
	ctx context.Context, ticket store.Ticket, promptHash string, begin start, cutErr error,
) (plan.RunID, error) {
	failed := store.RunSkeleton{
		Ticket: ticket.URL, PromptHash: promptHash,
		Step: begin.step.Name, Workflow: begin.workflow.Name, WorkflowHash: begin.workflow.Hash,
	}
	failedID, insertErr := l.store.InsertCutFailedRun(ctx, failed, l.clock.Now())
	if insertErr != nil {
		return 0, errors.Join(
			fmt.Errorf("cut %s: %w", ticket.Branch, cutErr),
			fmt.Errorf("record failed cut of %s: %w", ticket.Branch, insertErr),
		)
	}
	return plan.RunID(failedID), l.event(ctx, ticket.URL, store.EventCutFailed, cutErr.Error())
}

func (l *Loop) spawnRun(ctx context.Context, t *tick, spec spawnSpec) (id plan.RunID, err error) {
	spec = l.withDefaults(spec)
	ticket := spec.ticket
	runID, sessionID, err := l.store.InsertSessionRunSkeleton(ctx, store.RunSkeleton{
		Ticket: ticket.URL, Kind: spec.kind, BaselineSHA: spec.baseline, PromptHash: spec.hash,
		Choice: spec.choice, Session: spec.session(), Cause: spec.cause,
		Step: spec.step.Name, Workflow: spec.workflow.Name, WorkflowHash: spec.workflow.Hash,
	})
	if err != nil {
		return 0, fmt.Errorf("insert run skeleton for %s: %w", ticket.URL, err)
	}
	id = plan.RunID(runID)

	spawnCfg, err := l.spawnConfig(ctx, spec, runID, sessionID)
	if err != nil {
		return id, err
	}
	logPath := filepath.Join(l.ws.RunsDir, fmt.Sprintf("%d.jsonl", runID))
	logFile, err := os.Create(logPath)
	if err != nil {
		return id, fmt.Errorf("open log for run %d: %w", runID, err)
	}
	defer func() {
		if closeErr := logFile.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close log for run %d: %w", runID, closeErr))
		}
	}()
	spawnCfg.LogFile = logFile

	result, err := l.runner.Spawn(ctx, spawnCfg)
	if err != nil {
		return id, l.recordSpawnFailure(ctx, ticket.URL, runID, err)
	}

	spawned := store.Spawn{RunID: id, Pgid: result.Pid, StartedAt: l.clock.Now(), LogPath: logPath}
	if err := l.store.RecordSpawn(ctx, spawned); err != nil {
		return id, fmt.Errorf("record spawn of run %d: %w", runID, err)
	}
	t.spawned = append(t.spawned, plan.TicketURL(ticket.URL))
	if spec.kind.IsCommand() {
		t.spawnedCommands++
	}
	detail := fmt.Sprintf("spawned pid %d in %s", result.Pid, spec.worktree)
	if spec.baseline != "" {
		detail += " from " + spec.baseline
	}
	return id, l.event(ctx, ticket.URL, store.EventRunLaunched, detail)
}

func (l *Loop) withDefaults(spec spawnSpec) spawnSpec {
	if spec.workflow.Name == "" {
		spec.workflow = l.workflows[config.DefaultWorkflow]
		if spec.step.Name == "" {
			spec.step = spec.workflow.Steps[0]
		}
	}
	switch {
	case spec.step.Command:
		if spec.kind != plan.RunKindVerify {
			spec.kind = plan.RunKindCommand
		}
		spec.choice = plan.AgentChoice{}
	case spec.step.Model != "":
		spec.choice = plan.AgentChoice{Model: spec.step.Model}
	case spec.choice.Model == "":
		spec.choice.Model = config.AgentModel
	}
	return spec
}

func (l *Loop) spawnConfig(
	ctx context.Context, spec spawnSpec, runID int64, sessionID string,
) (runner.SpawnConfig, error) {
	step := spec.step
	if step.Command {
		return runner.SpawnConfig{AgentCommand: spec.verify, WorktreePath: spec.worktree}, nil
	}

	prompt := spec.prompt
	if len(spec.review) > 0 {
		attached, err := l.writeReviewAttachment(runID, spec.review)
		if err != nil {
			return runner.SpawnConfig{}, err
		}
		placeholder := filepath.Join(l.ws.RunsDir, plan.RunIDPlaceholder+"."+plan.ReviewAttachmentName)
		prompt = strings.ReplaceAll(prompt, placeholder, attached)
	}
	canAsk := len(step.Tools) == 0 || slices.Contains(step.Tools, "Write")
	if canAsk && spec.kind.Steerable() {
		prompt += plan.ComposeAsking(l.questionPath(plan.RunID(runID)))
	}
	promptPath := filepath.Join(l.ws.RunsDir, fmt.Sprintf("%d.prompt", runID))
	if err := os.WriteFile(promptPath, []byte(prompt), 0o600); err != nil {
		return runner.SpawnConfig{}, fmt.Errorf("write prompt for run %d: %w", runID, err)
	}
	if err := l.store.RecordPrompt(ctx, runID, prompt); err != nil {
		return runner.SpawnConfig{}, fmt.Errorf("store prompt for run %d: %w", runID, err)
	}
	systemPromptPath, err := l.systemPromptFor(runID, spec)
	if err != nil {
		return runner.SpawnConfig{}, err
	}

	cfg := runner.SpawnConfig{
		AgentCommand: l.cfg.AgentCommand,
		WorktreePath: spec.worktree,
		SettingsPath: l.ws.SettingsPath,
		Model:        spec.choice.Model,
		Effort:       spec.choice.Effort,
		Tools:        step.Tools,
		Prompt:       prompt,
		PromptPath:   promptPath,
		SessionID:    sessionID,
		Resume:       spec.resumeSessionID != "",
	}
	if spec.kind.TakesSystemPrompt() {
		cfg.SystemPromptPath = systemPromptPath
	}
	return cfg, nil
}

func (l *Loop) writeReviewAttachment(runID int64, comments []plan.ReviewComment) (string, error) {
	raw, err := json.MarshalIndent(comments, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode review comments for run %d: %w", runID, err)
	}
	path := filepath.Join(l.ws.RunsDir, fmt.Sprintf("%d.%s", runID, plan.ReviewAttachmentName))
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return "", fmt.Errorf("write review comments for run %d: %w", runID, err)
	}
	return path, nil
}

func (l *Loop) recordSpawnFailure(ctx context.Context, ticketURL string, runID int64, spawnErr error) error {
	failed := store.Disposition{
		RunID: plan.RunID(runID), Outcome: plan.Disposition(0),
		ExitCode: option.None[int](), Cost: option.None[float64](), EndedAt: l.clock.Now(),
	}
	if err := l.store.RecordDisposition(ctx, failed); err != nil {
		return errors.Join(
			fmt.Errorf("spawn run %d: %w", runID, spawnErr),
			fmt.Errorf("record disposition for failed spawn of run %d: %w", runID, err),
		)
	}
	return l.event(ctx, ticketURL, store.EventSpawnFailed, spawnErr.Error())
}
