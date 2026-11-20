package web

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	"github.com/O-Marsters-1997/command-center/internal/web/hx"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) error {
	ref := sessionRef(r)
	query := r.URL.Query()
	raw := option.None[view.RawFilter]()
	if query.Get("log") == "raw" {
		raw = option.Some(view.RawFilter{Type: query.Get("kind"), Query: query.Get("q")})
	}
	page, err := s.view.Session(r.Context(), s.clock.Now(), ref, raw)
	if err != nil {
		return fmt.Errorf("load session: %w", err)
	}
	return renderHTML(w, "session.tmpl", page)
}

func (s *Server) handleSessionActions(w http.ResponseWriter, r *http.Request) error {
	ref := sessionRef(r)
	page, err := s.view.Session(r.Context(), s.clock.Now(), ref, option.None[view.RawFilter]())
	if err != nil {
		return fmt.Errorf("load session: %w", err)
	}
	page.Fragment = true
	return renderFragment(w, r, page.TranscriptPath, "sessionActions", page)
}

func sessionRef(r *http.Request) view.SessionRef {
	run, _ := strconv.ParseInt(r.URL.Query().Get("run"), 10, 64)
	return view.SessionRef{
		Owner: r.PathValue("owner"), Name: r.PathValue("name"), N: r.PathValue("n"), Run: plan.RunID(run),
	}
}

func (s *Server) afterSessionVerb(
	w http.ResponseWriter, r *http.Request, ticketURL string, verb plan.Verb, toast string, undoID int64,
) error {
	ref, err := view.ParseSessionRef(plan.TicketURL(ticketURL))
	if err != nil {
		return errorf(http.StatusBadRequest, "%q has no session page", ticketURL)
	}
	if !hx.IsRequest(r) {
		seeOther(w, r, ref.Path())
		return nil
	}
	page, err := s.view.Session(r.Context(), s.clock.Now(), ref, option.None[view.RawFilter]())
	if err != nil {
		return fmt.Errorf("load session: %w", err)
	}
	page.Toast = toast
	page.UndoID, page.UndoVerb = undoID, verb
	page.ResetComposer = undoID != 0 && verb == plan.VerbFollowUp
	page.Fragment = true
	return renderHTML(w, "sessionActions", page)
}
