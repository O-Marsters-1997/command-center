package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Compose renders the prompt a launch authorises: the implement instruction plus each seam
// file's content as its own line, in config order.
func Compose(t Task, seams []string) string {
	lines := append([]string{"/implement " + t.TicketURL}, seams...)
	return strings.Join(lines, "\n")
}

// Hash fingerprints a composed prompt. Consent is bound to content (docs/command-centre-
// v1.md § 4b): a launch stores this at authorisation and the tick recomputes it at spawn
// time, refusing on mismatch.
func Hash(composed string) string {
	sum := sha256.Sum256([]byte(composed))
	return hex.EncodeToString(sum[:])
}
