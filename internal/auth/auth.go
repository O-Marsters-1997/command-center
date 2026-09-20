// Package auth mints and verifies passwords, session tokens and their stored hashes.
package auth

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

const (
	pbkdf2Iterations = 600_000
	saltLength       = 16
	keyLength        = 32

	passwordAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	passwordLength   = 24

	sessionTokenLength = 32
)

// HashPassword derives a PBKDF2-HMAC-SHA256 key from password under a fresh random salt and
// encodes both as pbkdf2-sha256$<iterations>$<hex salt>$<hex key>.
func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, pbkdf2Iterations, keyLength)
	if err != nil {
		return "", fmt.Errorf("derive key: %w", err)
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", pbkdf2Iterations, hex.EncodeToString(salt), hex.EncodeToString(key)), nil
}

// VerifyPassword reports whether password hashes to the same key as encoded, which must be in
// the format HashPassword produces. A malformed encoded string is a mismatch, never a panic.
func VerifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations <= 0 {
		return false
	}
	salt, err := hex.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := hex.DecodeString(parts[3])
	if err != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iterations, len(want))
	if err != nil {
		return false
	}
	return hmac.Equal(got, want)
}

// GeneratePassword returns a 24-character password drawn uniformly from a 62-character
// alphanumeric alphabet.
func GeneratePassword() (string, error) {
	password := make([]byte, passwordLength)
	for i := range password {
		c, err := randomAlphabetByte()
		if err != nil {
			return "", err
		}
		password[i] = c
	}
	return string(password), nil
}

func randomAlphabetByte() (byte, error) {
	limit := 256 - 256%len(passwordAlphabet)
	buf := make([]byte, 1)
	for {
		if _, err := rand.Read(buf); err != nil {
			return 0, fmt.Errorf("generate password byte: %w", err)
		}
		if int(buf[0]) < limit {
			return passwordAlphabet[int(buf[0])%len(passwordAlphabet)], nil
		}
	}
}

// NewSessionToken returns a fresh random session token, hex-encoded.
func NewSessionToken() (string, error) {
	buf := make([]byte, sessionTokenLength)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// HashToken returns the hex-encoded SHA-256 hash of token, for storage.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
