package desktop

// Rect is a rectangle on screen, in points, from its top-left corner.
type Rect struct {
	X, Y, Width, Height int
}

// WithinArea returns where a window with the given bounds moves to so it sits inside
// area: left and up, where needed, until its right and bottom edges are inside area, but
// never past area's left and top edges, so a window larger than area keeps its top-left
// corner on it. A window already inside area stays where it is.
func WithinArea(window, area Rect) (x, y int) {
	x, y = window.X, window.Y

	if right := area.X + area.Width; x+window.Width > right {
		x = max(right-window.Width, area.X)
	}

	if bottom := area.Y + area.Height; y+window.Height > bottom {
		y = max(bottom-window.Height, area.Y)
	}

	return x, y
}
