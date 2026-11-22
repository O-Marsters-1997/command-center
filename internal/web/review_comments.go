package web

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func commentTarget(r *http.Request) (view.DiffLine, error) {
	target := view.DiffLine{Path: r.FormValue("path"), Side: plan.Side(r.FormValue("side"))}
	target.Line, _ = strconv.Atoi(r.FormValue("line"))
	if target.Path == "" || target.Line < 1 || (target.Side != plan.SideOld && target.Side != plan.SideNew) {
		return view.DiffLine{}, errorf(http.StatusBadRequest,
			"comment target %q %q %q is not a diff line", target.Path, target.Side, r.FormValue("line"))
	}
	return target, nil
}

func (s *Server) handleCommentForm(w http.ResponseWriter, r *http.Request) error {
	target, err := commentTarget(r)
	if err != nil {
		return err
	}
	ref := sessionRef(r)
	form := view.ReviewForm{SessionPath: ref.Path(), Path: target.Path, Side: target.Side, Line: target.Line}
	return renderHTML(w, "reviewForm", form)
}

func (s *Server) handleAddComment(w http.ResponseWriter, r *http.Request) error {
	target, err := commentTarget(r)
	if err != nil {
		return err
	}
	body := strings.TrimSpace(r.FormValue("body"))
	if body == "" {
		return errorf(http.StatusBadRequest, "comment body is empty")
	}
	ref := sessionRef(r)
	now := s.clock.Now()
	anchor, err := s.view.CommentAnchor(r.Context(), now, ref, target)
	if err != nil {
		return fmt.Errorf("anchor comment: %w", err)
	}
	c := plan.ReviewComment{
		Tip: anchor.Tip, Path: target.Path, Line: target.Line, Side: target.Side, Hunk: anchor.Hunk, Body: body,
	}
	c.ID, err = s.store.AddReviewComment(r.Context(), store.NewReviewComment{
		Ticket: anchor.Ticket, ReviewComment: c, At: now,
	})
	if err != nil {
		return fmt.Errorf("add comment: %w", err)
	}
	return s.renderThread(w, r, anchor.Ticket, view.ReviewThread{SessionPath: ref.Path(), Comment: c}, "reviewPosted")
}

func (s *Server) pendingComment(r *http.Request) (plan.TicketURL, plan.ReviewComment, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return "", plan.ReviewComment{}, errorf(http.StatusBadRequest, "comment id %q is not a number", r.PathValue("id"))
	}
	ticket, err := s.view.SessionTicket(r.Context(), s.clock.Now(), sessionRef(r))
	if err != nil {
		return "", plan.ReviewComment{}, fmt.Errorf("resolve ticket: %w", err)
	}
	comments, err := s.store.ReviewComments(r.Context(), ticket)
	if err != nil {
		return "", plan.ReviewComment{}, fmt.Errorf("load comments: %w", err)
	}
	i := slices.IndexFunc(comments, func(c plan.ReviewComment) bool { return c.ID == id })
	if i < 0 {
		return "", plan.ReviewComment{}, errorf(http.StatusNotFound, "no pending comment %d", id)
	}
	return ticket, comments[i], nil
}

func (s *Server) handleDeleteComment(w http.ResponseWriter, r *http.Request) error {
	ticket, c, err := s.pendingComment(r)
	if err != nil {
		return err
	}
	if err := s.store.DeleteReviewComment(r.Context(), ticket, c.ID); err != nil {
		return fmt.Errorf("delete comment: %w", err)
	}
	return s.renderThread(w, r, ticket, view.ReviewThread{SessionPath: sessionRef(r).Path(), Comment: c}, "reviewDeleted")
}

func (s *Server) handleComment(w http.ResponseWriter, r *http.Request) error {
	_, c, err := s.pendingComment(r)
	if err != nil {
		return err
	}
	return renderHTML(w, "reviewThread", view.ReviewThread{SessionPath: sessionRef(r).Path(), Comment: c})
}

func (s *Server) handleEditCommentForm(w http.ResponseWriter, r *http.Request) error {
	_, c, err := s.pendingComment(r)
	if err != nil {
		return err
	}
	return renderHTML(w, "reviewEdit", view.ReviewThread{SessionPath: sessionRef(r).Path(), Comment: c})
}

func (s *Server) handleEditComment(w http.ResponseWriter, r *http.Request) error {
	ticket, c, err := s.pendingComment(r)
	if err != nil {
		return err
	}
	body := strings.TrimSpace(r.FormValue("body"))
	if body == "" {
		return errorf(http.StatusBadRequest, "comment body is empty")
	}
	if err := s.store.EditReviewComment(r.Context(), ticket, c.ID, body); err != nil {
		return fmt.Errorf("edit comment: %w", err)
	}
	c.Body = body
	return renderHTML(w, "reviewThread", view.ReviewThread{SessionPath: sessionRef(r).Path(), Comment: c})
}

func (s *Server) handleRestoreComment(w http.ResponseWriter, r *http.Request) error {
	target, err := commentTarget(r)
	if err != nil {
		return err
	}
	body := strings.TrimSpace(r.FormValue("body"))
	tip := plan.SHA(r.FormValue("tip"))
	if body == "" || tip == "" {
		return errorf(http.StatusBadRequest, "a restored comment needs its tip and body")
	}
	ref := sessionRef(r)
	now := s.clock.Now()
	ticket, err := s.view.SessionTicket(r.Context(), now, ref)
	if err != nil {
		return fmt.Errorf("resolve ticket: %w", err)
	}
	c := plan.ReviewComment{
		Tip: tip, Path: target.Path, Line: target.Line, Side: target.Side, Hunk: r.FormValue("hunk"), Body: body,
	}
	c.ID, err = s.store.AddReviewComment(r.Context(), store.NewReviewComment{Ticket: ticket, ReviewComment: c, At: now})
	if err != nil {
		return fmt.Errorf("restore comment: %w", err)
	}
	thread := view.ReviewThread{SessionPath: ref.Path(), Comment: c}
	return s.renderThread(w, r, ticket, thread, "reviewRestored")
}

func (s *Server) handleRequestChanges(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	ref := sessionRef(r)
	now := s.clock.Now()
	ticket, snap, err := s.view.SessionSnapshot(ctx, now, ref)
	if err != nil {
		return fmt.Errorf("resolve ticket: %w", err)
	}
	if err := requireOffer(snap, ticket, plan.VerbFollowUp); err != nil {
		return err
	}
	comments, err := s.store.ReviewComments(ctx, ticket)
	if err != nil {
		return fmt.Errorf("load comments: %w", err)
	}
	note := strings.TrimSpace(r.FormValue("note"))
	if note == "" && len(comments) == 0 {
		return errorf(http.StatusBadRequest, "add a comment or a note before requesting changes")
	}
	choice, err := parseAgentChoice(r.FormValue("model"), r.FormValue("effort"))
	if err != nil {
		return errorf(http.StatusBadRequest, "%v", err)
	}
	payload := store.FollowUpPayload{
		Message: note, Model: choice.Model, Effort: choice.Effort.UnwrapOr(""), Comments: comments,
	}
	if err := s.store.RequestChanges(ctx, ticket, payload, now); err != nil {
		return fmt.Errorf("request changes: %w", err)
	}
	s.nudge()
	sent := view.NewRequestChanges(ref.Path(), "", true, "", note, comments).
		Sent(cmp.Or(choice.Model, config.AgentModel), payload.Effort)
	return renderFragment(w, r, ref.Path(), "requestSent", sent)
}

func (s *Server) handleRequestDialog(w http.ResponseWriter, r *http.Request) error {
	ref := sessionRef(r)
	req, err := s.view.RequestChanges(r.Context(), s.clock.Now(), ref, r.FormValue("note"))
	if err != nil {
		return fmt.Errorf("load request changes: %w", err)
	}
	return renderFragment(w, r, ref.Path()+"/changes", "requestBody", req)
}

func (s *Server) handleRequestPreview(w http.ResponseWriter, r *http.Request) error {
	ref := sessionRef(r)
	req, err := s.view.RequestChanges(r.Context(), s.clock.Now(), ref, r.FormValue("note"))
	if err != nil {
		return fmt.Errorf("load request changes: %w", err)
	}
	return renderFragment(w, r, ref.Path()+"/changes", "requestPreview", req.SwapPart())
}

func (s *Server) handleRemoveRequestComment(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return errorf(http.StatusBadRequest, "comment id %q is not a number", r.PathValue("id"))
	}
	ref := sessionRef(r)
	ticket, err := s.view.SessionTicket(ctx, s.clock.Now(), ref)
	if err != nil {
		return fmt.Errorf("resolve ticket: %w", err)
	}
	comments, err := s.store.ReviewComments(ctx, ticket)
	if err != nil {
		return fmt.Errorf("load comments: %w", err)
	}
	i := slices.IndexFunc(comments, func(c plan.ReviewComment) bool { return c.ID == id })
	if i < 0 {
		return errorf(http.StatusNotFound, "no pending comment %d", id)
	}
	if err := s.store.DeleteReviewComment(ctx, ticket, id); err != nil {
		return fmt.Errorf("delete comment: %w", err)
	}
	req, err := s.view.RequestChanges(ctx, s.clock.Now(), ref, r.FormValue("note"))
	if err != nil {
		return fmt.Errorf("load request changes: %w", err)
	}
	removed := view.RemovedComment{
		RequestItem: view.RequestItem{SessionPath: ref.Path(), Comment: comments[i]}, Part: req.SwapPart(),
	}
	return renderFragment(w, r, ref.Path()+"/changes", "requestRemoved", removed)
}

func (s *Server) handleRestoreRequestComment(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	ref := sessionRef(r)
	now := s.clock.Now()
	target, err := commentTarget(r)
	if err != nil {
		return err
	}
	body := strings.TrimSpace(r.FormValue("body"))
	tip := plan.SHA(r.FormValue("tip"))
	if body == "" || tip == "" {
		return errorf(http.StatusBadRequest, "a restored comment needs its tip and body")
	}
	ticket, err := s.view.SessionTicket(ctx, now, ref)
	if err != nil {
		return fmt.Errorf("resolve ticket: %w", err)
	}
	c := plan.ReviewComment{
		Tip: tip, Path: target.Path, Line: target.Line, Side: target.Side, Hunk: r.FormValue("hunk"), Body: body,
	}
	c.ID, err = s.store.AddReviewComment(ctx, store.NewReviewComment{Ticket: ticket, ReviewComment: c, At: now})
	if err != nil {
		return fmt.Errorf("restore comment: %w", err)
	}
	req, err := s.view.RequestChanges(ctx, now, ref, r.FormValue("note"))
	if err != nil {
		return fmt.Errorf("load request changes: %w", err)
	}
	restored := view.RestoredComment{
		RequestItem: view.RequestItem{SessionPath: ref.Path(), Comment: c}, Part: req.SwapPart(),
	}
	return renderFragment(w, r, ref.Path()+"/changes", "requestRestored", restored)
}

const raisePollEvery = 200 * time.Millisecond

func (s *Server) handleRaise(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	ref := sessionRef(r)
	now := s.clock.Now()
	tip := plan.SHA(strings.TrimSpace(r.FormValue("sha")))
	if tip == "" {
		return errorf(http.StatusBadRequest, "sha is required to raise")
	}
	ticket, snap, err := s.view.SessionSnapshot(ctx, now, ref)
	if err != nil {
		return fmt.Errorf("resolve ticket: %w", err)
	}
	if err := requireOffer(snap, ticket, plan.VerbRaise); err != nil {
		return err
	}
	id, err := s.store.Raise(ctx, ticket, tip, now)
	if err != nil {
		return fmt.Errorf("raise: %w", err)
	}
	s.nudge()
	if err := s.awaitConsumed(ctx, id); err != nil {
		return err
	}
	result, err := s.view.RaiseResult(ctx, s.clock.Now(), ref)
	if err != nil {
		return fmt.Errorf("read raise result: %w", err)
	}
	return renderFragment(w, r, ref.Path()+"/changes", "raiseResult", result)
}

func (s *Server) awaitConsumed(ctx context.Context, id int64) error {
	ctx, cancel := context.WithTimeout(ctx, 3*store.TickPeriod)
	defer cancel()
	tick := time.NewTicker(raisePollEvery)
	defer tick.Stop()
	for {
		consumed, err := s.store.IntentConsumed(ctx, id)
		switch {
		case err != nil && ctx.Err() != nil:
			return errRaiseTimeout
		case err != nil:
			return fmt.Errorf("poll raise intent: %w", err)
		case consumed:
			return nil
		}
		select {
		case <-ctx.Done():
			return errRaiseTimeout
		case <-tick.C:
		}
	}
}

var errRaiseTimeout = errorf(http.StatusGatewayTimeout, "the loop has not run the raise yet; reload to check")

func (s *Server) renderThread(
	w http.ResponseWriter, r *http.Request, ticket plan.TicketURL, t view.ReviewThread, name string,
) error {
	comments, err := s.store.ReviewComments(r.Context(), ticket)
	if err != nil {
		return fmt.Errorf("count comments: %w", err)
	}
	t.Pending = len(comments)
	return renderHTML(w, name, t)
}
