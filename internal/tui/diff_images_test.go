package tui

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/homeend/gigagit/internal/domain"
)

func flat(w, h int, c color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

func imagePairView() *diffView {
	v := &diffView{title: "shot.png", binary: true}
	applyDiff(v, domain.Diff{Binary: true, OldImg: flat(40, 20, color.RGBA{255, 0, 0, 255}), OldKind: "png", OldDim: image.Point{40, 20}, NewImg: flat(60, 30, color.RGBA{0, 0, 255, 255}), NewKind: "png", NewDim: image.Point{60, 30}}, 20)
	return v
}

func widths(lines []string) (max int) {
	for _, l := range lines {
		if w := ansi.StringWidth(l); w > max {
			max = w
		}
	}
	return max
}

func TestDiffImageSideBySideSplitsTheWidth(t *testing.T) {
	t.Parallel()
	v := imagePairView()
	v.imgLayout = imgSideBySide
	lines := v.imageLines(81, 20)
	if len(lines) > 20 || widths(lines) > 81 {
		t.Fatalf("%d lines, widest %d: must fit 81×20", len(lines), widths(lines))
	}
	head := ansi.Strip(lines[0])
	if !strings.Contains(head, "old: png 40×20") || !strings.Contains(head, "new: png 60×30") {
		t.Fatalf("first line should name both sides: %q", head)
	}
	// Each side gets 40 columns: the 60 px new image scales to 40 cells wide.
	if body := ansi.Strip(lines[1]); len([]rune(body)) < 41+40 {
		t.Fatalf("row 1 should carry both thumbnails side by side: %q", body)
	}
}

func TestDiffImageStackedUsesTheFullWidth(t *testing.T) {
	t.Parallel()
	v := imagePairView()
	v.imgLayout = imgStacked
	lines := v.imageLines(81, 24)
	if len(lines) > 24 || widths(lines) > 81 {
		t.Fatalf("%d lines, widest %d: must fit 81×24", len(lines), widths(lines))
	}
	old, new := -1, -1
	for i, l := range lines {
		s := ansi.Strip(l)
		if strings.HasPrefix(s, "old: ") {
			old = i
		}
		if strings.HasPrefix(s, "new: ") {
			new = i
		}
	}
	if old != 0 || new <= old+1 || new > 12 {
		t.Fatalf("old info at %d, new at %d: want old on top, new in the lower half", old, new)
	}
	// 11 rows = 22 px tall for the 60×30 image: the height binds, 44 cells —
	// still wider than the 40 a side-by-side column allows.
	if w := len([]rune(ansi.Strip(lines[new+1]))); w != 44 {
		t.Fatalf("the new image should use the full width's worth of its 11 rows, got %d cells", w)
	}
}

func TestDiffImageOneAtATimeFlips(t *testing.T) {
	t.Parallel()
	v := imagePairView()
	v.imgLayout = imgSingle
	if head := ansi.Strip(v.imageLines(81, 20)[0]); !strings.HasPrefix(head, "new: ") {
		t.Fatalf("one at a time opens on the new side: %q", head)
	}
	v.imgShowOld = true
	if head := ansi.Strip(v.imageLines(81, 20)[0]); !strings.HasPrefix(head, "old: ") {
		t.Fatalf("flipped: %q", head)
	}
}

func TestDiffImageOneSidedPairShowsThatSide(t *testing.T) {
	t.Parallel()
	v := &diffView{title: "new.png", binary: true}
	applyDiff(v, domain.Diff{Binary: true, NewImg: flat(10, 10, color.RGBA{A: 255}), NewKind: "png", NewDim: image.Point{10, 10}}, 20)
	for _, layout := range []imgLayout{imgSideBySide, imgStacked, imgSingle} {
		v.imgLayout = layout
		lines := v.imageLines(81, 20)
		head := ansi.Strip(lines[0])
		if !strings.HasPrefix(head, "png 10×10") || strings.Contains(head, "new:") {
			t.Fatalf("layout %d: %q — one side needs no old/new marker", layout, head)
		}
		if hint := v.imageHint(); strings.Contains(hint, "[ctrl+w]") || strings.Contains(hint, "[tab]") {
			t.Fatalf("layout %d: hint %q offers keys that do nothing on one image", layout, hint)
		}
		if len(lines) < 6 {
			t.Fatalf("layout %d: the one image should fill the pane, got %d lines", layout, len(lines))
		}
	}
	if !v.hasImages() {
		t.Fatal("a one-sided image pair is still an image diff")
	}
	plain := &diffView{}
	applyDiff(plain, domain.Diff{Binary: true}, 20)
	if plain.hasImages() {
		t.Fatal("a binary with no decodable side is not an image diff")
	}
}

func TestDiffCtrlWCyclesImageLayoutsAndTabFlips(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	v := imagePairView()
	v.width = 100
	m = m.pushLayer(v)
	seq := []imgLayout{}
	for i := 0; i < 3; i++ {
		mm, _ := v.update(m, keyMsg("ctrl+w"))
		m = mm
		seq = append(seq, v.imgLayout)
	}
	if seq[0] != imgStacked || seq[1] != imgSingle || seq[2] != imgSideBySide {
		t.Fatalf("ctrl+w cycled %v, want stacked → one at a time → side by side", seq)
	}
	if m.diffImgLayout != imgSideBySide {
		t.Fatalf("the layout must persist for the session: %v", m.diffImgLayout)
	}
	mm, _ := v.update(m, keyMsg("ctrl+w"))
	m = mm
	mm, _ = v.update(m, keyMsg("ctrl+w")) // → one at a time
	m = mm
	mm, _ = v.update(m, keyMsg("tab"))
	m = mm
	if !v.imgShowOld {
		t.Fatal("tab in one-at-a-time flips to the old side")
	}
	if v.long != longScroll {
		t.Fatalf("the text mode must not move on an image diff: %v", v.long)
	}
	frame := ansi.Strip(v.render(m, ""))
	if !strings.Contains(frame, "old: png 40×20") || !strings.Contains(frame, "[tab]") {
		t.Fatalf("the frame should show the old side and the flip hint:\n%s", frame)
	}
}

func TestTabHintOnlyWhereTabDoesSomething(t *testing.T) {
	t.Parallel()
	v := imagePairView()
	for _, layout := range []imgLayout{imgSideBySide, imgStacked} {
		v.imgLayout = layout
		if hint := v.imageHint(); strings.Contains(hint, "[tab]") || !strings.Contains(hint, "[ctrl+w]") {
			t.Fatalf("layout %d: %q — both images are on screen, tab flips nothing", layout, hint)
		}
	}
	v.imgLayout = imgSingle
	if hint := v.imageHint(); !strings.Contains(hint, "[tab]") {
		t.Fatalf("one at a time: %q should offer tab", hint)
	}
}
