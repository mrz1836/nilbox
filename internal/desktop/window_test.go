package desktop_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/desktop"
)

func TestWithinArea(t *testing.T) {
	t.Parallel()

	// A 1440 x 875 work area below a 25-point menu bar.
	area := desktop.Rect{X: 0, Y: 25, Width: 1440, Height: 875}

	tests := []struct {
		name   string
		window desktop.Rect
		x, y   int
	}{
		{name: "inside stays put", window: desktop.Rect{X: 100, Y: 100, Width: 1040, Height: 676}, x: 100, y: 100},
		{name: "past the right edge moves left", window: desktop.Rect{X: 640, Y: 100, Width: 1040, Height: 676}, x: 400, y: 100},
		{name: "past the bottom edge moves up", window: desktop.Rect{X: 100, Y: 380, Width: 1040, Height: 676}, x: 100, y: 224},
		{
			name: "grown in the bottom-right corner moves back into it", window: desktop.Rect{X: 640, Y: 380, Width: 1040, Height: 676},
			x: 400, y: 224,
		},
		{name: "wider than the area keeps its left edge on it", window: desktop.Rect{X: 300, Y: 100, Width: 1600, Height: 676}, x: 0, y: 100},
		{name: "taller than the area keeps its top edge on it", window: desktop.Rect{X: 100, Y: 300, Width: 1040, Height: 1000}, x: 100, y: 25},
		{name: "already left of the area is left alone", window: desktop.Rect{X: -50, Y: 100, Width: 1040, Height: 676}, x: -50, y: 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			x, y := desktop.WithinArea(tt.window, area)
			assert.Equal(t, tt.x, x, "x")
			assert.Equal(t, tt.y, y, "y")
		})
	}

	t.Run("on a screen whose area does not start at the origin", func(t *testing.T) {
		t.Parallel()

		// A second screen to the right of the first.
		right := desktop.Rect{X: 1440, Y: 0, Width: 1920, Height: 1055}

		x, y := desktop.WithinArea(desktop.Rect{X: 2800, Y: 600, Width: 1040, Height: 676}, right)
		assert.Equal(t, 2320, x)
		assert.Equal(t, 379, y)
	})
}
