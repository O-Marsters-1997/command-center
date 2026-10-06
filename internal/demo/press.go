package demo

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
)

// Press posts the board's verb button for the scenario ticket id, as a human clicking it would.
func (s *Sim) Press(_ context.Context, verb, id string) error {
	for _, i := range s.issues {
		if i.ID != id {
			continue
		}
		form := url.Values{"verb": {verb}, "ticket": {i.url}}
		req := httptest.NewRequest(http.MethodPost, "/verb", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		s.server.ServeHTTP(rec, req)
		if rec.Code != http.StatusSeeOther {
			return fmt.Errorf("POST /verb %s %s: %d: %s", verb, id, rec.Code, rec.Body)
		}
		return nil
	}
	return fmt.Errorf("no scenario ticket %q", id)
}
