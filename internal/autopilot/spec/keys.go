package spec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
)

// Why checkKeys refuses a document.
var (
	errDuplicateKey = errors.New("a key is given twice")
	errUnknownKey   = errors.New("a key isn't one the format has, spelled exactly")
	errTooDeep      = errors.New("it nests too deep")

	// errMalformed stops the walk at JSON that isn't well formed, which the decoder
	// refuses with its own words.
	errMalformed = errors.New("malformed")
)

// checkKeys refuses JSON whose keys aren't exactly the format's: encoding/json matches a
// key to a field ignoring case, Unicode's folding included ("ſteps" for "steps"), so
// "Trigger" beside "trigger" would read as the trigger, and an object's key given twice
// would read as the last. It walks the document beside the Spec type, holding every
// object's keys, byte for byte, to its struct's JSON names, refusing a key given twice and
// nesting past maxDepth. JSON that isn't well formed is left for the decoder to refuse.
func checkKeys(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))

	err := walkValue(dec, reflect.TypeFor[Spec](), 0)
	if errors.Is(err, errMalformed) {
		return nil
	}

	return err
}

// walkValue checks the next value of the document, which a field of type t holds.
func walkValue(dec *json.Decoder, t reflect.Type, depth int) error {
	tok, err := dec.Token()
	if err != nil {
		return errMalformed
	}

	d, ok := tok.(json.Delim)
	if !ok {
		return nil
	}

	if depth >= maxDepth {
		return errTooDeep
	}

	if d == '{' {
		return walkObject(dec, t, depth+1)
	}

	return walkArray(dec, t, depth+1)
}

// walkObject checks an object's keys against the struct type t holds, through pointers;
// one that holds no struct, which the decoder refuses as the wrong type, takes any key,
// each once.
func walkObject(dec *json.Decoder, t reflect.Type, depth int) error {
	fields := jsonFields(t)
	seen := map[string]bool{}

	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return errMalformed
		}

		key, _ := tok.(string)
		field, known := fields[key]

		switch {
		case seen[key]:
			return fmt.Errorf("%w: %q", errDuplicateKey, key)
		case fields != nil && !known:
			return fmt.Errorf("%w: %q", errUnknownKey, key)
		}

		seen[key] = true

		if err := walkValue(dec, field, depth); err != nil {
			return err
		}
	}

	_, err := dec.Token()
	if err != nil {
		return errMalformed
	}

	return nil
}

// walkArray checks each element of an array, which a slice of type t holds.
func walkArray(dec *json.Decoder, t reflect.Type, depth int) error {
	var elem reflect.Type
	if t != nil && t.Kind() == reflect.Slice {
		elem = t.Elem()
	}

	for dec.More() {
		if err := walkValue(dec, elem, depth); err != nil {
			return err
		}
	}

	_, err := dec.Token()
	if err != nil {
		return errMalformed
	}

	return nil
}

// jsonFields returns the JSON names of the struct t holds, through pointers, with each
// field's type; nil for a type that holds no struct.
func jsonFields(t reflect.Type) map[string]reflect.Type {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	if t == nil || t.Kind() != reflect.Struct {
		return nil
	}

	fields := make(map[string]reflect.Type, t.NumField())

	for i := range t.NumField() {
		f := t.Field(i)
		if name, _, _ := strings.Cut(f.Tag.Get("json"), ","); name != "" && name != "-" {
			fields[name] = f.Type
		}
	}

	return fields
}
