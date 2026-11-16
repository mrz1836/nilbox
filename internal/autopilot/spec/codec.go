package spec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
)

// MaxBytes caps a stored spec.
const MaxBytes = 64 << 10

// maxDepth caps how deep a spec's JSON nests, well past the format's own depth.
const maxDepth = 16

var (
	// ErrUnreadable is returned for a spec that isn't one: not JSON, a field the format
	// doesn't have, a key given twice, a value of the wrong type, anything after the
	// document, or more than MaxBytes.
	ErrUnreadable = errors.New("the routine can't be read")

	// ErrNewer is returned for a spec a newer build wrote, which this one doesn't guess
	// at.
	ErrNewer = errors.New("made by a newer version: update to edit it")
)

// Reason is why a routine is broken.
type Reason string

// The reasons a routine is broken.
const (
	// ReasonNewer is a spec a newer build wrote.
	ReasonNewer Reason = "newer"

	// ReasonUnreadable is a spec that can't be read.
	ReasonUnreadable Reason = "unreadable"

	// ReasonInvalid is a spec that reads but no longer validates.
	ReasonInvalid Reason = "invalid"
)

// BrokenError is why a stored spec won't load. Its routine doesn't run and isn't
// rewritten; its row says why.
type BrokenError struct {
	Reason Reason

	// Err is what went wrong: ErrNewer, ErrUnreadable, or a *ValidationError.
	Err error
}

// Error says why the routine is broken.
func (e *BrokenError) Error() string {
	return e.Err.Error()
}

// Unwrap returns what went wrong.
func (e *BrokenError) Unwrap() error {
	return e.Err
}

// Encode writes a spec as it is stored: compact JSON, its fields in the format's order.
func Encode(s Spec) ([]byte, error) {
	data, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("encode the routine: %w", err)
	}

	return data, nil
}

// Decode reads a spec strictly, refusing a field the format doesn't have, a key given
// twice, a value of the wrong type, anything after the document, and more than MaxBytes,
// each as ErrUnreadable. It checks no value: Validate does.
func Decode(data []byte) (Spec, error) {
	if len(data) > MaxBytes {
		return Spec{}, fmt.Errorf("%w: larger than %d bytes", ErrUnreadable, MaxBytes)
	}

	if err := checkKeys(data); err != nil {
		return Spec{}, fmt.Errorf("%w: %w", ErrUnreadable, err)
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	var s Spec
	if err := dec.Decode(&s); err != nil {
		return Spec{}, fmt.Errorf("%w: %w", ErrUnreadable, err)
	}

	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Spec{}, fmt.Errorf("%w: more after the routine", ErrUnreadable)
	}

	return s, nil
}

// Load reads a stored spec: its version first, then the spec strictly, normalized, and
// validated. A spec it can't load returns a *BrokenError, with the spec as far as it
// was read.
func Load(data []byte) (Spec, error) {
	version, err := peekVersion(data)
	if err != nil {
		return Spec{}, &BrokenError{Reason: ReasonUnreadable, Err: err}
	}

	if version > Version {
		return Spec{}, &BrokenError{Reason: ReasonNewer, Err: fmt.Errorf("%w (version %d)", ErrNewer, version)}
	}

	s, err := Decode(data)
	if err != nil {
		return Spec{}, &BrokenError{Reason: ReasonUnreadable, Err: err}
	}

	s = Normalize(migrate(s))

	if err := Validate(s); err != nil {
		return s, &BrokenError{Reason: ReasonInvalid, Err: err}
	}

	return s, nil
}

// migrate brings a spec of an older version to Version. Version 1 is the first, so
// there is nothing older to bring forward yet; a later version adds its step here.
func migrate(s Spec) Spec {
	return s
}

// peekVersion reads only a spec's version, so a newer build's spec, whose fields this
// build doesn't know, says it is newer rather than unreadable.
func peekVersion(data []byte) (int, error) {
	if len(data) > MaxBytes {
		return 0, fmt.Errorf("%w: larger than %d bytes", ErrUnreadable, MaxBytes)
	}

	var head struct {
		Version json.RawMessage `json:"version"`
	}

	if err := json.Unmarshal(data, &head); err != nil {
		return 0, fmt.Errorf("%w: %w", ErrUnreadable, err)
	}

	version, err := strconv.Atoi(string(head.Version))
	if err != nil || version < 1 {
		return 0, fmt.Errorf("%w: it has no version", ErrUnreadable)
	}

	return version, nil
}

// errDuplicateKey and errTooDeep are why checkKeys refuses a document.
var (
	errDuplicateKey = errors.New("a key is given twice")
	errTooDeep      = errors.New("it nests too deep")
)

// checkKeys refuses JSON that gives an object's key twice, which encoding/json would
// read as the last one silently, or that nests deeper than maxDepth. JSON that isn't
// well formed is left for the decoder to refuse.
func checkKeys(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))

	var stack []map[string]bool

	// expectKey is set inside an object where the next string is a key.
	expectKey := false

	for {
		tok, err := dec.Token()
		if err != nil {
			return nil //nolint:nilerr // a document that isn't well formed is the decoder's to refuse
		}

		stack, expectKey, err = step(stack, expectKey, tok)
		if err != nil {
			return err
		}
	}
}

// step follows one token of a document for checkKeys.
func step(stack []map[string]bool, expectKey bool, tok json.Token) ([]map[string]bool, bool, error) {
	switch t := tok.(type) {
	case json.Delim:
		return delim(stack, t)
	case string:
		if expectKey {
			keys := stack[len(stack)-1]
			if keys[t] {
				return nil, false, fmt.Errorf("%w: %q", errDuplicateKey, t)
			}

			keys[t] = true

			return stack, false, nil
		}
	}

	return stack, len(stack) > 0 && stack[len(stack)-1] != nil, nil
}

// delim follows an opening or closing bracket for checkKeys: an object pushes its keys,
// an array pushes nil, and a closing bracket pops.
func delim(stack []map[string]bool, d json.Delim) ([]map[string]bool, bool, error) {
	switch d {
	case '{':
		stack = append(stack, map[string]bool{})
	case '[':
		stack = append(stack, nil)
	default:
		stack = stack[:len(stack)-1]
	}

	if len(stack) > maxDepth {
		return nil, false, errTooDeep
	}

	return stack, len(stack) > 0 && stack[len(stack)-1] != nil, nil
}
