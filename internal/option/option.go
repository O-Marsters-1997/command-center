package option

import (
	"encoding/json"
	"fmt"
)

// Option holds a value that may be absent. It is comparable when T is.
type Option[T any] struct {
	value T
	ok    bool
}

func Some[T any](v T) Option[T] {
	return Option[T]{value: v, ok: true}
}

func None[T any]() Option[T] {
	return Option[T]{}
}

func (o Option[T]) IsSome() bool {
	return o.ok
}

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

func (o Option[T]) UnwrapOr(fallback T) T {
	if !o.ok {
		return fallback
	}
	return o.value
}

// MarshalJSON encodes Some as its value and None as null.
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

// UnmarshalJSON decodes null as None and all other JSON values as Some.
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

func (o Option[T]) String() string {
	if !o.ok {
		return "None"
	}
	return fmt.Sprintf("Some(%v)", o.value)
}
