package tui

import (
	"fmt"
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
		v.imgOldInfo = i18n.T("old: %s %d×%d, %s", out.OldKind, out.OldDim.X, out.OldDim.Y, fmtBytes(out.OldBytes))
	}
	if out.NewImg != nil {
		v.imgNewInfo = i18n.T("new: %s %d×%d, %s", out.NewKind, out.NewDim.X, out.NewDim.Y, fmtBytes(out.NewBytes))
	}
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
	case v.imgOld == nil || v.imgNew == nil || v.imgLayout == imgSingle:
		img, info := v.imgNew, v.imgNewInfo
		if v.imgNew == nil || (v.imgShowOld && v.imgOld != nil) {
			img, info = v.imgOld, v.imgOldInfo
		}
		lines = append([]string{info}, paintImageRows(termimg.Cells(img, w, h-1))...)
	case v.imgLayout == imgStacked:
		top := h / 2 // the old image's share, its info line included
		lines = append([]string{v.imgOldInfo}, paintImageRows(termimg.Cells(v.imgOld, w, top-1))...)
		for len(lines) < top {
			lines = append(lines, "")
		}
		lines = append(lines, v.imgNewInfo)
		lines = append(lines, paintImageRows(termimg.Cells(v.imgNew, w, h-top-1))...)
	default: // side by side
		colW := (w - 1) / 2
		if colW < 1 {
			colW = 1
		}
		left := append([]string{truncate(v.imgOldInfo, colW)}, paintImageRows(termimg.Cells(v.imgOld, colW, h-1))...)
		right := append([]string{truncate(v.imgNewInfo, colW)}, paintImageRows(termimg.Cells(v.imgNew, colW, h-1))...)
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
	layout := i18n.T("side by side")
	switch {
	case v.imgOld == nil || v.imgNew == nil:
		layout = i18n.T("one side")
	case v.imgLayout == imgStacked:
		layout = i18n.T("stacked")
	case v.imgLayout == imgSingle && v.imgShowOld:
		layout = i18n.T("one at a time (old)")
	case v.imgLayout == imgSingle:
		layout = i18n.T("one at a time (new)")
	}
	return i18n.T("  image: %s  [ctrl+w] layout  [tab] old/new  [esc] back", layout)
}
