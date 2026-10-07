package auth_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/auth"
)

const (
	password         = "correct horse battery staple"
	passwordAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
)

var encodedPattern = regexp.MustCompile(`^pbkdf2-sha256\$600000\$[0-9a-f]{32}\$[0-9a-f]{64}$`)

func TestHashPasswordEncodesAndSaltsEachCall(t *testing.T) {
	t.Parallel()

	first, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	second, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !encodedPattern.MatchString(first) {
		t.Errorf("HashPassword() = %q, want to match %s", first, encodedPattern)
	}
	if first == second {
		t.Errorf("HashPassword twice with one password gave identical output %q", first)
	}
	if strings.Contains(first, password) {
		t.Errorf("HashPassword() = %q, contains the password", first)
	}
}

func TestVerifyPassword(t *testing.T) {
	t.Parallel()

	encoded, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	tests := map[string]struct {
		password, encoded string
		want              bool
	}{
		"right password":         {password, encoded, true},
		"wrong password":         {"wrong password entirely", encoded, false},
		"empty":                  {"anything", "", false},
		"wrong scheme":           {"anything", "bcrypt$10$abcd$abcd", false},
		"too few fields":         {"anything", "pbkdf2-sha256$600000$abcd", false},
		"non-numeric iterations": {"anything", "pbkdf2-sha256$many$abcd$abcd", false},
		"non-hex salt":           {"anything", "pbkdf2-sha256$600000$zzzz$abcd", false},
		"non-hex key":            {"anything", "pbkdf2-sha256$600000$abcd$zzzz", false},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := auth.VerifyPassword(tt.password, tt.encoded); got != tt.want {
				t.Errorf("VerifyPassword(%q, %q) = %t, want %t", tt.password, tt.encoded, got, tt.want)
			}
		})
	}
}

func TestGeneratePasswordIs24CharsFromTheAlphabet(t *testing.T) {
	t.Parallel()

	generated, err := auth.GeneratePassword()
	if err != nil {
		t.Fatalf("GeneratePassword: %v", err)
	}
	if len(generated) != 24 {
		t.Errorf("len(GeneratePassword()) = %d, want 24", len(generated))
	}
	for _, c := range generated {
		if !strings.ContainsRune(passwordAlphabet, c) {
			t.Errorf("GeneratePassword() = %q, contains %q outside the alphabet", generated, c)
		}
	}
}

func TestSessionTokens(t *testing.T) {
	t.Parallel()

	first, err := auth.NewSessionToken()
	if err != nil {
		t.Fatalf("NewSessionToken: %v", err)
	}
	second, err := auth.NewSessionToken()
	if err != nil {
		t.Fatalf("NewSessionToken: %v", err)
	}
	if first == "" || first == second {
		t.Errorf("NewSessionToken twice = %q, %q, want two distinct non-empty tokens", first, second)
	}
	hash := auth.HashToken(first)
	if hash != auth.HashToken(first) {
		t.Errorf("HashToken(%q) is not deterministic", first)
	}
	if hash == first || hash == auth.HashToken(second) {
		t.Errorf("HashToken(%q) = %q, want a hash distinct from the token and from another token's hash", first, hash)
	}
}
