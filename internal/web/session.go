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

type loginView struct{ Failed bool }

func (s *Server) handleLoginPage(w http.ResponseWriter, _ *http.Request) error {
	w.Header().Set("Cache-Control", "no-store")
	return renderHTML(w, "login.tmpl", loginView{})
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
	encoded := user.PasswordHash
	if missing {
		encoded = dummyPasswordHash()
	}
	ok := s.verifyPassword(password, encoded)
	if missing || !ok {
		w.Header().Set("Cache-Control", "no-store")
		return renderHTMLStatus(w, http.StatusUnauthorized, "login.tmpl", loginView{Failed: true})
	}

	token, err := auth.NewSessionToken()
	if err != nil {
		return err
	}
	now := s.clock.Now()
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
