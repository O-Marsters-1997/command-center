package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func commentTarget(r *http.Request) (path string, side plan.Side, line int, err error) {
	path = r.FormValue("path")
	side = plan.Side(r.FormValue("side"))
	line, _ = strconv.Atoi(r.FormValue("line"))
	if path == "" || line < 1 || (side != plan.SideOld && side != plan.SideNew) {
		return "", "", 0, errorf(http.StatusBadRequest,
			"comment target %q %q %q is not a diff line", path, side, r.FormValue("line"))
	}
	return path, side, line, nil
}

func (s *Server) handleCommentForm(w http.ResponseWriter, r *http.Request) error {
	path, side, line, err := commentTarget(r)
	if err != nil {
		return err
	}
	ref := sessionRef(r)
	return renderHTML(w, "reviewForm", view.ReviewForm{SessionPath: ref.Path(), Path: path, Side: side, Line: line})
}

func (s *Server) handleAddComment(w http.ResponseWriter, r *http.Request) error {
	path, side, line, err := commentTarget(r)
	if err != nil {
		return err
	}
	body := strings.TrimSpace(r.FormValue("body"))
	if body == "" {
		return errorf(http.StatusBadRequest, "comment body is empty")
	}
	ref := sessionRef(r)
	now := s.clock.Now()
	anchor, err := s.view.CommentAnchor(r.Context(), now, ref, s.dataDir, path, side, line)
	if err != nil {
		return fmt.Errorf("anchor comment: %w", err)
	}
	c := plan.ReviewComment{Tip: anchor.Tip, Path: path, Line: line, Side: side, Hunk: anchor.Hunk, Body: body}
	c.ID, err = s.store.AddReviewComment(r.Context(), store.NewReviewComment{
		Ticket: anchor.Ticket, ReviewComment: c, At: now,
	})
	if err != nil {
		return fmt.Errorf("add comment: %w", err)
	}
	return s.renderThread(w, r, anchor.Ticket, view.ReviewThread{SessionPath: ref.Path(), Comment: c}, "reviewPosted")
}

func (s *Server) handleDeleteComment(w http.ResponseWriter, r *http.Request) error {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return errorf(http.StatusBadRequest, "comment id %q is not a number", r.PathValue("id"))
	}
	ticket, err := s.view.SessionTicket(r.Context(), s.clock.Now(), sessionRef(r))
	if err != nil {
		return fmt.Errorf("resolve ticket: %w", err)
	}
	if err := s.store.DeleteReviewComment(r.Context(), ticket, id); err != nil {
		return fmt.Errorf("delete comment: %w", err)
	}
	return s.renderThread(w, r, ticket, view.ReviewThread{}, "reviewDeleted")
}

func (s *Server) handleRequestChanges(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	ref := sessionRef(r)
	now := s.clock.Now()
	ticket, err := s.view.SessionTicket(ctx, now, ref)
	if err != nil {
		return fmt.Errorf("resolve ticket: %w", err)
	}
	snap, err := s.view.Snapshot(ctx, now)
	if err != nil {
		return fmt.Errorf("load snapshot: %w", err)
	}
	if !snap.Offers(ticket, plan.VerbFollowUp) {
		entry, _ := snap.Entry(ticket)
		return errorf(http.StatusConflict, "%s is %s and does not offer %s: %s",
			ticket, entry.State, plan.VerbFollowUp, entry.Reason)
	}
	comments, err := s.store.ReviewComments(ctx, ticket)
	if err != nil {
		return fmt.Errorf("load comments: %w", err)
	}
	note := strings.TrimSpace(r.FormValue("note"))
	if note == "" && len(comments) == 0 {
		return errorf(http.StatusBadRequest, "add a comment or a note before requesting changes")
	}
	payload := store.FollowUpPayload{Message: plan.ComposeReview(note, comments)}
	if err := s.store.RequestChanges(ctx, ticket, payload, now); err != nil {
		return fmt.Errorf("request changes: %w", err)
	}
	s.nudge()
	seeOther(w, r, ref.Path())
	return nil
}

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
