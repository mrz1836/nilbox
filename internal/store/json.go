package store

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// EncodeJSON encodes v as the text of a JSON column. A nil slice is stored as [] and a nil
// map as {}, never as null, so a column that holds a list or an object always holds one.
func EncodeJSON(v any) (string, error) {
	switch rv := reflect.ValueOf(v); {
	case rv.Kind() == reflect.Slice && rv.IsNil():
		return "[]", nil
	case rv.Kind() == reflect.Map && rv.IsNil():
		return "{}", nil
	}

	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("encode %T as JSON: %w", v, err)
	}

	return string(b), nil
}

// DecodeJSON decodes the text of a JSON column into a T. On an error it returns T's zero
// value.
func DecodeJSON[T any](text string) (T, error) {
	var v T
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		var zero T

		return zero, fmt.Errorf("decode %T from JSON: %w", v, err)
	}

	return v, nil
}
