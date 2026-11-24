package spec

import (
	"encoding/json"
	"errors"
	"fmt"
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

// Decode reads a spec strictly, as DecodeExact reads one within MaxBytes and the depth
// cap: it refuses a field the format doesn't have, a key not spelled exactly as the
// format spells it (encoding/json alone would take "Trigger" for "trigger"), a key given
// twice, a value of the wrong type, anything after the document, and more than MaxBytes,
// each as ErrUnreadable. It checks no value: Validate does.
func Decode(data []byte) (Spec, error) {
	s, err := DecodeExact[Spec](data, MaxBytes, maxDepth)

	switch {
	case errors.Is(err, errAfter):
		return Spec{}, fmt.Errorf("%w: more after the routine", ErrUnreadable)
	case err != nil:
		return Spec{}, fmt.Errorf("%w: %w", ErrUnreadable, err)
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

	s = Normalize(s)

	if err := Validate(s); err != nil {
		return s, &BrokenError{Reason: ReasonInvalid, Err: err}
	}

	return s, nil
}

// migrate brings a spec of an older version to Version, one step per version. Version 2
// adds only the UUIDs, which a version 1 spec has none of, so a version 1 spec is one of
// version 2 as it is. A version it doesn't know, newer or none, is left for Validate to
// refuse.
func migrate(s Spec) Spec {
	if s.Version == 1 {
		s.Version = 2
	}

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
