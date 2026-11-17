package settings

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mrz1836/nilbox/internal/prefs/prefval"
)

// KeyHiddenLabels is the patterns of the labels the inbox's cards leave out, as a JSON
// list, such as "size/*": a card already shows its own size, so the size label repeats it.
// Only the window reads it, for the cards' labels alone; the filter, the issue sheet's
// label picker, Autopilot, and the prompts' variables still see every label. A key never
// stored holds DefaultHiddenLabels; a list stored empty stays empty.
//
// A pattern matches a label's whole name, ignoring case: * matches any run of characters,
// / included, ? matches one character, and anything else matches itself, as Autopilot's
// label patterns do (internal/autopilot/eval's globMatch, and frontend/src/lib/labels.ts,
// which the window matches with).
const KeyHiddenLabels = "labels.hidden"

// Bounds on the hidden labels. A repository's labels number far fewer; they only keep a
// stray call from storing anything unbounded.
const (
	maxHiddenLabels      = 100
	maxHiddenLabelLength = 100
)

var (
	// ErrTooManyHiddenLabels is returned for more than maxHiddenLabels patterns.
	ErrTooManyHiddenLabels = errors.New("too many hidden labels")

	// ErrHiddenLabelTooLong is returned for a pattern longer than maxHiddenLabelLength
	// characters.
	ErrHiddenLabelTooLong = errors.New("a hidden label pattern is too long")

	// ErrInvalidHiddenLabel is returned for a pattern holding a control character, such as
	// a line break, which no label's name holds; the window keys its matcher by the
	// patterns joined with line breaks.
	ErrInvalidHiddenLabel = errors.New("a hidden label pattern holds a control character")
)

// DefaultHiddenLabels are the patterns a fresh database hides, which Restore the defaults
// goes back to: the size labels, which the card's own size grade already shows.
func DefaultHiddenLabels() []string {
	return []string{"size/*"}
}

// cleanHiddenLabels trims each pattern and leaves out the blank ones and those already in
// the list, ignoring case, keeping the first, in order. It refuses more than
// maxHiddenLabels patterns, one longer than maxHiddenLabelLength characters, and one that
// holds a control character.
func cleanHiddenLabels(patterns []string) ([]string, error) {
	out := make([]string, 0, len(patterns))
	seen := make(map[string]bool, len(patterns))

	for _, raw := range patterns {
		p := strings.TrimSpace(raw)
		key := strings.ToLower(p)

		if p == "" || seen[key] {
			continue
		}

		if utf8.RuneCountInString(p) > maxHiddenLabelLength {
			return nil, fmt.Errorf("%w: keep each to %d characters", ErrHiddenLabelTooLong, maxHiddenLabelLength)
		}

		if strings.ContainsFunc(p, unicode.IsControl) {
			return nil, fmt.Errorf("%w: %q", ErrInvalidHiddenLabel, p)
		}

		seen[key] = true

		out = append(out, p)
	}

	if len(out) > maxHiddenLabels {
		return nil, fmt.Errorf("%w: %d; keep it to %d", ErrTooManyHiddenLabels, len(out), maxHiddenLabels)
	}

	return out, nil
}

// decodeHiddenLabels reads the hidden labels from the stored preferences: the defaults
// when none were ever stored or what is stored can't be read, and the stored list,
// cleaned, otherwise, an empty one included.
func decodeHiddenLabels(stored map[string]string) []string {
	raw, ok := stored[KeyHiddenLabels]
	if !ok {
		return DefaultHiddenLabels()
	}

	list, ok := prefval.List[string](raw)
	if !ok {
		return DefaultHiddenLabels()
	}

	clean, err := cleanHiddenLabels(list)
	if err != nil {
		return DefaultHiddenLabels()
	}

	return clean
}

// SetHiddenLabels stores the patterns of the labels the inbox's cards leave out, in order,
// and returns the settings with them. Each is trimmed, and blank ones and repeats (ignoring
// case) are left out; an empty list hides no label.
func (s *Service) SetHiddenLabels(ctx context.Context, patterns []string) (Settings, error) {
	values, err := checkHiddenLabels(patterns)
	if err != nil {
		return Settings{}, err
	}

	return s.save(ctx, setMany(values, ""))
}

// checkHiddenLabels refuses patterns SetHiddenLabels would, and returns their write,
// cleaned.
func checkHiddenLabels(patterns []string) (map[string]string, error) {
	clean, err := cleanHiddenLabels(patterns)
	if err != nil {
		return nil, err
	}

	return map[string]string{KeyHiddenLabels: prefval.EncodeList(clean)}, nil
}
