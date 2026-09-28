package tui

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/homeend/gigagit/internal/termimg"
)

func loadBytes(t *testing.T, path string, data []byte) fileContentMsg {
	t.Helper()
	msg := loadFileContentSrcCmd("t", path, false, func(context.Context) ([]byte, error) { return data, nil })().(fileContentMsg)
	return msg
}

func TestBinaryFileLoadsAsAPlaceholder(t *testing.T) {
	t.Parallel()
	data := append([]byte("GIF89a\x00\x01\x02"), bytes.Repeat([]byte{0x9b, 0xff, 0x00}, 2000)...)
	msg := loadBytes(t, "blob.bin", data)
	if len(msg.lines) != 1 || msg.lines[0].src || !strings.Contains(msg.lines[0].text, "binary") {
		t.Fatalf("lines = %+v, want one non-source placeholder naming a binary file", msg.lines)
	}
	if !strings.Contains(msg.lines[0].text, "5.9 KB") { // its size
		t.Fatalf("placeholder %q should carry the size", msg.lines[0].text)
	}
}

func TestImageFileLoadsAsAnImage(t *testing.T) {
	t.Parallel()
	img := image.NewRGBA(image.Rect(0, 0, 40, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 40; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 6), 0, uint8(y * 12), 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	msg := loadBytes(t, "shot.png", b.Bytes())
	if msg.img == nil {
		t.Fatal("a PNG must load as an image")
	}
	m := wtWindow(t, "shot.png")
	m.width, m.height = 120, 30
	d := newOpenFile(fileSource{kind: srcWorktree}, "shot.png")
	m.filesPreview = d
	d.fill(msg, 20, 50)
	view := ansi.Strip(m.View())
	// 40×20 px into the pane: one cell per pixel column, two pixel rows per
	// cell → 40 cols × 10 rows under the info line. In a colour terminal
	// the rows are ▀ cells painted by the decorator; the test's colourless
	// profile gets the luminance ramp, the same shape.
	if n := len(d.p.lines); n != 11 {
		t.Fatalf("image rows = %d, want the info line + 10", n-1)
	}
	for _, l := range d.p.lines[1:] {
		if w := len([]rune(l.text)); w != 40 {
			t.Fatalf("image row %q is %d cells wide, want 40", l.text, w)
		}
		if (l.cells != nil) != strings.Contains(l.text, "▀") {
			t.Fatalf("a ▀ row must carry its cells and a ramp row none: %+v", l)
		}
	}
	if !strings.Contains(view, "40×20") {
		t.Fatalf("the preview should say the image's size:\n%s", view)
	}
	for _, l := range strings.Split(view, "\n") {
		if ansi.StringWidth(l) > 120 {
			t.Fatalf("a frame line overflows the terminal: %q", l)
		}
	}
}

func TestSanitizeDropsC1AndBidiControls(t *testing.T) {
	t.Parallel()
	in := "a\u009bb‮c⁦d\u0085e"
	if got := sanitizeForDisplay(in); got != "abcde" {
		t.Fatalf("sanitizeForDisplay = %q, want abcde (C1 and bidi controls dropped)", got)
	}
	lines := fileContentLinesTok([]byte(in), nil)
	if lines[0].text != "abcde" || len(lines[0].raw) != len(in) {
		t.Fatalf("line = %+v", lines[0])
	}
}

func TestImageRowDecoratorPaintsOnlyTheCells(t *testing.T) {
	t.Parallel()
	cells := []termimg.Cell{{Top: color.RGBA{255, 0, 0, 255}, Bottom: color.RGBA{0, 0, 255, 255}}}
	deco := imageRowDecorator(cells)
	out := deco("▀▀  ", 0, 0)
	if !strings.HasSuffix(out, "▀  ") || !strings.HasPrefix(ansi.Strip(out), "▀▀") {
		t.Fatalf("decorated = %q: the first ▀ is painted, the second has no cell, the padding stays", out)
	}
	// A horizontal scroll offsets which cell the first visible ▀ is.
	if got := ansi.Strip(deco("▀", 1, 0)); got != "▀" {
		t.Fatalf("scrolled = %q", got)
	}
}

func TestImageRefitsToTheBox(t *testing.T) {
	t.Parallel()
	p := &contentPopup{img: image.NewRGBA(image.Rect(0, 0, 100, 100)), imgInfo: "info"}
	p.fitImage(50, 11)
	if len(p.lines) != 11 || len([]rune(p.lines[1].text)) != 20 {
		t.Fatalf("50×11 box: %d lines, first row %d cells (want 11 lines: info + 10 rows of 20 — 100 px tall into 20 px-rows)", len(p.lines), len([]rune(p.lines[1].text)))
	}
	p.fitImage(120, 40)
	if len(p.lines) != 1+39 || len([]rune(p.lines[1].text)) != 78 {
		t.Fatalf("120×40 box: %d lines, first row %d cells (want 40 lines, 78 cells: 100 px into 78 px-rows)", len(p.lines), len([]rune(p.lines[1].text)))
	}
}

func TestCursorBandSkipsImageRows(t *testing.T) {
	t.Parallel()
	p := &contentPopup{cur: 3}
	cells := []termimg.Cell{{}}
	if _, marked := previewRowMark(p, 3, false, contentLine{text: "▀", cells: cells}); marked {
		t.Fatal("an image row must not wear the cursor band: the decorator's per-cell colours would cancel it after the first cell")
	}
	if _, marked := previewRowMark(p, 3, false, contentLine{text: "info"}); !marked {
		t.Fatal("a text row under the cursor keeps the band")
	}
	p.lsel.on, p.lsel.anchor = true, 3
	if _, marked := previewRowMark(p, 3, false, contentLine{text: "▀", cells: cells}); marked {
		t.Fatal("nor the selection stripe")
	}
}
