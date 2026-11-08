package settings

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/mrz1836/nilbox/internal/prefs"
)

// KeyWindowSize is the size the window opens at, stored as "<width>x<height>".
const KeyWindowSize = "window.size"

// Window sizes, in points. A fresh database opens the window at the default size; the
// minimum is also the smallest the window can be resized to.
const (
	DefaultWindowWidth  = 1280
	DefaultWindowHeight = 800
	MinWindowWidth      = 800
	MinWindowHeight     = 520
	MaxWindowWidth      = 7680
	MaxWindowHeight     = 4320
)

// ErrInvalidWindowSize is returned for a window size outside the bounds.
var ErrInvalidWindowSize = errors.New("invalid window size")

// WindowSize is the window's size, in points.
type WindowSize struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// DefaultWindowSize returns the size the window opens at on a fresh database.
func DefaultWindowSize() WindowSize {
	return WindowSize{Width: DefaultWindowWidth, Height: DefaultWindowHeight}
}

// Fit returns the size shrunk, where needed, to fit a screen area of the given width and
// height, but never below the minimum. A zero or negative area leaves the size as it is.
func (w WindowSize) Fit(width, height int) WindowSize {
	if width > 0 && w.Width > width {
		w.Width = max(width, MinWindowWidth)
	}

	if height > 0 && w.Height > height {
		w.Height = max(height, MinWindowHeight)
	}

	return w
}

// ReadWindowSize returns the size the window opens at, from the preferences in store: the
// stored size, or the default when none is stored or the stored one is no longer valid.
// Only the desktop shell reads the size on its own, so this is a function over the store
// rather than a method of the Service the window binds.
func ReadWindowSize(ctx context.Context, store *prefs.Store) (WindowSize, error) {
	stored, _, err := store.Get(ctx, KeyWindowSize)
	if err != nil {
		return DefaultWindowSize(), fmt.Errorf("read the window size: %w", err)
	}

	if size, ok := decodeWindowSize(stored); ok {
		return size, nil
	}

	return DefaultWindowSize(), nil
}

// SetWindowSize stores the size the window opens at, which takes effect at the next
// launch: MinWindowWidth by MinWindowHeight up to MaxWindowWidth by MaxWindowHeight. It
// returns the settings with it.
func (s *Service) SetWindowSize(ctx context.Context, size WindowSize) (Settings, error) {
	if !size.valid() {
		return Settings{}, fmt.Errorf("%w: %d x %d; choose %d x %d up to %d x %d", ErrInvalidWindowSize, size.Width,
			size.Height, MinWindowWidth, MinWindowHeight, MaxWindowWidth, MaxWindowHeight)
	}

	return s.save(ctx, setting(KeyWindowSize, size.encode()))
}

// valid reports whether the size is within the bounds.
func (w WindowSize) valid() bool {
	return w.Width >= MinWindowWidth && w.Width <= MaxWindowWidth &&
		w.Height >= MinWindowHeight && w.Height <= MaxWindowHeight
}

// encode renders the size as stored: "<width>x<height>".
func (w WindowSize) encode() string {
	return strconv.Itoa(w.Width) + "x" + strconv.Itoa(w.Height)
}

// decodeWindowSize parses a stored size, reporting false for one that is malformed or
// out of bounds.
func decodeWindowSize(stored string) (WindowSize, bool) {
	width, height, ok := strings.Cut(stored, "x")
	if !ok {
		return WindowSize{}, false
	}

	w, errW := strconv.Atoi(width)
	h, errH := strconv.Atoi(height)

	size := WindowSize{Width: w, Height: h}
	if errW != nil || errH != nil || !size.valid() {
		return WindowSize{}, false
	}

	return size, true
}
