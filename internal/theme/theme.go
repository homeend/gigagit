// Package theme is gg's pure colour-role catalogue. A Theme names one colour
// per UI role; the TUI builds lipgloss styles from it (internal/tui/styles.go)
// and paints the frame's background from Bg/Fg. The zero Theme (Terminal)
// means "inherit": every role "" keeps the TUI's historical literal and no
// background is painted, so the terminal's own scheme shows through.
//
// Values are "#rrggbb" hex strings or 256-colour indexes ("240"); both are
// passed to lipgloss.Color unchanged. DAG leaf: no gg or lipgloss imports.
package theme

// Theme names. The config value is the English protocol string; only the
// Settings-row rendering is localized.
const (
	NameTerminal = "terminal"
	NameDark     = "dark"
	NameLight    = "light"
)

// Theme is one named colour scheme. Field groups mirror the spec's role table.
type Theme struct {
	Name string

	// Frame: painted under every cell when both are set.
	Bg, Fg string

	// Text tiers.
	Dim, Muted, Bright string

	// Chrome.
	FocusBorder, ModalBorder, TooltipFg, TooltipBg string
	ErrFg, ErrBg, TagDeco                          string
	StatusErrFg                                    string

	// Diff / editor surfaces.
	DiffAddBg, DiffDelBg, DiffAddCursorBg, DiffDelCursorBg string
	CursorRowBg, FieldBg, FieldCursorFg, FieldCursorBg     string
	MessageBlockBg, SaveBannerFg, SaveBannerBg             string

	// Signals.
	NoticeHot, NoticeDim, ReviewHot, ReviewDim  string
	NoteUser, NoteAgent, NoteStale, PickerLabel string

	// Palettes: index = graph lane % 7, and index = syntax.Class
	// (Plain, Keyword, Type, Func, Name, String, Number, Comment, Operator,
	// Punct, Attr). Syntax[Plain] and Syntax[Name] stay "" by design.
	Lanes  [7]string
	Syntax [11]string
}

// roles returns every scalar role in declaration order (tests + builders).
func (t Theme) roles() []string {
	return []string{
		t.Bg, t.Fg,
		t.Dim, t.Muted, t.Bright,
		t.FocusBorder, t.ModalBorder, t.TooltipFg, t.TooltipBg,
		t.ErrFg, t.ErrBg, t.TagDeco, t.StatusErrFg,
		t.DiffAddBg, t.DiffDelBg, t.DiffAddCursorBg, t.DiffDelCursorBg,
		t.CursorRowBg, t.FieldBg, t.FieldCursorFg, t.FieldCursorBg,
		t.MessageBlockBg, t.SaveBannerFg, t.SaveBannerBg,
		t.NoticeHot, t.NoticeDim, t.ReviewHot, t.ReviewDim,
		t.NoteUser, t.NoteAgent, t.NoteStale, t.PickerLabel,
	}
}

// Terminal is the inherit-everything theme (today's look).
var Terminal = Theme{Name: NameTerminal}

// Dark pins Windows Terminal's "Campbell" scheme for the basic ANSI slots gg
// uses and keeps the historical 256-cube accents (already identical
// everywhere). Source: learn.microsoft.com Windows Terminal color schemes.
var Dark = Theme{
	Name: NameDark,
	Bg:   "#0C0C0C", Fg: "#CCCCCC",
	Dim: "#585858", Muted: "#BCBCBC", Bright: "#FFFFFF",
	FocusBorder: "#3B78FF", ModalBorder: "#F9F1A5", TooltipFg: "#0C0C0C", TooltipBg: "#F9F1A5",
	ErrFg: "#E74856", ErrBg: "#C50F1F", TagDeco: "220", StatusErrFg: "#F2F2F2",
	DiffAddBg: "22", DiffDelBg: "52", DiffAddCursorBg: "28", DiffDelCursorBg: "88",
	CursorRowBg: "237", FieldBg: "236", FieldCursorFg: "236", FieldCursorBg: "250",
	MessageBlockBg: "236", SaveBannerFg: "#F2F2F2", SaveBannerBg: "22",
	NoticeHot: "196", NoticeDim: "124", ReviewHot: "39", ReviewDim: "31",
	NoteUser: "75", NoteAgent: "141", NoteStale: "240", PickerLabel: "245",
	Lanes:  [7]string{"33", "208", "40", "201", "51", "220", "129"},
	Syntax: [11]string{"", "141", "79", "222", "", "150", "215", "245", "252", "250", "180"},
}

// Light is Everforest "light soft" (bg0 #F3EAD3), the warm not-white tier.
// Source: github.com/sainnhe/everforest palette.md. Diff/err backgrounds are
// ~15% tints over bg0.
var Light = Theme{
	Name: NameLight,
	Bg:   "#F3EAD3", Fg: "#5C6A72",
	Dim: "#A6B0A0", Muted: "#829181", Bright: "#3A4A52",
	FocusBorder: "#3A94C5", ModalBorder: "#DFA000", TooltipFg: "#3A4A52", TooltipBg: "#F1E4C5",
	ErrFg: "#F85552", ErrBg: "#F1D1CF", TagDeco: "#DFA000", StatusErrFg: "#3A4A52",
	DiffAddBg: "#E1E4BD", DiffDelBg: "#F4D9D4", DiffAddCursorBg: "#CFDAA8", DiffDelCursorBg: "#EBC3BD",
	CursorRowBg: "#E5DFC5", FieldBg: "#E5DFC5", FieldCursorFg: "#F3EAD3", FieldCursorBg: "#5C6A72",
	MessageBlockBg: "#EAE4CA", SaveBannerFg: "#F3EAD3", SaveBannerBg: "#8DA101",
	NoticeHot: "#F85552", NoticeDim: "#B85450", ReviewHot: "#3A94C5", ReviewDim: "#35A77C",
	NoteUser: "#3A94C5", NoteAgent: "#DF69BA", NoteStale: "#A6B0A0", PickerLabel: "#829181",
	Lanes:  [7]string{"#3A94C5", "#F57D26", "#8DA101", "#DF69BA", "#35A77C", "#DFA000", "#F85552"},
	Syntax: [11]string{"", "#DF69BA", "#35A77C", "#DFA000", "", "#8DA101", "#F57D26", "#939F91", "#5C6A72", "#829181", "#B8860B"},
}

var builtins = []Theme{Terminal, Dark, Light}

// Names lists the built-in theme names in Settings-cycle order.
func Names() []string {
	out := make([]string, len(builtins))
	for i, t := range builtins {
		out[i] = t.Name
	}
	return out
}

// Lookup resolves a config value. "" and "terminal" are Terminal. Unknown
// names return ok=false; callers fall back to Terminal and tell the user.
func Lookup(name string) (Theme, bool) {
	if name == "" {
		return Terminal, true
	}
	for _, t := range builtins {
		if t.Name == name {
			return t, true
		}
	}
	return Terminal, false
}
