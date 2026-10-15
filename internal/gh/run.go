package gh

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

func Decode[T any](raw []byte) (T, error) {
	var decoded T
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return decoded, fmt.Errorf("decode %T: %w", decoded, err)
	}
	return decoded, nil
}

func DecodeStream[T any](raw []byte) ([]T, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	var all []T
	for pages := 0; ; pages++ {
		var page []T
		err := dec.Decode(&page)
		if errors.Is(err, io.EOF) && pages > 0 {
			return all, nil
		}
		if err != nil {
			return nil, fmt.Errorf("decode %T pages: %w", page, err)
		}
		all = append(all, page...)
	}
}
