package devtools_test

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/devtools"
)

// whitePicture is a white PNG width by height pixels.
func whitePicture(t *testing.T, width, height int) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)

	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))

	return buf.Bytes()
}

// colorsAt decodes the PNG picture and returns its colors at each point.
func colorsAt(t *testing.T, picture []byte, points ...image.Point) []color.RGBA {
	t.Helper()

	img, err := png.Decode(bytes.NewReader(picture))
	require.NoError(t, err)

	colors := make([]color.RGBA, 0, len(points))
	for _, p := range points {
		c, ok := color.RGBAModel.Convert(img.At(p.X, p.Y)).(color.RGBA)
		require.True(t, ok)

		colors = append(colors, c)
	}

	return colors
}

func TestOutline(t *testing.T) {
	t.Parallel()

	// The colors of the pictures: the window's, and the box Outline draws.
	white := color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	blue := color.RGBA{R: 0x25, G: 0x63, B: 0xeb, A: 0xff}

	t.Run("boxes the element just outside it, in the picture's own pixels", func(t *testing.T) {
		t.Parallel()

		// A viewport 20 CSS pixels wide, its picture 40: two pixels to one. The element
		// spans pixels 10 to 20 each way, and the box, 2 CSS pixels thick, the 4 around it.
		boxed, err := devtools.Outline(whitePicture(t, 40, 40), devtools.Rect{X: 5, Y: 5, Width: 5, Height: 5}, 20)
		require.NoError(t, err)

		assert.Equal(t, []color.RGBA{blue, blue, blue, blue}, colorsAt(t, boxed,
			image.Pt(8, 15), image.Pt(21, 15), image.Pt(15, 7), image.Pt(15, 22)), "left, right, top, and bottom")
		assert.Equal(t, []color.RGBA{white, white, white}, colorsAt(t, boxed,
			image.Pt(15, 15), image.Pt(5, 15), image.Pt(15, 25)), "the element, and what is beyond the box, as they were")
	})

	t.Run("reads a pixel to a CSS pixel when the viewport's width is not known", func(t *testing.T) {
		t.Parallel()

		boxed, err := devtools.Outline(whitePicture(t, 40, 40), devtools.Rect{X: 5, Y: 5, Width: 5, Height: 5}, 0)
		require.NoError(t, err)

		assert.Equal(t, []color.RGBA{blue, white}, colorsAt(t, boxed, image.Pt(4, 7), image.Pt(7, 7)))
	})

	t.Run("draws what of the box falls on the picture", func(t *testing.T) {
		t.Parallel()

		boxed, err := devtools.Outline(whitePicture(t, 20, 20), devtools.Rect{X: -5, Y: 15, Width: 50, Height: 50}, 20)
		require.NoError(t, err)

		assert.Equal(t, []color.RGBA{blue, white}, colorsAt(t, boxed, image.Pt(10, 14), image.Pt(10, 18)))
	})

	t.Run("leaves a picture with no element on it as it was", func(t *testing.T) {
		t.Parallel()

		picture := whitePicture(t, 4, 4)

		same, err := devtools.Outline(picture, devtools.Rect{X: 1, Y: 1}, 4)
		require.NoError(t, err)
		assert.Equal(t, picture, same)
	})

	t.Run("refuses what is not a PNG", func(t *testing.T) {
		t.Parallel()

		_, err := devtools.Outline([]byte("png"), devtools.Rect{Width: 1, Height: 1}, 1)
		require.ErrorContains(t, err, "read the picture")
	})
}
