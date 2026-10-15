package web

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/web/hx"
)

func seeOther(w http.ResponseWriter, r *http.Request, to string) {
	if hx.IsRequest(r) {
		w.Header().Set(hx.HeaderLocation, to)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}

func renderFragment(w http.ResponseWriter, r *http.Request, host, block string, data any) error {
	w.Header().Add("Vary", hx.HeaderRequest)
	if !hx.IsRequest(r) {
		seeOther(w, r, host)
		return nil
	}
	return renderHTML(w, block, data)
}

func returnPath(back, fallback string) string {
	u, err := url.Parse(back)
	if err != nil || u.Scheme != "" || u.Host != "" || !strings.HasPrefix(back, "/") ||
		strings.HasPrefix(back, "//") || strings.Contains(back, `\`) {
		return fallback
	}
	for seg := range strings.SplitSeq(u.Path, "/") {
		if seg == "." || seg == ".." {
			return fallback
		}
	}
	return back
}
