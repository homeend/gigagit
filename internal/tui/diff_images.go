package tui

import (
	"fmt"
	"image"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/termimg"
)

// imgLayout is how the diff view lays out an image pair (ctrl+w cycles;
// the choice persists for the session in Model.diffImgLayout).
type imgLayout int

const (
	imgSideBySide imgLayout = iota // old left, new right, half the width each
	imgStacked                     // old above new, the full width each
	imgSingle                      // one image at the full pane; tab flips old/new
)

// setImages takes a binary verdict's decoded sides into the view: the
// images and one info line each ("old: png 640×480, 12.3 KB"). Nothing to
// do when neither side decoded — the view stays the binary placeholder.
func (v *diffView) setImages(out domain.Diff) {
	v.imgOld, v.imgNew = out.OldImg, out.NewImg
	v.imgKey, v.imgLines = "", nil
	if out.OldImg != nil {
		v.imgOldInfo = i18n.T("%s %d×%d, %s", out.OldKind, out.OldDim.X, out.OldDim.Y, fmtBytes(out.OldBytes))
	}
	if out.NewImg != nil {
		v.imgNewInfo = i18n.T("%s %d×%d, %s", out.NewKind, out.NewDim.X, out.NewDim.Y, fmtBytes(out.NewBytes))
	}
}

// twoSided reports a pair with both images: only then do "old:"/"new:"
// markers and the layout keys mean anything.
func (v *diffView) twoSided() bool { return v.imgOld != nil && v.imgNew != nil }

// sideInfo is a side's info line: marked old/new in a two-sided pair, bare
// when the one image needs no telling apart.
func (v *diffView) sideInfo(old bool) string {
	switch {
	case !v.twoSided() && old:
		return v.imgOldInfo
	case !v.twoSided():
		return v.imgNewInfo
	case old:
		return i18n.T("old: %s", v.imgOldInfo)
	}
	return i18n.T("new: %s", v.imgNewInfo)
}

// hasImages reports an image diff: a binary pair with at least one side
// decoded. An added or deleted image has one side.
func (v *diffView) hasImages() bool {
	return v.binary && (v.imgOld != nil || v.imgNew != nil)
}

// imageLines lays the pair out in a w × h body under the current layout,
// cached until the layout, the box or the shown side changes. A one-sided
// pair takes the single layout whatever is chosen: there is nothing to put
// beside or beneath it.
func (v *diffView) imageLines(w, h int) []string {
	key := fmt.Sprintf("%d:%d:%d:%v", v.imgLayout, w, h, v.imgShowOld)
	if key == v.imgKey && v.imgLines != nil {
		return v.imgLines
	}
	if w < 1 {
		w = 1
	}
	if h < 2 {
		h = 2
	}
	var lines []string
	switch {
	case !v.twoSided() || v.imgLayout == imgSingle:
		img, info := v.imgNew, v.sideInfo(false)
		if v.imgNew == nil || (v.imgShowOld && v.imgOld != nil) {
			img, info = v.imgOld, v.sideInfo(true)
		}
		lines = append([]string{info}, paintImageRows(termimg.Cells(img, w, h-1))...)
	case v.imgLayout == imgStacked:
		top := h / 2 // the old image's share, its info line included
		lines = append([]string{v.sideInfo(true)}, paintImageRows(termimg.Cells(v.imgOld, w, top-1))...)
		for len(lines) < top {
			lines = append(lines, "")
		}
		lines = append(lines, v.sideInfo(false))
		lines = append(lines, paintImageRows(termimg.Cells(v.imgNew, w, h-top-1))...)
	default: // side by side
		colW := (w - 1) / 2
		if colW < 1 {
			colW = 1
		}
		left := append([]string{truncate(v.sideInfo(true), colW)}, paintImageRows(termimg.Cells(v.imgOld, colW, h-1))...)
		right := append([]string{truncate(v.sideInfo(false), colW)}, paintImageRows(termimg.Cells(v.imgNew, colW, h-1))...)
		n := max(len(left), len(right))
		for i := 0; i < n; i++ {
			l, r := "", ""
			if i < len(left) {
				l = left[i]
			}
			if i < len(right) {
				r = right[i]
			}
			lines = append(lines, padRight(l, colW)+" "+r)
		}
	}
	for i, l := range lines {
		lines[i] = truncate(l, w)
	}
	v.imgKey, v.imgLines = key, lines
	return lines
}

// stackImageRows caps a stacked image file's thumbnail, its info line
// included: the stack is a list of files, not an image viewer.
const stackImageRows = 12

// stackImageRowCount is how many stream lines a stacked image file takes:
// the info line plus its taller side at two pixels a row (a thumbnail is
// never scaled up), within stackImageRows. Known before the width is — a
// narrow pane only leaves some of the rows blank.
func (v *diffView) stackImageRowCount() int {
	rows := 0
	for _, img := range []image.Image{v.imgOld, v.imgNew} {
		if img != nil {
			rows = max(rows, (img.Bounds().Dy()+1)/2)
		}
	}
	return min(1+rows, stackImageRows)
}

// stackImageLines is a stacked image file's thumbnail: the pair side by side
// (one image alone when one-sided) in stackImageRowCount rows, whatever
// layout the single-file view last used — the stack has no layout keys.
func (v *diffView) stackImageLines(w int) []string {
	layout, showOld := v.imgLayout, v.imgShowOld
	v.imgLayout, v.imgShowOld = imgSideBySide, false
	defer func() { v.imgLayout, v.imgShowOld = layout, showOld }()
	return v.imageLines(w, v.stackImageRowCount())
}

// paintImageRows renders cell rows as painted ▀ lines — or, on a terminal
// with no colour, as the luminance glyph ramp.
func paintImageRows(cells [][]termimg.Cell) []string {
	if lipgloss.ColorProfile() == termenv.Ascii {
		return termimg.Ramp(cells)
	}
	out := make([]string, len(cells))
	for i, row := range cells {
		out[i] = imageRowDecorator(row)(strings.Repeat("▀", len(row)), 0, 0)
	}
	return out
}

// imageHint is the hint line under an image pair: the layout in force and
// its keys.
func (v *diffView) imageHint() string {
	if !v.twoSided() {
		return i18n.T("  image  [esc] back") // one image: no layout to cycle, nothing to flip
	}
	switch v.imgLayout {
	case imgStacked:
		return i18n.T("  image: stacked  [ctrl+w] layout  [esc] back")
	case imgSingle:
		if v.imgShowOld {
			return i18n.T("  image: one at a time (old)  [ctrl+w] layout  [tab] old/new  [esc] back")
		}
		return i18n.T("  image: one at a time (new)  [ctrl+w] layout  [tab] old/new  [esc] back")
	}
	return i18n.T("  image: side by side  [ctrl+w] layout  [esc] back")
}
