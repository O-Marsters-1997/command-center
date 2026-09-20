package cc_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/auth"
	"github.com/O-Marsters-1997/command-center/internal/cc"
)

func createLoginUser(t *testing.T, store *cc.Store, email, password string) {
	t.Helper()

	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if err := store.CreateUser(t.Context(), email, hash, time.Now()); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
}

func postLogin(t *testing.T, srv *httptest.Server, email, password string) *http.Response {
	t.Helper()

	form := strings.NewReader("email=" + email + "&password=" + password)
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/login", form)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", srv.URL)
	resp, err := noRedirect(srv).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func loginAs(t *testing.T, srv *httptest.Server, email, password string) *http.Cookie {
	t.Helper()

	resp := postLogin(t, srv, email, password)
	defer func() { _ = resp.Body.Close() }()
	assertSeeOtherHome(t, resp)
	for _, c := range resp.Cookies() {
		if c.Name == "cc_session" {
			return c
		}
	}
	t.Fatal("no cc_session cookie set")
	return nil
}

func TestGetLoginRendersEmailAndPasswordFieldsWithNoChrome(t *testing.T) {
	t.Parallel()

	server := cc.NewServer(openStore(t), time.Now, nil, "")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/login", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, want := range []string{`type="email" name="email"`, `type="password" name="password"`} {
		if !strings.Contains(body, want) {
			t.Errorf("login page does not contain %q:\n%s", want, body)
		}
	}
	for _, unwanted := range []string{`id="sidebar-nav"`, `<header`, `id="masthead"`} {
		if strings.Contains(body, unwanted) {
			t.Errorf("login page contains %q, want no chrome:\n%s", unwanted, body)
		}
	}
}

func TestPostLoginWithCorrectPasswordSetsCookieAndRedirects(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	createLoginUser(t, store, "olly@example.com", "correct horse battery staple")
	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, ""))
	t.Cleanup(srv.Close)

	cookie := loginAs(t, srv, "olly%40example.com", "correct+horse+battery+staple")
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" {
		t.Errorf("cc_session cookie = %+v, want HttpOnly, Secure, SameSite=Lax, Path=/", cookie)
	}

	row, err := store.UserForLogin(t.Context(), "olly@example.com")
	if err != nil {
		t.Fatalf("UserForLogin: %v", err)
	}
	owner, err := store.SessionOwner(t.Context(), auth.HashToken(cookie.Value), time.Now())
	if err != nil {
		t.Fatalf("SessionOwner(hash of cookie): %v", err)
	}
	if owner != row.ID {
		t.Errorf("SessionOwner(hash of cookie) = %d, want %d", owner, row.ID)
	}
	if _, err := store.SessionOwner(t.Context(), cookie.Value, time.Now()); err == nil {
		t.Error("SessionOwner(raw cookie value) = nil error, want a failure: the raw token must not be what is stored")
	}
}

func TestLoggingInTwiceDeletesTheFirstSession(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	createLoginUser(t, store, "olly@example.com", "correct horse battery staple")
	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, ""))
	t.Cleanup(srv.Close)

	first := loginAs(t, srv, "olly%40example.com", "correct+horse+battery+staple")
	second := loginAs(t, srv, "olly%40example.com", "correct+horse+battery+staple")
	if first.Value == second.Value {
		t.Fatal("two logins minted the same token")
	}

	if _, err := store.SessionOwner(t.Context(), auth.HashToken(first.Value), time.Now()); err == nil {
		t.Error("SessionOwner(first token) = nil error after a second login, want a failure")
	}
	row, err := store.UserForLogin(t.Context(), "olly@example.com")
	if err != nil {
		t.Fatalf("UserForLogin: %v", err)
	}
	owner, err := store.SessionOwner(t.Context(), auth.HashToken(second.Value), time.Now())
	if err != nil || owner != row.ID {
		t.Errorf("SessionOwner(second token) = %d, %v, want %d, nil", owner, err, row.ID)
	}
}

func TestWrongPasswordAndUnknownEmailProduceByteIdenticalResponses(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	createLoginUser(t, store, "olly@example.com", "correct horse battery staple")
	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, ""))
	t.Cleanup(srv.Close)

	wrongPassword := postLogin(t, srv, "olly%40example.com", "not+the+password")
	defer func() { _ = wrongPassword.Body.Close() }()
	unknownEmail := postLogin(t, srv, "nobody%40example.com", "not+the+password")
	defer func() { _ = unknownEmail.Body.Close() }()

	if wrongPassword.StatusCode != unknownEmail.StatusCode {
		t.Fatalf("status = %d vs %d, want equal", wrongPassword.StatusCode, unknownEmail.StatusCode)
	}
	wrongBody := readAll(t, wrongPassword)
	unknownBody := readAll(t, unknownEmail)
	if wrongBody != unknownBody {
		t.Errorf("wrong password body:\n%s\nunknown email body:\n%s\nwant identical", wrongBody, unknownBody)
	}
}

func TestUnknownEmailStillRunsTheKDF(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(cc.NewServer(openStore(t), time.Now, nil, ""))
	t.Cleanup(srv.Close)

	start := time.Now()
	resp := postLogin(t, srv, "nobody%40example.com", "whatever")
	defer func() { _ = resp.Body.Close() }()
	elapsed := time.Since(start)

	const kdfFloor = 20 * time.Millisecond
	if elapsed < kdfFloor {
		t.Errorf("unknown-email login answered in %s, want >= %s: the dummy hash was not verified against", elapsed, kdfFloor)
	}
}

func TestLoginEmailIsCaseInsensitiveAndTrimmed(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	createLoginUser(t, store, "olly@example.com", "correct horse battery staple")
	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, ""))
	t.Cleanup(srv.Close)

	loginAs(t, srv, "+OLLY%40EXAMPLE.COM+", "correct+horse+battery+staple")
}

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
