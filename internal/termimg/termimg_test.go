package termimg

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// twoTone is a w×h image: the left half red, the right half blue.
func twoTone(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{255, 0, 0, 255}
			if x >= w/2 {
				c = color.RGBA{0, 0, 255, 255}
			}
			img.Set(x, y, c)
		}
	}
	return img
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestDecodeKnowsPNGAndRejectsNoise(t *testing.T) {
	img, kind, err := Decode(encodePNG(t, twoTone(8, 4)))
	if err != nil || kind != "png" || img.Bounds().Dx() != 8 {
		t.Fatalf("Decode(png) = %v %q %v", img, kind, err)
	}
	if _, _, err := Decode([]byte("\x00\x01\x02 not an image")); err == nil {
		t.Fatal("noise must not decode")
	}
}

func TestCellsFitTheBoxAndKeepTheAspect(t *testing.T) {
	// 80×80 px into 40 cols × 30 rows: a cell is 1 px wide and 2 px tall,
	// so the width binds — 40 cols → scale ½ → 40 px tall → 20 rows.
	cells := Cells(twoTone(80, 80), 40, 30)
	if len(cells) != 20 || len(cells[0]) != 40 {
		t.Fatalf("cells = %d rows × %d cols, want 20 × 40", len(cells), len(cells[0]))
	}
	// 8×4 px into a huge box: never scaled up past 1 px per cell width.
	cells = Cells(twoTone(8, 4), 200, 100)
	if len(cells) != 2 || len(cells[0]) != 8 {
		t.Fatalf("small image = %d rows × %d cols, want 2 × 8 (no upscaling)", len(cells), len(cells[0]))
	}
}

func TestCellsAverageTheirPixels(t *testing.T) {
	cells := Cells(twoTone(8, 4), 8, 2)
	l, r := cells[0][0], cells[0][7]
	if l.Top != (color.RGBA{255, 0, 0, 255}) || l.Bottom != l.Top {
		t.Fatalf("left cell = %+v, want red top and bottom", l)
	}
	if r.Top != (color.RGBA{0, 0, 255, 255}) {
		t.Fatalf("right cell = %+v, want blue", r)
	}
	// Halving the width averages a red and a blue column into purple.
	cells = Cells(twoTone(4, 2), 2, 1)
	if len(cells[0]) != 2 {
		t.Fatalf("cols = %d", len(cells[0]))
	}
}

func TestRampMapsLuminanceToGlyphs(t *testing.T) {
	cells := Cells(twoTone(8, 4), 8, 2)
	rows := Ramp(cells)
	if len(rows) != 2 || len([]rune(rows[0])) != 8 {
		t.Fatalf("ramp rows = %q", rows)
	}
	black := Ramp([][]Cell{{{Top: color.RGBA{0, 0, 0, 255}, Bottom: color.RGBA{0, 0, 0, 255}}}})[0]
	white := Ramp([][]Cell{{{Top: color.RGBA{255, 255, 255, 255}, Bottom: color.RGBA{255, 255, 255, 255}}}})[0]
	if black == white {
		t.Fatalf("black %q and white %q must differ", black, white)
	}
}

func TestShrinkBoundsTheWorkingImage(t *testing.T) {
	small := Shrink(twoTone(3000, 1000), 600, 600)
	if b := small.Bounds(); b.Dx() != 600 || b.Dy() != 200 {
		t.Fatalf("shrunk to %v, want 600×200 (the wider side binds, aspect kept)", b)
	}
	if got := small.At(0, 0); got != (color.RGBA{255, 0, 0, 255}) {
		t.Fatalf("left pixel = %v, want red", got)
	}
	same := Shrink(twoTone(8, 4), 600, 600)
	if b := same.Bounds(); b.Dx() != 8 || b.Dy() != 4 {
		t.Fatalf("a small image is returned as is, got %v", b)
	}
}
