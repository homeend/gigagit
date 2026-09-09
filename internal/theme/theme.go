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

// Light is a neutral light-grey scheme: charcoal text on an off-white grey
// ground (bg0 #E9E9E5), modelled on a plain charcoal-on-off-white nvim look.
// It replaced an Everforest "light soft" first cut, which the user found too
// colourful (cream ground) and too light (text contrast). Accents stay muted
// and desaturated so the grey frame carries the look; diff/err backgrounds are
// low-saturation tints over bg0, cursor tints one step darker.
var Light = Theme{
	Name: NameLight,
	Bg:   "#E9E9E5", Fg: "#33393F",
	Dim: "#8A8F8A", Muted: "#5F6570", Bright: "#1F2428",
	FocusBorder: "#2F6FB8", ModalBorder: "#B08000", TooltipFg: "#33393F", TooltipBg: "#F1E7B5",
	ErrFg: "#C0392B", ErrBg: "#F1D5D2", TagDeco: "#B08000", StatusErrFg: "#33393F",
	DiffAddBg: "#D8EAD0", DiffDelBg: "#F1D5D2", DiffAddCursorBg: "#C5DDB9", DiffDelCursorBg: "#E6C2BE",
	CursorRowBg: "#DADAD5", FieldBg: "#DADAD5", FieldCursorFg: "#E9E9E5", FieldCursorBg: "#33393F",
	MessageBlockBg: "#DFDFDA", SaveBannerFg: "#E9E9E5", SaveBannerBg: "#3E8E41",
	NoticeHot: "#C0392B", NoticeDim: "#A0564C", ReviewHot: "#2F6FB8", ReviewDim: "#2A8C8C",
	NoteUser: "#2F6FB8", NoteAgent: "#6B4FBB", NoteStale: "#8A8F8A", PickerLabel: "#5F6570",
	Lanes:  [7]string{"#2F6FB8", "#C7641B", "#3E8E41", "#6B4FBB", "#2A8C8C", "#B08000", "#C0392B"},
	Syntax: [11]string{"", "#6B4FBB", "#2A8C8C", "#B08000", "", "#3E8E41", "#C7641B", "#8A8F8A", "#33393F", "#5F6570", "#A0682A"},
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
