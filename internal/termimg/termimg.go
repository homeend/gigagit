// Package termimg turns a raster image into terminal cells: half-block
// glyphs (▀) whose foreground is the upper pixel and background the lower,
// the way chafa and viu draw a thumbnail, plus a luminance glyph ramp for a
// terminal with no colour. Stdlib only (image/jpeg, image/png, image/gif);
// the caller paints the cells with its own styles. DAG leaf.
package termimg

import (
	"bytes"
	"image"
	"image/color"
	_ "image/gif"  // registered decoders: Decode picks by magic bytes
	_ "image/jpeg" //
	_ "image/png"  //
)

// MaxPixels bounds what Decode is willing to decode into memory
// (width × height); a larger image is refused rather than allocated.
const MaxPixels = 40_000_000

// Decode decodes data as a PNG, JPEG or GIF (by its magic bytes) and names
// the format. An image past MaxPixels, or anything else, is an error.
func Decode(data []byte) (image.Image, string, error) {
	cfg, kind, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, "", err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > MaxPixels {
		return nil, "", image.ErrFormat
	}
	img, kind, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", err
	}
	return img, kind, nil
}

// Cell is one terminal cell of a rendered image: the colour of its upper
// half and of its lower half (each the box average of the pixels it stands
// for). Alpha is composited over black.
type Cell struct {
	Top, Bottom color.RGBA
}

// Cells lays img out in at most cols × rows cells, keeping its aspect: a
// cell stands for a 1-pixel-wide, 2-pixel-tall box, so an image is never
// scaled UP (a small icon stays small) and is scaled down by the same
// factor on both axes until it fits. cols or rows < 1 yields no cells.
func Cells(img image.Image, cols, rows int) [][]Cell {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 || cols < 1 || rows < 1 {
		return nil
	}
	// Scale so that w*s ≤ cols and h*s ≤ 2*rows, s ≤ 1.
	s := 1.0
	if float64(cols)/float64(w) < s {
		s = float64(cols) / float64(w)
	}
	if float64(2*rows)/float64(h) < s {
		s = float64(2*rows) / float64(h)
	}
	cw := int(float64(w)*s + 0.5)
	ch := int(float64(h)*s+0.5) / 2
	if cw < 1 {
		cw = 1
	}
	if ch < 1 {
		ch = 1
	}
	// Each cell covers the source box [x0,x1) × [y0,y1) for its top half and
	// the box beneath for its bottom half; pixel rows are split in 2*ch bands.
	out := make([][]Cell, ch)
	for cy := 0; cy < ch; cy++ {
		row := make([]Cell, cw)
		for cx := 0; cx < cw; cx++ {
			x0, x1 := band(cx, cw, w), band(cx+1, cw, w)
			y0, y1 := band(2*cy, 2*ch, h), band(2*cy+1, 2*ch, h)
			y2 := band(2*cy+2, 2*ch, h)
			row[cx] = Cell{
				Top:    average(img, b.Min.X+x0, b.Min.X+x1, b.Min.Y+y0, b.Min.Y+y1),
				Bottom: average(img, b.Min.X+x0, b.Min.X+x1, b.Min.Y+y1, b.Min.Y+y2),
			}
		}
		out[cy] = row
	}
	return out
}

// band maps index i of n bands onto a pixel edge in [0, size]: the
// half-open box of band i is [band(i), band(i+1)), never empty.
func band(i, n, size int) int {
	if i >= n {
		return size
	}
	e := i * size / n
	if e >= size {
		e = size - 1
	}
	return e
}

// average is the mean colour of the box, alpha composited over black; an
// empty box (a band narrower than a pixel) samples its first pixel.
func average(img image.Image, x0, x1, y0, y1 int) color.RGBA {
	if x1 <= x0 {
		x1 = x0 + 1
	}
	if y1 <= y0 {
		y1 = y0 + 1
	}
	var r, g, b, n uint64
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			pr, pg, pb, pa := img.At(x, y).RGBA() // 16-bit, premultiplied
			_ = pa
			r += uint64(pr >> 8)
			g += uint64(pg >> 8)
			b += uint64(pb >> 8)
			n++
		}
	}
	if n == 0 {
		return color.RGBA{A: 255}
	}
	return color.RGBA{R: uint8(r / n), G: uint8(g / n), B: uint8(b / n), A: 255}
}

// ramp is the glyph ladder for a colourless terminal, darkest first.
const ramp = " .:-=+*#%@"

// Ramp renders cells as plain glyphs by luminance (a cell's two halves
// averaged), one string per row, for a terminal that cannot show colour.
func Ramp(cells [][]Cell) []string {
	out := make([]string, len(cells))
	r := []rune(ramp)
	for y, row := range cells {
		buf := make([]rune, len(row))
		for x, c := range row {
			l := (luma(c.Top) + luma(c.Bottom)) / 2
			i := int(l * float64(len(r)-1) / 255)
			buf[x] = r[i]
		}
		out[y] = string(buf)
	}
	return out
}

// luma is a colour's perceived brightness, 0..255.
func luma(c color.RGBA) float64 {
	return 0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B)
}

// Shrink returns img box-averaged down so that it fits maxW × maxH pixels
// (aspect kept), or img itself when it already does. A decoded photo is
// millions of pixels; the cells are re-fitted from the WORKING copy on every
// resize, so shrinking once at load keeps each fit cheap.
func Shrink(img image.Image, maxW, maxH int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxW && h <= maxH || w <= 0 || h <= 0 || maxW < 1 || maxH < 1 {
		return img
	}
	s := float64(maxW) / float64(w)
	if float64(maxH)/float64(h) < s {
		s = float64(maxH) / float64(h)
	}
	nw, nh := int(float64(w)*s+0.5), int(float64(h)*s+0.5)
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	out := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		y0, y1 := band(y, nh, h), band(y+1, nh, h)
		for x := 0; x < nw; x++ {
			x0, x1 := band(x, nw, w), band(x+1, nw, w)
			out.SetRGBA(x, y, average(img, b.Min.X+x0, b.Min.X+x1, b.Min.Y+y0, b.Min.Y+y1))
		}
	}
	return out
}
