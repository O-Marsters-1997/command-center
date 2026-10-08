package web_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/auth"
	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web"
)

const loginPassword = "correct horse"

func seedUser(t *testing.T, st *store.Store) {
	t.Helper()
	hash, err := auth.HashPassword(loginPassword)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateUser(t.Context(), "me@example.com", hash, testNow); err != nil {
		t.Fatal(err)
	}
}

func postLogin(t *testing.T, server *web.Server, email, password string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"email": {email}, "password": {password}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	return rec
}

func sessionCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == "cc_session" {
			return c
		}
	}
	t.Fatalf("no cc_session cookie; status %d", rec.Code)
	return nil
}

func TestLoginPageGolden(t *testing.T) {
	t.Parallel()

	rec := get(t, newServer(openStore(t), testNow), "/login")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	assertGolden(t, "testdata/login.golden.html", rec.Body.Bytes())
}

func TestLoginSetsSessionCookieStoredOnlyAsHash(t *testing.T) {
	t.Parallel()

	st := openStore(t)
	seedUser(t, st)
	rec := postLogin(t, newServer(st, testNow), "  Me@Example.com ", loginPassword)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Fatalf("got %d to %q, want 303 to /", rec.Code, rec.Header().Get("Location"))
	}
	c := sessionCookie(t, rec)
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" {
		t.Errorf("cookie attributes wrong: %+v", c)
	}
	if _, err := st.SessionOwner(t.Context(), auth.HashToken(c.Value), testNow); err != nil {
		t.Errorf("no session for the hash of the cookie: %v", err)
	}
	if _, err := st.SessionOwner(t.Context(), c.Value, testNow); err == nil {
		t.Error("raw token is stored as a session key")
	}
	if _, err := st.SessionOwner(t.Context(), auth.HashToken(c.Value), testNow.Add(31*24*time.Hour)); err == nil {
		t.Error("session still valid after 30 days")
	}
}

func TestSecondLoginDeletesFirstSession(t *testing.T) {
	t.Parallel()

	st := openStore(t)
	seedUser(t, st)
	server := newServer(st, testNow)
	first := sessionCookie(t, postLogin(t, server, "me@example.com", loginPassword))
	second := sessionCookie(t, postLogin(t, server, "me@example.com", loginPassword))

	if _, err := st.SessionOwner(t.Context(), auth.HashToken(first.Value), testNow); err == nil {
		t.Error("first session survived a second login")
	}
	if _, err := st.SessionOwner(t.Context(), auth.HashToken(second.Value), testNow); err != nil {
		t.Errorf("second session missing: %v", err)
	}
}

func TestWrongPasswordAndUnknownEmailAreIdenticalAndBothRunTheKDF(t *testing.T) {
	t.Parallel()

	st := openStore(t)
	seedUser(t, st)
	server := newServer(st, testNow)
	var verified int
	server.SetVerifyPassword(func(password, encoded string) bool {
		verified++
		return auth.VerifyPassword(password, encoded)
	})

	wrong := postLogin(t, server, "me@example.com", "nope")
	unknown := postLogin(t, server, "nobody@example.com", "nope")

	if verified != 2 {
		t.Errorf("KDF ran %d times for two failed logins, want 2", verified)
	}
	if wrong.Code != http.StatusUnauthorized || wrong.Code != unknown.Code {
		t.Errorf("statuses %d and %d, want both 401", wrong.Code, unknown.Code)
	}
	if wrong.Body.String() != unknown.Body.String() {
		t.Errorf("bodies differ:\n%s\n---\n%s", wrong.Body, unknown.Body)
	}
	if len(wrong.Result().Cookies()) != 0 || len(unknown.Result().Cookies()) != 0 {
		t.Error("a failed login set a cookie")
	}
}
