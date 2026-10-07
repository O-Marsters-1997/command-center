package view

import (
	"context"
	"strings"
	"time"
)

type FeatureRow struct {
	Feature  string
	Imported bool
}

type ImportError struct {
	Age     string
	Feature string
	Message string
}

type Features struct {
	Chrome
	Features        []FeatureRow
	Query           string
	LastImportError *ImportError
}

// Features lists offered, the features the trackers offer, narrowed by the case-insensitive query
// and marked imported where a ticket of that feature is already stored.
func (r *Reader) Features(ctx context.Context, now time.Time, offered []string, query string) (Features, error) {
	tickets, err := r.store.Tickets(ctx)
	if err != nil {
		return Features{}, err
	}
	lastErr, failed, err := r.store.LastImportError(ctx)
	if err != nil {
		return Features{}, err
	}
	imported := make(map[string]bool)
	for _, f := range distinctFeatures(tickets) {
		imported[f] = true
	}

	q := strings.ToLower(query)
	rows := make([]FeatureRow, 0, len(offered))
	for _, f := range offered {
		if q != "" && !strings.Contains(strings.ToLower(f), q) {
			continue
		}
		rows = append(rows, FeatureRow{Feature: f, Imported: imported[f]})
	}

	chrome, err := r.Chrome(ctx, now, ParseParams(nil))
	if err != nil {
		return Features{}, err
	}
	chrome.Section = "features"

	page := Features{Chrome: chrome, Features: rows, Query: query}
	if failed {
		page.LastImportError = &ImportError{
			Age: relative(now, lastErr.At).Age, Feature: lastErr.Feature, Message: lastErr.Message,
		}
	}
	return page, nil
}
