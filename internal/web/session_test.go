package web_test

import (
	"database/sql"
	"errors"
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
	if _, err := st.SessionOwner(t.Context(), c.Value, testNow); !errors.Is(err, sql.ErrNoRows) {
		t.Error("raw token is stored as a session key")
	}
	expired := testNow.Add(31 * 24 * time.Hour)
	if _, err := st.SessionOwner(t.Context(), auth.HashToken(c.Value), expired); !errors.Is(err, sql.ErrNoRows) {
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

	if _, err := st.SessionOwner(t.Context(), auth.HashToken(first.Value), testNow); !errors.Is(err, sql.ErrNoRows) {
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

func gatedGet(
	t *testing.T, server *web.Server, path string, cookie *http.Cookie, headers map[string]string,
) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	return rec
}

func TestSessionGate(t *testing.T) {
	t.Parallel()

	st := seededStore(t, testNow)
	server := web.NewServer(st, fixedClock(testNow), "")
	live, err := st.SeedSession(t.Context(), "live@example.com", testNow, testNow.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	stale, err := st.SeedSession(t.Context(), "stale@example.com", testNow.Add(-2*time.Hour), testNow.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	html := map[string]string{"Accept": "text/html"}
	htmx := map[string]string{"HX-Request": "true"}
	sse := map[string]string{"Accept": "text/event-stream"}
	session := func(value string) *http.Cookie { return &http.Cookie{Name: "cc_session", Value: value} }

	tests := []struct {
		name     string
		path     string
		cookie   *http.Cookie
		headers  map[string]string
		status   int
		location string
		hxRedir  string
	}{
		{"no cookie redirects a page", "/", nil, html, http.StatusSeeOther, "/login", ""},
		{"valid cookie renders the board", "/", session(live), html, http.StatusOK, "", ""},
		{"htmx poll gets an HX-Redirect", "/board", nil, htmx, http.StatusOK, "", "/login"},
		{"event stream gets 401", "/events", nil, sse, http.StatusUnauthorized, "", ""},
		{"unknown cookie is refused", "/", session("nope"), html, http.StatusSeeOther, "/login", ""},
		{"expired session is refused", "/", session(stale), html, http.StatusSeeOther, "/login", ""},
		{"login page is public", "/login", nil, html, http.StatusOK, "", ""},
		{"stylesheet is public", "/assets/app.css", nil, nil, http.StatusOK, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := gatedGet(t, server, tt.path, tt.cookie, tt.headers)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.status, rec.Body)
			}
			if got := rec.Header().Get("Location"); got != tt.location {
				t.Errorf("Location = %q, want %q", got, tt.location)
			}
			if got := rec.Header().Get("HX-Redirect"); got != tt.hxRedir {
				t.Errorf("HX-Redirect = %q, want %q", got, tt.hxRedir)
			}
			if tt.hxRedir != "" && rec.Body.Len() != 0 {
				t.Errorf("HX-Redirect response has a body: %q", rec.Body)
			}
		})
	}
}

func TestFailuresBackOffTwoToTheNSecondsCappedAtSixty(t *testing.T) {
	t.Parallel()

	st := openStore(t)
	seedUser(t, st)
	now := testNow
	for n, want := range []time.Duration{2, 4, 8, 16, 32, 60, 60} {
		postLogin(t, newServer(st, now), "me@example.com", "nope")
		user, err := st.UserForLogin(t.Context(), "me@example.com")
		if err != nil {
			t.Fatal(err)
		}
		if user.FailedCount != int32(n+1) {
			t.Errorf("failed_count = %d, want %d", user.FailedCount, n+1)
		}
		if got := user.NextAttemptAt.Time.Sub(now); got != want*time.Second {
			t.Errorf("failure %d backs off %v, want %v", n+1, got, want*time.Second)
		}
		now = user.NextAttemptAt.Time
	}
}

func TestAttemptBeforeNextAttemptIsRefusedWithoutTheKDF(t *testing.T) {
	t.Parallel()

	st := openStore(t)
	seedUser(t, st)
	wrong := postLogin(t, newServer(st, testNow), "me@example.com", "nope")

	early := newServer(st, testNow.Add(time.Second))
	early.SetVerifyPassword(func(string, string) bool {
		t.Error("KDF ran for an attempt inside the backoff window")
		return true
	})
	refused := postLogin(t, early, "me@example.com", loginPassword)

	if refused.Code != wrong.Code || refused.Body.String() != wrong.Body.String() {
		t.Errorf("refused response differs from a wrong password:\n%d %s\n---\n%d %s",
			refused.Code, refused.Body, wrong.Code, wrong.Body)
	}
	if len(refused.Result().Cookies()) != 0 {
		t.Error("a refused attempt set a cookie")
	}
	user, err := st.UserForLogin(t.Context(), "me@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if user.FailedCount != 1 {
		t.Errorf("failed_count = %d after a refused attempt, want 1", user.FailedCount)
	}
}

func TestSuccessfulLoginResetsBackoff(t *testing.T) {
	t.Parallel()

	st := openStore(t)
	seedUser(t, st)
	postLogin(t, newServer(st, testNow), "me@example.com", "nope")
	later := testNow.Add(2 * time.Second)
	sessionCookie(t, postLogin(t, newServer(st, later), "me@example.com", loginPassword))

	user, err := st.UserForLogin(t.Context(), "me@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if user.FailedCount != 0 || user.NextAttemptAt.Valid {
		t.Errorf("after success failed_count = %d, next_attempt_at valid = %v", user.FailedCount, user.NextAttemptAt.Valid)
	}
}

func postLogout(server *web.Server, c *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.AddCookie(c)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	return rec
}

func TestLogoutDeletesSessionClearsCookieAndRedirects(t *testing.T) {
	t.Parallel()

	st := openStore(t)
	server := newServer(st, testNow)
	token, err := st.SeedSession(t.Context(), "me@example.com", testNow, testNow.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	rec := postLogout(server, &http.Cookie{Name: "cc_session", Value: token})

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("got %d to %q, want 303 to /login", rec.Code, rec.Header().Get("Location"))
	}
	cleared := sessionCookie(t, rec)
	if cleared.Value != "" || cleared.MaxAge >= 0 {
		t.Errorf("cookie not cleared: %+v", cleared)
	}
	if _, err := st.SessionOwner(t.Context(), auth.HashToken(token), testNow); !errors.Is(err, sql.ErrNoRows) {
		t.Error("session row survived logout")
	}
}

func TestGetLogoutIsNotRouted(t *testing.T) {
	t.Parallel()

	st := openStore(t)
	token, err := st.SeedSession(t.Context(), "me@example.com", testNow, testNow.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	rec := gatedGet(t, newServer(st, testNow), "/logout", &http.Cookie{Name: "cc_session", Value: token}, nil)
	if rec.Code != http.StatusMethodNotAllowed && rec.Code != http.StatusNotFound {
		t.Fatalf("GET /logout status = %d", rec.Code)
	}
}
