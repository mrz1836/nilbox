package devtools

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"strings"
	"time"
)

// noteTrailRunes is how much of a note's words its entry on the trail shows.
const noteTrailRunes = 80

// Rect is where the web view lays an element out: in CSS pixels, from the viewport's top
// left.
type Rect struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Element is an element of the page as the agent describes it: its tag, id, classes,
// role, and name (its label, title, or text), where it is, and its data-* and aria-*
// attributes.
type Element struct {
	Tag     string            `json:"tag"`
	ID      string            `json:"id,omitempty"`
	Classes string            `json:"classes,omitempty"`
	Role    string            `json:"role,omitempty"`
	Name    string            `json:"name,omitempty"`
	Rect    Rect              `json:"rect"`
	Marks   map[string]string `json:"marks,omitempty"`
}

// A Note is what the user said about an element they pointed at in the window (⌥⌘P, or
// inspect:point): their words; the control it is in as the trail names it (Target), and
// where (Where); the view the window showed; the element as the agent describes it, and
// its text around it; under React's development build (app:dev), the components that
// render it, outermost first, and the file whose JSX made it, its line approximate; and
// the viewport's size and a picture of the window, taken as the note came, or why there
// is none. N numbers the notes from 1 each time the app starts.
type Note struct {
	N              int       `json:"n"`
	Time           time.Time `json:"time"`
	Text           string    `json:"text"`
	Target         string    `json:"target"`
	Where          string    `json:"where,omitempty"`
	View           string    `json:"view,omitempty"`
	Components     string    `json:"components,omitempty"`
	Source         string    `json:"source,omitempty"`
	Nearby         string    `json:"nearby,omitempty"`
	Element        Element   `json:"element"`
	ViewportWidth  int       `json:"viewportWidth"`
	ViewportHeight int       `json:"viewportHeight"`
	Shot           []byte    `json:"shot,omitempty"`
	ShotError      string    `json:"shotError,omitempty"`
}

// Outline returns the PNG picture with a box drawn just outside rect, where a note's
// element is in a viewport viewportWidth CSS pixels wide. The picture is in the web view's
// own pixels, two to a CSS pixel on a Retina screen, so rect is scaled by the picture's
// width over viewportWidth (1 when that is not known). The box is the magenta the agent
// outlines the element with in the window, which no theme of the app uses. An empty rect
// leaves the picture as it was.
func Outline(picture []byte, rect Rect, viewportWidth int) ([]byte, error) {
	src, err := png.Decode(bytes.NewReader(picture))
	if err != nil {
		return nil, fmt.Errorf("read the picture: %w", err)
	}

	if rect.Width <= 0 || rect.Height <= 0 {
		return picture, nil
	}

	bounds := src.Bounds()

	scale := 1.0
	if viewportWidth > 0 {
		scale = float64(bounds.Dx()) / float64(viewportWidth)
	}

	at := func(v int) int { return int(math.Round(float64(v) * scale)) }
	thick := max(2, at(2))

	box := image.Rect(at(rect.X), at(rect.Y), at(rect.X+rect.Width), at(rect.Y+rect.Height)).Add(bounds.Min)
	outer := box.Inset(-thick)

	img := image.NewRGBA(bounds)
	draw.Draw(img, bounds, src, bounds.Min, draw.Src)

	paint := image.NewUniform(color.RGBA{R: 0xff, G: 0x2d, B: 0x95, A: 0xff})

	for _, side := range []image.Rectangle{
		image.Rect(outer.Min.X, outer.Min.Y, outer.Max.X, box.Min.Y),
		image.Rect(outer.Min.X, box.Max.Y, outer.Max.X, outer.Max.Y),
		image.Rect(outer.Min.X, box.Min.Y, box.Min.X, box.Max.Y),
		image.Rect(box.Max.X, box.Min.Y, outer.Max.X, box.Max.Y),
	} {
		draw.Draw(img, side.Intersect(bounds), paint, image.Point{}, draw.Src)
	}

	var out bytes.Buffer
	if err = png.Encode(&out, img); err != nil {
		return nil, fmt.Errorf("write the picture: %w", err)
	}

	return out.Bytes(), nil
}

// noteTrailTarget is a note as the trail shows it: its number, its words on one line and
// cut short, and the control it is on.
func noteTrailTarget(note Note) string {
	return fmt.Sprintf("#%d “%s” on %s", note.N, clipRunes(strings.Join(strings.Fields(note.Text), " "), noteTrailRunes),
		note.Target)
}

// clipRunes is text cut to at most n runes, an ellipsis ending it when it was cut.
func clipRunes(text string, n int) string {
	runes := []rune(text)
	if len(runes) <= n {
		return text
	}

	return string(runes[:n-1]) + "…"
}
