package brand

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// markPoints walks MarkPath and returns every coordinate it names, as x and y lists. It
// supports the commands the mark uses: M, L, and C take x,y pairs, H takes an x, and Z
// takes nothing.
func markPoints(t *testing.T) (xs, ys []float64) {
	t.Helper()

	tokens := regexp.MustCompile(`[A-Za-z]|-?[0-9]+(?:\.[0-9]+)?`).FindAllString(MarkPath, -1)
	require.Equal(t, strings.Join(tokens, ""), strings.NewReplacer(" ", "").Replace(MarkPath),
		"the path holds only commands and numbers")

	var command string

	var args []float64

	flush := func() {
		switch command {
		case "M", "L", "C":
			require.Zerof(t, len(args)%2, "%s takes x,y pairs, got %v", command, args)

			for i := 0; i < len(args); i += 2 {
				xs = append(xs, args[i])
				ys = append(ys, args[i+1])
			}
		case "H":
			require.Lenf(t, args, 1, "H takes one x")

			xs = append(xs, args[0])
		case "Z":
			require.Empty(t, args, "Z takes no arguments")
		case "":
		default:
			require.Failf(t, "unexpected path command", "%q", command)
		}

		args = args[:0]
	}

	for _, tok := range tokens {
		v, err := strconv.ParseFloat(tok, 64)
		if err != nil {
			flush()

			command = tok

			continue
		}

		args = append(args, v)
	}

	flush()

	return xs, ys
}

func TestMarkIsThreeClosedContours(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 3, strings.Count(MarkPath, "M"), "the box's two edges and the slash")
	assert.Equal(t, 3, strings.Count(MarkPath, "Z"), "every contour is closed")
	assert.True(t, strings.HasPrefix(MarkPath, "M"))
	assert.True(t, strings.HasSuffix(MarkPath, "Z"))
}

// TestMarkFillsItsGrid checks that every point lies on the grid and that the outline
// touches all four edges, so the grid is the mark's exact bounding box and the frontend
// and the icon can size and center it from MarkWidth and MarkHeight alone.
func TestMarkFillsItsGrid(t *testing.T) {
	t.Parallel()

	xs, ys := markPoints(t)
	require.NotEmpty(t, xs)
	require.NotEmpty(t, ys)

	bounds := func(vs []float64) (lo, hi float64) {
		lo, hi = vs[0], vs[0]
		for _, v := range vs {
			lo, hi = min(lo, v), max(hi, v)
		}

		return lo, hi
	}

	minX, maxX := bounds(xs)
	minY, maxY := bounds(ys)

	assert.InDelta(t, 0, minX, 0.001)
	assert.InDelta(t, MarkWidth, maxX, 0.001)
	assert.InDelta(t, 0, minY, 0.001)
	assert.InDelta(t, MarkHeight, maxY, 0.001)
}

func TestMarkHighlightIsAColor(t *testing.T) {
	t.Parallel()

	assert.Regexp(t, `^#[0-9A-F]{6}$`, MarkHighlight)
}
