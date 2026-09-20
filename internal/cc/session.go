package cc

import (
	_ "embed"
	"html/template"
	"net/http"
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/auth"
)

//go:embed login.tmpl
var loginSource string

var loginPage = template.Must(page.New("login").Parse(loginSource))

const sessionCookieName = "cc_session"

const dummyPasswordHash = "pbkdf2-sha256$600000$49fb85de4f63cad5bf8297c9ea8a7e94" +
	"$f4f8ca647b7d37a43231bbb11b4af7916a2f861f27bd7cf8658abf38d88a0df9"

type loginView struct {
	Error string
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := loginPage.Execute(w, loginView{}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handlePostLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	email := strings.ToLower(strings.TrimSpace(r.PostForm.Get("email")))
	password := r.PostForm.Get("password")

	user, err := s.store.UserForLogin(r.Context(), email)
	if err != nil {
		auth.VerifyPassword(password, dummyPasswordHash)
		s.renderLoginFailure(w)
		return
	}
	if !auth.VerifyPassword(password, user.PasswordHash) {
		s.renderLoginFailure(w)
		return
	}

	ctx := r.Context()
	if err := s.store.DeleteSession(ctx, user.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	token, err := auth.NewSessionToken()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.store.IssueSession(ctx, user.ID, auth.HashToken(token), s.now()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) renderLoginFailure(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_ = loginPage.Execute(w, loginView{Error: "incorrect email or password"})
}
