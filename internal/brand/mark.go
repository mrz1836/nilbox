package brand

// The mark is the product's logo: the empty-set sign, ∅, drawn as a box. Nil, and a box.
//
// It is one outline in SVG path data, filled with the nonzero rule, on a MarkWidth by
// MarkHeight grid whose origin is the top-left corner of the mark's bounding box. The app
// icon centers it on Apple's 1024-point icon grid, where the box is 408 points square with
// 74-point strokes and continuous (squircle) corners of radius 132, and the slash, also 74
// points wide, crosses it at 58 degrees and runs 52 points past it at either end. The
// frontend draws the same outline beside the product name.
//
// After changing the mark, run `magex brand:generate` and then `magex brand:icons`, which
// renders the app icon and records which artwork it came from.
const (
	// MarkWidth is the width of the mark's grid.
	MarkWidth = 408

	// MarkHeight is the height of the mark's grid.
	MarkHeight = 512

	// MarkPath is the mark's outline: the box's outer edge, clockwise; its inner edge,
	// counterclockwise, so the nonzero rule leaves the box open; and the slash, which
	// overlaps both.
	MarkPath = markBoxOuter + markBoxInner + markSlash

	// MarkHighlight is the warm light the mark catches at its top edge: the top of the app
	// icon glyph's gradient, and the color the frontend's mark warms toward from the accent.
	MarkHighlight = "#FFC690"
)

// The three contours of MarkPath.
const (
	markBoxOuter = "M201.8 52L206.2 52C264.3 52 293.4 52 319.6 60.6C358.8 73.4 386.6 101.2 399.4 140.4" +
		"C408 166.6 408 195.7 408 253.8L408 258.2C408 316.3 408 345.4 399.4 371.6" +
		"C386.6 410.8 358.8 438.6 319.6 451.4C293.4 460 264.3 460 206.2 460L201.8 460" +
		"C143.7 460 114.6 460 88.4 451.4C49.2 438.6 21.4 410.8 8.6 371.6C0 345.4 0 316.3 0 258.2L0 253.8" +
		"C0 195.7 0 166.6 8.6 140.4C21.4 101.2 49.2 73.4 88.4 60.6C114.6 52 143.7 52 201.8 52Z"

	markBoxInner = "M162.7 126C137.1 126 124.4 126 112.9 129.8C95.6 135.4 83.4 147.6 77.8 164.9" +
		"C74 176.4 74 189.1 74 214.7L74 297.3C74 322.9 74 335.6 77.8 347.1" +
		"C83.4 364.4 95.6 376.6 112.9 382.2C124.4 386 137.1 386 162.7 386L245.3 386" +
		"C270.9 386 283.6 386 295.1 382.2C312.4 376.6 324.6 364.4 330.2 347.1" +
		"C334 335.6 334 322.9 334 297.3L334 214.7C334 189.1 334 176.4 330.2 164.9" +
		"C324.6 147.6 312.4 135.4 295.1 129.8C283.6 126 270.9 126 245.3 126L162.7 126Z"

	markSlash = "M320.3 0H407.6L87.7 512H0.4Z"
)
