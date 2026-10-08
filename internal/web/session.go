package web

import (
	"net/http"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/auth"
)

const (
	sessionCookie = "cc_session"
	sessionTTL    = 30 * 24 * time.Hour

	dummyPasswordHash = "pbkdf2-sha256$600000$b1587cc8331a8b2b3d56d9ae2cb8fad2$" +
		"2eeb8c3e2f342ef738806c830c0da915ef5e76c60efa82289e50eb5a27c0e56a"
)

type loginView struct{ Failed bool }

func (s *Server) handleLoginPage(w http.ResponseWriter, _ *http.Request) error {
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
	encoded := user.PasswordHash
	if err != nil {
		encoded = dummyPasswordHash
	}
	ok := s.verifyPassword(password, encoded)
	if err != nil || !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return renderHTML(w, "login.tmpl", loginView{Failed: true})
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
