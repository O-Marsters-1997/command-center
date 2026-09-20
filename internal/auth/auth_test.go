package auth_test

import (
	"encoding/hex"
	"regexp"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/auth"
)

var encodedPattern = regexp.MustCompile(`^pbkdf2-sha256\$600000\$[0-9a-f]{32}\$[0-9a-f]{64}$`)

func TestHashPasswordEncodesAsPBKDF2SHA256(t *testing.T) {
	t.Parallel()

	encoded, err := auth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !encodedPattern.MatchString(encoded) {
		t.Errorf("HashPassword() = %q, want to match %s", encoded, encodedPattern)
	}
}

func TestHashPasswordNeverContainsThePassword(t *testing.T) {
	t.Parallel()

	password := "correct horse battery staple"
	encoded, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if strings.Contains(encoded, password) {
		t.Errorf("HashPassword(%q) = %q, contains the password", password, encoded)
	}
	if strings.Contains(encoded, hex.EncodeToString([]byte(password))) {
		t.Errorf("HashPassword(%q) = %q, contains the hex-encoded password", password, encoded)
	}
}

func TestHashPasswordSaltsEachCallDifferently(t *testing.T) {
	t.Parallel()

	first, err := auth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	second, err := auth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if first == second {
		t.Errorf("HashPassword called twice with the same password produced identical output: %q", first)
	}
}

func TestVerifyPasswordRoundTripsTheRightPassword(t *testing.T) {
	t.Parallel()

	encoded, err := auth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !auth.VerifyPassword("correct horse battery staple", encoded) {
		t.Error("VerifyPassword() = false, want true for the password that was hashed")
	}
}

func TestVerifyPasswordRejectsTheWrongPassword(t *testing.T) {
	t.Parallel()

	encoded, err := auth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if auth.VerifyPassword("wrong password entirely", encoded) {
		t.Error("VerifyPassword() = true, want false for a password that was never hashed")
	}
}

const passwordAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

func TestGeneratePasswordIs24CharsFromTheAlphabet(t *testing.T) {
	t.Parallel()

	password, err := auth.GeneratePassword()
	if err != nil {
		t.Fatalf("GeneratePassword: %v", err)
	}
	if len(password) != 24 {
		t.Errorf("len(GeneratePassword()) = %d, want 24", len(password))
	}
	for _, c := range password {
		if !strings.ContainsRune(passwordAlphabet, c) {
			t.Errorf("GeneratePassword() = %q, contains %q outside the alphabet", password, c)
		}
	}
}

func TestGeneratePasswordDrawsWithoutDetectableBias(t *testing.T) {
	t.Parallel()

	const draws = 10_000
	counts := make(map[rune]int)
	for range draws {
		password, err := auth.GeneratePassword()
		if err != nil {
			t.Fatalf("GeneratePassword: %v", err)
		}
		for _, c := range password {
			if !strings.ContainsRune(passwordAlphabet, c) {
				t.Fatalf("GeneratePassword() = %q, contains %q outside the alphabet", password, c)
			}
			counts[c]++
		}
	}

	total := draws * 24
	expected := float64(total) / float64(len(passwordAlphabet))
	var chiSquared float64
	for _, c := range passwordAlphabet {
		diff := float64(counts[c]) - expected
		chiSquared += diff * diff / expected
	}
	// Critical value for a chi-squared goodness-of-fit test with 61 degrees of freedom
	// (62 alphabet symbols) is ~99.6 at p=0.001. 200 stays far above chance noise while still
	// catching a real bias such as modulo bias from naive rejection-free sampling.
	const chiSquaredCeiling = 200.0
	if chiSquared > chiSquaredCeiling {
		t.Errorf("chi-squared = %.1f over %d draws, want <= %.1f (no detectable bias)", chiSquared, draws, chiSquaredCeiling)
	}
}

func TestNewSessionTokenIsUniqueEachCall(t *testing.T) {
	t.Parallel()

	first, err := auth.NewSessionToken()
	if err != nil {
		t.Fatalf("NewSessionToken: %v", err)
	}
	second, err := auth.NewSessionToken()
	if err != nil {
		t.Fatalf("NewSessionToken: %v", err)
	}
	if first == second {
		t.Errorf("NewSessionToken called twice produced identical output: %q", first)
	}
	if first == "" {
		t.Error("NewSessionToken() = \"\", want a non-empty token")
	}
}

func TestHashTokenIsDeterministicAndDoesNotReturnTheToken(t *testing.T) {
	t.Parallel()

	token, err := auth.NewSessionToken()
	if err != nil {
		t.Fatalf("NewSessionToken: %v", err)
	}
	first := auth.HashToken(token)
	second := auth.HashToken(token)
	if first != second {
		t.Errorf("HashToken(%q) is not deterministic: %q != %q", token, first, second)
	}
	if first == token {
		t.Errorf("HashToken(%q) = %q, want a hash distinct from the raw token", token, first)
	}
}

func TestHashTokenDistinguishesDifferentTokens(t *testing.T) {
	t.Parallel()

	first, err := auth.NewSessionToken()
	if err != nil {
		t.Fatalf("NewSessionToken: %v", err)
	}
	second, err := auth.NewSessionToken()
	if err != nil {
		t.Fatalf("NewSessionToken: %v", err)
	}
	if auth.HashToken(first) == auth.HashToken(second) {
		t.Errorf("HashToken produced the same hash for two different tokens")
	}
}

func TestVerifyPasswordRejectsMalformedEncodingWithoutPanicking(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"empty":                  "",
		"wrong scheme":           "bcrypt$10$abcd$abcd",
		"too few fields":         "pbkdf2-sha256$600000$abcd",
		"non-numeric iterations": "pbkdf2-sha256$many$abcd$abcd",
		"non-hex salt":           "pbkdf2-sha256$600000$zzzz$abcd",
		"non-hex key":            "pbkdf2-sha256$600000$abcd$zzzz",
	}
	for name, encoded := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if auth.VerifyPassword("anything", encoded) {
				t.Errorf("VerifyPassword(_, %q) = true, want false", encoded)
			}
		})
	}
}
