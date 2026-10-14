package gh

import "testing"

func FuzzDecode(f *testing.F) {
	f.Fuzz(func(t *testing.T, raw []byte) {
		_, _ = decode(raw)
	})
}
