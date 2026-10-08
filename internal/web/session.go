package web

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/auth"
)

const (
	sessionCookie = "cc_session"
	sessionTTL    = 30 * 24 * time.Hour
)

var dummyPasswordHash = sync.OnceValue(func() string {
	hash, err := auth.HashPassword("dummy")
	if err != nil {
		panic(err)
	}
	return hash
})

func publicRoute(r *http.Request) bool {
	read := r.Method == http.MethodGet || r.Method == http.MethodHead
	if r.URL.Path == "/login" {
		return read || r.Method == http.MethodPost
	}
	return read && strings.HasPrefix(r.URL.Path, "/assets/")
}

func (s *Server) hasSession(r *http.Request) (bool, error) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return false, nil
	}
	_, err = s.store.SessionOwner(r.Context(), auth.HashToken(c.Value), s.clock.Now())
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// requireSession refuses every request that carries no live session, except the login page and
// the stylesheet it needs.
func (s *Server) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.open || publicRoute(r) {
			next.ServeHTTP(w, r)
			return
		}
		ok, err := s.hasSession(r)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if ok {
			next.ServeHTTP(w, r)
			return
		}
		switch {
		case r.Header.Get("HX-Request") == "true":
			w.Header().Set("HX-Redirect", "/login")
		case r.Method == http.MethodGet && strings.Contains(r.Header.Get("Accept"), "text/html"):
			http.Redirect(w, r, "/login", http.StatusSeeOther)
		default:
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		}
	})
}

type loginView struct{ Failed bool }

func (s *Server) handleLoginPage(w http.ResponseWriter, _ *http.Request) error {
	w.Header().Set("Cache-Control", "no-store")
	return renderHTML(w, "login.tmpl", loginView{})
}

func loginFailed(w http.ResponseWriter) error {
	w.Header().Set("Cache-Control", "no-store")
	return renderHTMLStatus(w, http.StatusUnauthorized, "login.tmpl", loginView{Failed: true})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) error {
	if err := r.ParseForm(); err != nil {
		return withStatus(http.StatusBadRequest, err)
	}
	ctx := r.Context()
	email := strings.ToLower(strings.TrimSpace(r.PostFormValue("email")))
	password := r.PostFormValue("password")

	user, err := s.store.UserForLogin(ctx, email)
	missing := errors.Is(err, sql.ErrNoRows)
	if err != nil && !missing {
		return err
	}
	now := s.clock.Now()
	if !missing {
		admitted, err := s.store.ClaimLoginAttempt(ctx, user.ID, now)
		if err != nil {
			return err
		}
		if !admitted {
			return loginFailed(w)
		}
	}
	encoded := user.PasswordHash
	if missing {
		encoded = dummyPasswordHash()
	}
	ok := s.verifyPassword(password, encoded)
	if missing || !ok {
		return loginFailed(w)
	}

	token, err := auth.NewSessionToken()
	if err != nil {
		return err
	}
	expires := now.Add(sessionTTL)
	if err := s.store.IssueSession(ctx, user.ID, auth.HashToken(token), now, expires); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
	return nil
}
