package settings

import (
	"context"
	"errors"
	"fmt"

	"github.com/mrz1836/nilbox/internal/event"
)

// KeyTextSize is the size of the app's text, stored as TextSizeDefault, TextSizeLarge, or
// TextSizeLarger.
const KeyTextSize = "ui.textSize"

// TextSize is the size of the app's text, apart from the size of its window. A larger size
// scales the text and the spacing sized with it, as a browser's font size setting does;
// the frontend sets the scale for each choice.
type TextSize string

// Text size choices.
const (
	// TextSizeDefault shows the text at the size the app is designed at (100%).
	TextSizeDefault TextSize = "default"

	// TextSizeLarge shows the text a step larger (115%).
	TextSizeLarge TextSize = "large"

	// TextSizeLarger shows the text two steps larger (130%).
	TextSizeLarger TextSize = "larger"
)

// DefaultTextSize is the text size of a fresh database.
const DefaultTextSize = TextSizeDefault

// ErrInvalidTextSize is returned for a text size that is not one of the choices.
var ErrInvalidTextSize = errors.New("unknown text size")

// Percent is the scale the text size sets, as a percent of the size the app is designed
// at. A size that is not one of the choices counts as DefaultTextSize. The frontend keeps
// the same scales (frontend/src/lib/textSize.ts), and a test holds the two in step.
func (t TextSize) Percent() int {
	switch t {
	case TextSizeDefault:
		return 100
	case TextSizeLarge:
		return 115
	case TextSizeLarger:
		return 130
	default:
		return DefaultTextSize.Percent()
	}
}

// textSizes is the text size choice: the sizes in the order Settings offers them,
// smallest first, falling back to DefaultTextSize for a stored size that is unset or no
// longer a choice.
func textSizes() choice[TextSize] {
	return choice[TextSize]{
		values: []TextSize{TextSizeDefault, TextSizeLarge, TextSizeLarger}, fallback: DefaultTextSize,
		invalid: ErrInvalidTextSize,
	}
}

// TextSize returns the stored text size, or DefaultTextSize when none is stored or the
// stored value is no longer a choice. The desktop shell reads it to open the window at
// it.
func (s *Service) TextSize(ctx context.Context) (TextSize, error) {
	stored, _, err := s.prefs.Get(ctx, KeyTextSize)
	if err != nil {
		return DefaultTextSize, fmt.Errorf("read the text size: %w", err)
	}

	return textSizes().decode(stored), nil
}

// SetTextSize stores the size of the app's text: TextSizeDefault, TextSizeLarge, or
// TextSizeLarger, and returns the settings with it. The page shows the new size itself;
// once it is stored, an event.TextSizeChanged is emitted, so the desktop shell sets the
// window's smallest size to suit it.
func (s *Service) SetTextSize(ctx context.Context, size TextSize) (Settings, error) {
	values, err := checkTextSize(size)
	if err != nil {
		return Settings{}, err
	}

	out, err := s.save(ctx, setMany(values, ""))
	if err != nil {
		return Settings{}, err
	}

	s.events.Emit(event.TextSizeChanged{Size: string(size)})

	return out, nil
}

// checkTextSize refuses a text size that is not one of the choices, and returns its
// write.
func checkTextSize(size TextSize) (map[string]string, error) {
	if err := textSizes().check(size); err != nil {
		return nil, err
	}

	return map[string]string{KeyTextSize: string(size)}, nil
}
