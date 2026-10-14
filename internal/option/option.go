// Package option provides Option, a value that may be absent.
package option

import (
	"encoding/json"
	"fmt"
)

// Option is a value that may be absent. It is comparable with == when T is.
type Option[T any] struct {
	value T
	ok    bool
}

// Some wraps a present value.
func Some[T any](v T) Option[T] {
	return Option[T]{value: v, ok: true}
}

// None returns an absent value.
func None[T any]() Option[T] {
	return Option[T]{}
}

// IsSome reports whether a value is present.
func (o Option[T]) IsSome() bool {
	return o.ok
}

// IsNone reports whether no value is present.
func (o Option[T]) IsNone() bool {
	return !o.ok
}

// Unwrap returns the contained value and panics if absent.
func (o Option[T]) Unwrap() T {
	if !o.ok {
		panic("option: Unwrap called on None")
	}
	return o.value
}

// UnwrapOr returns the contained value, or fallback if absent.
func (o Option[T]) UnwrapOr(fallback T) T {
	if !o.ok {
		return fallback
	}
	return o.value
}

// MarshalJSON encodes Some as the JSON of its value and None as null.
func (o Option[T]) MarshalJSON() ([]byte, error) {
	if !o.ok {
		return []byte("null"), nil
	}
	b, err := json.Marshal(o.value)
	if err != nil {
		return nil, fmt.Errorf("marshal option: %w", err)
	}
	return b, nil
}

// UnmarshalJSON decodes null as None and any other JSON value as Some.
func (o *Option[T]) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*o = None[T]()
		return nil
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return fmt.Errorf("unmarshal option: %w", err)
	}
	*o = Some(v)
	return nil
}

// String implements fmt.Stringer.
func (o Option[T]) String() string {
	if !o.ok {
		return "None"
	}
	return fmt.Sprintf("Some(%v)", o.value)
}
