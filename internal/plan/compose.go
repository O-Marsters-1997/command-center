package plan

import (
	"crypto/sha256"
	"encoding/hex"
)

// Compose renders the prompt a launch authorises: the implement instruction for the ticket.
func Compose(t Ticket) string {
	return "/implement " + t.URL
}

// Hash fingerprints a composed prompt. Consent is bound to content (docs/command-centre-
// v1.md § 4b): a launch stores this at authorisation and the tick recomputes it at spawn
// time, refusing on mismatch.
func Hash(composed string) string {
	sum := sha256.Sum256([]byte(composed))
	return hex.EncodeToString(sum[:])
}
