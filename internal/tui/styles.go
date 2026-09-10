package tui

import (
	"sync/atomic"

	"github.com/charmbracelet/lipgloss"
	"github.com/homeend/gigagit/internal/syntax"
	"github.com/homeend/gigagit/internal/theme"
)

// styles is every themed lipgloss style the TUI renders with. It is built
// once per theme by buildStyles and read through st(); the active pointer is
// swapped atomically by setTheme so a Settings switch races with nothing
// (TUI tests render in parallel — reassigning package vars would be a data
// race under ./test.sh race).
//
// Field names keep the pre-theme package-var names so call sites read as
// st().focusedPanel where they used to read focusedPanel.
type styles struct {
	th theme.Theme

	// view.go
	titleStyle   lipgloss.Style
	focusedPanel lipgloss.Style
	bluredPanel  lipgloss.Style
	selectedRow  lipgloss.Style
	modalStyle   lipgloss.Style
	statusErr    lipgloss.Style // was statusErrStyle
	errorText    lipgloss.Style // was errorStyle
	reviewHot    lipgloss.Style // was reviewHotStyle
	reviewDim    lipgloss.Style // was reviewDimStyle

	// shared dim tier (was many "240" literals). muted/bright have no style
	// field of their own — buildStyles keeps them as local lipgloss.Colors
	// feeding noteBody/diffEmph/diffCursorNo/noteSummary directly.
	dim lipgloss.Style // dimIdentStyle, dimRowStyle, pickerDim, conflictSrcStyle, diffGapCell, diffGutter, diffFold, noteFrameStale, noteDim, unsetStyle, langHintStyle

	// commit_ident.go
	tagDeco lipgloss.Style

	// conflict_picker.go
	pickerLabel lipgloss.Style

	// content_popup.go
	messageBlock lipgloss.Style
	errModal     lipgloss.Style // modalStyle.BorderForeground(ErrFg)

	// diff_render.go
	diffDelCell    lipgloss.Style
	diffAddCell    lipgloss.Style
	diffGapCell    lipgloss.Style
	diffGutter     lipgloss.Style
	diffFold       lipgloss.Style
	diffEmph       lipgloss.Style
	diffCursorRow  lipgloss.Style
	diffCursorNo   lipgloss.Style
	diffAddCursor  lipgloss.Style
	diffDelCursor  lipgloss.Style
	diffGapCursor  lipgloss.Style
	noteFrameUser  lipgloss.Style
	noteFrameAgent lipgloss.Style
	noteFrameStale lipgloss.Style
	noteSummary    lipgloss.Style
	noteBody       lipgloss.Style
	noteDim        lipgloss.Style

	attnInfo  lipgloss.Style
	attnWarn  lipgloss.Style
	attnError lipgloss.Style

	// field_style.go
	field       lipgloss.Style // was fieldStyle
	fieldCursor lipgloss.Style // was fieldCursorStyle

	// notify.go
	noticeHot lipgloss.Style
	noticeDim lipgloss.Style

	// save_content.go
	saveBanner lipgloss.Style

	// tooltip.go
	tooltip lipgloss.Style

	lanes  [7]lipgloss.Color
	syntax [11]string
}

// legacy is the pre-theme literal for each role: what `terminal` renders.
var legacy = theme.Theme{
	Name: theme.NameTerminal,
	Dim:  "240", Muted: "250", Bright: "231",
	FocusBorder: "12", ModalBorder: "11", TooltipFg: "0", TooltipBg: "11",
	ErrFg: "9", ErrBg: "1", TagDeco: "220", StatusErrFg: "15",
	DiffAddBg: "22", DiffDelBg: "52", DiffAddCursorBg: "28", DiffDelCursorBg: "88",
	CursorRowBg: "237", FieldBg: "236", FieldCursorFg: "236", FieldCursorBg: "250",
	MessageBlockBg: "236", SaveBannerFg: "15", SaveBannerBg: "22",
	NoticeHot: "196", NoticeDim: "124", ReviewHot: "39", ReviewDim: "31",
	NoteUser: "75", NoteAgent: "141", NoteStale: "240", PickerLabel: "245",
	AttentionInfo: "24", AttentionWarn: "94", AttentionError: "89",
	Lanes:  [7]string{"33", "208", "40", "201", "51", "220", "129"},
	Syntax: [11]string{"", "141", "79", "222", "", "150", "215", "245", "252", "250", "180"},
}

// pick returns v, or the legacy literal when the theme leaves the role "".
func pick(v, fallback string) lipgloss.Color {
	if v == "" {
		return lipgloss.Color(fallback)
	}
	return lipgloss.Color(v)
}

// buildStyles materialises a theme. Bg/Fg are NOT applied to styles: they are
// painted under the whole frame by paintFrame (see frame()).
func buildStyles(th theme.Theme) *styles {
	s := &styles{th: th}
	dim := pick(th.Dim, legacy.Dim)
	muted := pick(th.Muted, legacy.Muted)
	bright := pick(th.Bright, legacy.Bright)
	ns := lipgloss.NewStyle

	s.titleStyle = ns().Bold(true)
	s.focusedPanel = ns().Border(lipgloss.RoundedBorder()).BorderForeground(pick(th.FocusBorder, legacy.FocusBorder)).Padding(0, 1)
	s.bluredPanel = ns().Border(lipgloss.RoundedBorder()).BorderForeground(dim).Padding(0, 1)
	s.selectedRow = ns().Reverse(true)
	s.modalStyle = ns().Border(lipgloss.DoubleBorder()).BorderForeground(pick(th.ModalBorder, legacy.ModalBorder)).Padding(1, 2)
	s.statusErr = ns().Bold(true).Foreground(pick(th.StatusErrFg, legacy.StatusErrFg)).Background(pick(th.ErrBg, legacy.ErrBg))
	s.errorText = ns().Foreground(pick(th.ErrFg, legacy.ErrFg))
	s.reviewHot = ns().Foreground(pick(th.ReviewHot, legacy.ReviewHot)).Bold(true)
	s.reviewDim = ns().Foreground(pick(th.ReviewDim, legacy.ReviewDim))

	s.dim = ns().Foreground(dim)

	s.tagDeco = ns().Foreground(pick(th.TagDeco, legacy.TagDeco))
	s.pickerLabel = ns().Bold(true).Foreground(pick(th.PickerLabel, legacy.PickerLabel))
	s.messageBlock = ns().Background(pick(th.MessageBlockBg, legacy.MessageBlockBg))
	s.errModal = s.modalStyle.BorderForeground(pick(th.ErrFg, legacy.ErrFg))

	s.diffDelCell = ns().Background(pick(th.DiffDelBg, legacy.DiffDelBg))
	s.diffAddCell = ns().Background(pick(th.DiffAddBg, legacy.DiffAddBg))
	s.diffGapCell = ns().Foreground(dim)
	s.diffGutter = ns().Foreground(dim)
	s.diffFold = ns().Foreground(dim)
	s.diffEmph = ns().Bold(true).Foreground(bright)
	s.diffCursorRow = ns().Background(pick(th.CursorRowBg, legacy.CursorRowBg))
	s.diffCursorNo = ns().Bold(true).Foreground(bright)
	s.diffAddCursor = ns().Background(pick(th.DiffAddCursorBg, legacy.DiffAddCursorBg))
	s.diffDelCursor = ns().Background(pick(th.DiffDelCursorBg, legacy.DiffDelCursorBg))
	s.diffGapCursor = s.diffGapCell.Background(pick(th.CursorRowBg, legacy.CursorRowBg))
	s.noteFrameUser = ns().Foreground(pick(th.NoteUser, legacy.NoteUser))
	s.noteFrameAgent = ns().Foreground(pick(th.NoteAgent, legacy.NoteAgent))
	s.noteFrameStale = ns().Foreground(pick(th.NoteStale, legacy.NoteStale))
	s.noteSummary = ns().Bold(true).Foreground(bright)
	s.noteBody = ns().Foreground(muted)
	s.noteDim = ns().Foreground(dim)

	s.attnInfo = ns().Background(pick(th.AttentionInfo, legacy.AttentionInfo))
	s.attnWarn = ns().Background(pick(th.AttentionWarn, legacy.AttentionWarn))
	s.attnError = ns().Background(pick(th.AttentionError, legacy.AttentionError))

	s.field = ns().Background(pick(th.FieldBg, legacy.FieldBg))
	s.fieldCursor = ns().Background(pick(th.FieldCursorBg, legacy.FieldCursorBg)).Foreground(pick(th.FieldCursorFg, legacy.FieldCursorFg))

	s.noticeHot = ns().Foreground(pick(th.NoticeHot, legacy.NoticeHot)).Bold(true)
	s.noticeDim = ns().Foreground(pick(th.NoticeDim, legacy.NoticeDim))
	s.saveBanner = ns().Foreground(pick(th.SaveBannerFg, legacy.SaveBannerFg)).Background(pick(th.SaveBannerBg, legacy.SaveBannerBg))
	s.tooltip = ns().Foreground(pick(th.TooltipFg, legacy.TooltipFg)).Background(pick(th.TooltipBg, legacy.TooltipBg))

	for i := range s.lanes {
		s.lanes[i] = pick(th.Lanes[i], legacy.Lanes[i])
	}
	for i := range s.syntax {
		if th.Syntax[i] != "" {
			s.syntax[i] = th.Syntax[i]
		} else {
			s.syntax[i] = legacy.Syntax[i]
		}
	}
	return s
}

// lane is the graph lane colour (index by lane % 7, negative clamps to 0).
func (s *styles) lane(lane int) lipgloss.Color {
	if lane < 0 {
		lane = 0
	}
	return s.lanes[lane%len(s.lanes)]
}

// syntaxColor is the palette entry for c ("" for Plain / Name / unknown).
func (s *styles) syntaxColor(c syntax.Class) string {
	if int(c) < len(s.syntax) {
		return s.syntax[c]
	}
	return ""
}

// syntaxStyle returns base with c's foreground applied ("" leaves base alone,
// so an uncoloured class renders byte-identically to the pre-syntax path).
func (s *styles) syntaxStyle(base lipgloss.Style, c syntax.Class) lipgloss.Style {
	if col := s.syntaxColor(c); col != "" {
		return base.Foreground(lipgloss.Color(col))
	}
	return base
}

// frame returns the colours paintFrame lays under the whole screen; both ""
// for terminal (no painting).
func (s *styles) frame() (bg, fg lipgloss.Color) {
	return lipgloss.Color(s.th.Bg), lipgloss.Color(s.th.Fg)
}

var activeStyles atomic.Pointer[styles]

func init() { activeStyles.Store(buildStyles(theme.Terminal)) }

// st is the active style set. Never cache the pointer across a Settings
// switch; read it per render.
func st() *styles { return activeStyles.Load() }

// swatchStyle paints a COLOUR SAMPLE for the Settings colour editor: a filled
// cell for a background role, the glyph's own foreground otherwise. It lives
// here because styles.go is the one file allowed to name a lipgloss colour —
// every other file reads st(), and this is the single surface that must paint a
// colour the active theme does not itself carry (the one being typed).
func swatchStyle(colour string, bg bool) lipgloss.Style {
	if bg {
		return lipgloss.NewStyle().Background(lipgloss.Color(colour))
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(colour))
}

// setTheme swaps the active style set. Callers that own a screen must follow
// it with tea.ClearScreen so unchanged rows are repainted.
func setTheme(th theme.Theme) { activeStyles.Store(buildStyles(th)) }

// activeTheme reports the theme st() was built from.
func activeTheme() theme.Theme { return st().th }

// attnStyle maps a steering command's tone onto the attention band style.
// The tone is an agent-supplied protocol value, so the allowlist lives here:
// anything outside it paints nothing rather than defaulting to a colour the
// agent did not ask for.
func attnStyle(tone string) (lipgloss.Style, bool) {
	s := st()
	switch tone {
	case "info":
		return s.attnInfo, true
	case "warn":
		return s.attnWarn, true
	case "error":
		return s.attnError, true
	}
	return lipgloss.Style{}, false
}
