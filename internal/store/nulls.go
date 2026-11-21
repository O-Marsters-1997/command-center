package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
)

func notNull(s string) sql.NullString {
	return sql.NullString{String: s, Valid: true}
}

func notNullTime(t time.Time) sql.NullTime {
	return sql.NullTime{Time: t, Valid: true}
}

func nullIfEmpty(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

func nullTime(t time.Time) sql.NullTime {
	return sql.NullTime{Time: t.UTC(), Valid: !t.IsZero()}
}

func nullInt(o option.Option[int]) sql.NullInt64 {
	return sql.NullInt64{Int64: int64(o.UnwrapOr(0)), Valid: o.IsSome()}
}

func nullFloat(o option.Option[float64]) sql.NullFloat64 {
	return sql.NullFloat64{Float64: o.UnwrapOr(0), Valid: o.IsSome()}
}

func nullString(o option.Option[string]) sql.NullString {
	return sql.NullString{String: o.UnwrapOr(""), Valid: o.IsSome()}
}

func optInt(n sql.NullInt64) option.Option[int] {
	if !n.Valid {
		return option.None[int]()
	}
	return option.Some(int(n.Int64))
}

func optFloat(n sql.NullFloat64) option.Option[float64] {
	if !n.Valid {
		return option.None[float64]()
	}
	return option.Some(n.Float64)
}

func optTime(t sql.NullTime) option.Option[time.Time] {
	if !t.Valid {
		return option.None[time.Time]()
	}
	return option.Some(t.Time)
}

func parseOutcome(s sql.NullString) (option.Option[plan.Outcome], error) {
	if !s.Valid {
		return option.None[plan.Outcome](), nil
	}
	outcome, err := plan.ParseOutcome(s.String)
	if err != nil {
		return option.None[plan.Outcome](), fmt.Errorf("runs.outcome: %w", err)
	}
	return option.Some(outcome), nil
}
