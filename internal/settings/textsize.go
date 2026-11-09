package settings

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/mrz1836/nilbox/internal/textutil"
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

// textSizes returns the text size choices in the order Settings offers them, smallest
// first.
func textSizes() []TextSize {
	return []TextSize{TextSizeDefault, TextSizeLarge, TextSizeLarger}
}

// validTextSize reports whether size is one of the choices.
func validTextSize(size TextSize) bool {
	return slices.Contains(textSizes(), size)
}

// decodeTextSize reads a stored text size, falling back to DefaultTextSize for one that
// is unset or no longer a choice.
func decodeTextSize(stored string) TextSize {
	if size := TextSize(stored); validTextSize(size) {
		return size
	}

	return DefaultTextSize
}

// TextSize returns the stored text size, or DefaultTextSize when none is stored or the
// stored value is no longer a choice. The desktop shell reads it to open the window at
// it.
func (s *Service) TextSize(ctx context.Context) (TextSize, error) {
	stored, _, err := s.prefs.Get(ctx, KeyTextSize)
	if err != nil {
		return DefaultTextSize, fmt.Errorf("read the text size: %w", err)
	}

	return decodeTextSize(stored), nil
}

// SetTextSize stores the size of the app's text: TextSizeDefault, TextSizeLarge, or
// TextSizeLarger, and returns the settings with it. The page shows the new size itself.
func (s *Service) SetTextSize(ctx context.Context, size TextSize) (Settings, error) {
	if !validTextSize(size) {
		return Settings{}, fmt.Errorf("%w %q: choose one of %s", ErrInvalidTextSize, size,
			textutil.Join(textSizes(), ", "))
	}

	return s.save(ctx, setting(KeyTextSize, string(size)))
}
