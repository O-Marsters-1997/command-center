// Package hx names the htmx headers and decides whether a request is an htmx one.
package hx

import "net/http"

const (
	HeaderRequest        = "HX-Request"
	HeaderCurrentURL     = "HX-Current-URL"
	HeaderHistoryRestore = "HX-History-Restore-Request"
	HeaderLocation       = "HX-Location"
	HeaderRefresh        = "HX-Refresh"
	HeaderRetarget       = "HX-Retarget"
	HeaderReswap         = "HX-Reswap"
	HeaderReselect       = "HX-Reselect"
)

// IsRequest reports whether htmx sent r for a partial swap. A history-restore request wants
// the full page, so it reports false.
func IsRequest(r *http.Request) bool {
	return r.Header.Get(HeaderRequest) == "true" && r.Header.Get(HeaderHistoryRestore) != "true"
}

func CurrentURL(r *http.Request) string {
	return r.Header.Get(HeaderCurrentURL)
}
