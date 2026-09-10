package theme

import "fmt"

// Override is one theme's user-configurable roles: every field mirrors Theme,
// TOML-tagged in snake_case so config can decode a [themes.<name>] table.
// An empty string (or a nil slice) means "not set" — the built-in value
// stands. There is deliberately no way to unset a role back to "inherit the
// terminal": that is what the terminal theme is for.
type Override struct {
	Bg string `toml:"bg"`
	Fg string `toml:"fg"`

	Dim    string `toml:"dim"`
	Muted  string `toml:"muted"`
	Bright string `toml:"bright"`

	FocusBorder string `toml:"focus_border"`
	ModalBorder string `toml:"modal_border"`
	TooltipFg   string `toml:"tooltip_fg"`
	TooltipBg   string `toml:"tooltip_bg"`

	ErrFg       string `toml:"err_fg"`
	ErrBg       string `toml:"err_bg"`
	StatusErrFg string `toml:"status_err_fg"`
	TagDeco     string `toml:"tag_deco"`

	DiffAddBg       string `toml:"diff_add_bg"`
	DiffDelBg       string `toml:"diff_del_bg"`
	DiffAddCursorBg string `toml:"diff_add_cursor_bg"`
	DiffDelCursorBg string `toml:"diff_del_cursor_bg"`

	CursorRowBg   string `toml:"cursor_row_bg"`
	FieldBg       string `toml:"field_bg"`
	FieldCursorFg string `toml:"field_cursor_fg"`
	FieldCursorBg string `toml:"field_cursor_bg"`

	MessageBlockBg string `toml:"message_block_bg"`
	SaveBannerFg   string `toml:"save_banner_fg"`
	SaveBannerBg   string `toml:"save_banner_bg"`

	NoticeHot string `toml:"notice_hot"`
	NoticeDim string `toml:"notice_dim"`
	ReviewHot string `toml:"review_hot"`
	ReviewDim string `toml:"review_dim"`

	NoteUser    string `toml:"note_user"`
	NoteAgent   string `toml:"note_agent"`
	NoteStale   string `toml:"note_stale"`
	PickerLabel string `toml:"picker_label"`

	AttentionInfo  string `toml:"attention_info_bg"`
	AttentionWarn  string `toml:"attention_warn_bg"`
	AttentionError string `toml:"attention_error_bg"`

	Lanes  []string `toml:"lanes"`  // exactly 7 when set; "" entry = keep the base value
	Syntax []string `toml:"syntax"` // exactly 11 when set; "" entry = keep the base value
}

// roleField is one row of the ordered accessor table below: the TOML key, a
// one-line description for `gg config populate`, and pointer accessors into a
// Theme and an Override. Overlay, Merge, AsOverride and RoleDocs all loop over
// this table, so ADDING A ROLE MEANS ADDING A ROW HERE — TestRoleFieldsCover-
// EveryRole (override_test.go) fails until you do.
type roleField struct {
	key  string
	doc  string
	get  func(*Theme) *string
	getO func(*Override) *string
}

var roleFields = []roleField{
	{"bg", "frame background", func(t *Theme) *string { return &t.Bg }, func(o *Override) *string { return &o.Bg }},
	{"fg", "frame foreground (default text)", func(t *Theme) *string { return &t.Fg }, func(o *Override) *string { return &o.Fg }},

	{"dim", "dim tier: unfocused borders, gutters, hints, stale notes", func(t *Theme) *string { return &t.Dim }, func(o *Override) *string { return &o.Dim }},
	{"muted", "muted tier: note bodies and other secondary text", func(t *Theme) *string { return &t.Muted }, func(o *Override) *string { return &o.Muted }},
	{"bright", "bright tier: emphasized diff text, note summaries", func(t *Theme) *string { return &t.Bright }, func(o *Override) *string { return &o.Bright }},

	{"focus_border", "border of the focused panel", func(t *Theme) *string { return &t.FocusBorder }, func(o *Override) *string { return &o.FocusBorder }},
	{"modal_border", "border of modals and popups", func(t *Theme) *string { return &t.ModalBorder }, func(o *Override) *string { return &o.ModalBorder }},
	{"tooltip_fg", "tooltip text", func(t *Theme) *string { return &t.TooltipFg }, func(o *Override) *string { return &o.TooltipFg }},
	{"tooltip_bg", "tooltip background", func(t *Theme) *string { return &t.TooltipBg }, func(o *Override) *string { return &o.TooltipBg }},

	{"err_fg", "error text and the error modal's border", func(t *Theme) *string { return &t.ErrFg }, func(o *Override) *string { return &o.ErrFg }},
	{"err_bg", "error status-bar background", func(t *Theme) *string { return &t.ErrBg }, func(o *Override) *string { return &o.ErrBg }},
	{"status_err_fg", "error status-bar text (must contrast with err_bg)", func(t *Theme) *string { return &t.StatusErrFg }, func(o *Override) *string { return &o.StatusErrFg }},
	{"tag_deco", "tag decorations next to commits", func(t *Theme) *string { return &t.TagDeco }, func(o *Override) *string { return &o.TagDeco }},

	{"diff_add_bg", "added-line background in diffs", func(t *Theme) *string { return &t.DiffAddBg }, func(o *Override) *string { return &o.DiffAddBg }},
	{"diff_del_bg", "removed-line background in diffs", func(t *Theme) *string { return &t.DiffDelBg }, func(o *Override) *string { return &o.DiffDelBg }},
	{"diff_add_cursor_bg", "added line under the diff line cursor", func(t *Theme) *string { return &t.DiffAddCursorBg }, func(o *Override) *string { return &o.DiffAddCursorBg }},
	{"diff_del_cursor_bg", "removed line under the diff line cursor", func(t *Theme) *string { return &t.DiffDelCursorBg }, func(o *Override) *string { return &o.DiffDelCursorBg }},

	{"cursor_row_bg", "diff line-cursor row background", func(t *Theme) *string { return &t.CursorRowBg }, func(o *Override) *string { return &o.CursorRowBg }},
	{"field_bg", "text-input field background", func(t *Theme) *string { return &t.FieldBg }, func(o *Override) *string { return &o.FieldBg }},
	{"field_cursor_fg", "text-input cursor foreground", func(t *Theme) *string { return &t.FieldCursorFg }, func(o *Override) *string { return &o.FieldCursorFg }},
	{"field_cursor_bg", "text-input cursor background", func(t *Theme) *string { return &t.FieldCursorBg }, func(o *Override) *string { return &o.FieldCursorBg }},

	{"message_block_bg", "commit-message block background in popups", func(t *Theme) *string { return &t.MessageBlockBg }, func(o *Override) *string { return &o.MessageBlockBg }},
	{"save_banner_fg", "save-to-file banner text", func(t *Theme) *string { return &t.SaveBannerFg }, func(o *Override) *string { return &o.SaveBannerFg }},
	{"save_banner_bg", "save-to-file banner background", func(t *Theme) *string { return &t.SaveBannerBg }, func(o *Override) *string { return &o.SaveBannerBg }},

	{"notice_hot", "notification-center badge while it blinks", func(t *Theme) *string { return &t.NoticeHot }, func(o *Override) *string { return &o.NoticeHot }},
	{"notice_dim", "notification-center badge at rest", func(t *Theme) *string { return &t.NoticeDim }, func(o *Override) *string { return &o.NoticeDim }},
	{"review_hot", "AI review/capture indicator while running", func(t *Theme) *string { return &t.ReviewHot }, func(o *Override) *string { return &o.ReviewHot }},
	{"review_dim", "AI review/capture indicator at rest", func(t *Theme) *string { return &t.ReviewDim }, func(o *Override) *string { return &o.ReviewDim }},

	{"note_user", "review-note frame, your own notes", func(t *Theme) *string { return &t.NoteUser }, func(o *Override) *string { return &o.NoteUser }},
	{"note_agent", "review-note frame, agent-written notes", func(t *Theme) *string { return &t.NoteAgent }, func(o *Override) *string { return &o.NoteAgent }},
	{"note_stale", "review-note frame, notes whose anchor moved", func(t *Theme) *string { return &t.NoteStale }, func(o *Override) *string { return &o.NoteStale }},
	{"picker_label", "hunk/conflict picker side labels", func(t *Theme) *string { return &t.PickerLabel }, func(o *Override) *string { return &o.PickerLabel }},

	{"attention_info_bg", "agent attention band background, info tone (gg session highlight)", func(t *Theme) *string { return &t.AttentionInfo }, func(o *Override) *string { return &o.AttentionInfo }},
	{"attention_warn_bg", "agent attention band background, warn tone", func(t *Theme) *string { return &t.AttentionWarn }, func(o *Override) *string { return &o.AttentionWarn }},
	{"attention_error_bg", "agent attention band background, error tone", func(t *Theme) *string { return &t.AttentionError }, func(o *Override) *string { return &o.AttentionError }},
}

const (
	lanesDoc  = `graph lane colours, one per lane (7; "" = inherit)`
	syntaxDoc = `syntax classes Plain,Keyword,Type,Func,Name,String,Number,Comment,Operator,Punct,Attr (11; "" = inherit, and Plain/Name are uncoloured by design)`
)

// ValidColour reports whether s is a "#rrggbb" hex string (case-insensitive)
// or a 0–255 colour-cube index — the two forms lipgloss.Color understands and
// the only two a theme role may hold. "" is not valid (callers treat it as
// "not set" before asking).
func ValidColour(s string) bool {
	if len(s) == 7 && s[0] == '#' {
		for i := 1; i < 7; i++ {
			c := s[i]
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
				return false
			}
		}
		return true
	}
	if len(s) == 0 || len(s) > 3 {
		return false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
		n = n*10 + int(s[i]-'0')
	}
	return n <= 255
}

// Overlay returns base with every SET, VALID field of o applied. An empty
// field (and a nil lanes/syntax slice) is "not set" and leaves base alone; an
// empty ENTRY inside lanes/syntax likewise keeps base's value for that index,
// so a fully-empty example block is inert. Invalid entries are skipped and
// reported as "key=value" ("bg=#12", "syntax[3]=zz") — a wrong-length list is
// rejected whole ("lanes=[6 entries, want 7]"). The reports come back in field
// order, scalars first, then lanes, then syntax. Name is never overridden.
func Overlay(base Theme, o Override) (Theme, []string) {
	out := base
	var bad []string
	for _, f := range roleFields {
		v := *f.getO(&o)
		if v == "" {
			continue
		}
		if !ValidColour(v) {
			bad = append(bad, f.key+"="+v)
			continue
		}
		*f.get(&out) = v
	}
	bad = overlayList(o.Lanes, out.Lanes[:], "lanes", bad)
	bad = overlayList(o.Syntax, out.Syntax[:], "syntax", bad)
	return out, bad
}

// overlayList applies src onto dst (a slice aliasing the Theme's array),
// appending any complaint to bad.
func overlayList(src, dst []string, key string, bad []string) []string {
	if src == nil {
		return bad
	}
	if len(src) != len(dst) {
		return append(bad, fmt.Sprintf("%s=[%d entries, want %d]", key, len(src), len(dst)))
	}
	for i, v := range src {
		if v == "" {
			continue
		}
		if !ValidColour(v) {
			bad = append(bad, fmt.Sprintf("%s[%d]=%s", key, i, v))
			continue
		}
		dst[i] = v
	}
	return bad
}

// Merge layers hi over lo per field (a set field in hi wins) — the config
// overlay rule for [themes.<name>] across the global and repo files. Lists are
// all-or-nothing: a set hi list replaces lo's, copied so neither input aliases
// the result.
func Merge(lo, hi Override) Override {
	out := lo
	for _, f := range roleFields {
		if v := *f.getO(&hi); v != "" {
			*f.getO(&out) = v
		}
	}
	out.Lanes = mergeList(lo.Lanes, hi.Lanes)
	out.Syntax = mergeList(lo.Syntax, hi.Syntax)
	return out
}

// mergeList copies whichever layer set the list — hi whenever it is non-nil,
// INCLUDING a written-but-empty `lanes = []`, which is set, not absent: the
// copy stays non-nil so Overlay reports its length instead of silently falling
// back to the lower layer.
func mergeList(lo, hi []string) []string {
	src := lo
	if hi != nil {
		src = hi
	}
	if src == nil {
		return nil
	}
	out := make([]string, len(src))
	copy(out, src)
	return out
}

// AsOverride returns the Override that reproduces th's current values — the
// source for populate's commented example block. Terminal yields an all-empty
// Override (every role inherits), which Overlay treats as a no-op.
func (th Theme) AsOverride() Override {
	var o Override
	for _, f := range roleFields {
		*f.getO(&o) = *f.get(&th)
	}
	o.Lanes = append([]string(nil), th.Lanes[:]...)
	o.Syntax = append([]string(nil), th.Syntax[:]...)
	return o
}

// Value returns the scalar this Override holds for a RoleDocs key ("" when
// unset, and for an unknown key or the two list keys). It lets a renderer walk
// RoleDocs and pull each value without reflecting over the struct.
func (o Override) Value(key string) string {
	for _, f := range roleFields {
		if f.key == key {
			return *f.getO(&o)
		}
	}
	return ""
}

// RoleDoc is one documented Override key.
type RoleDoc struct{ Key, Doc string }

// RoleDocs lists every Override key with its description, scalars in field
// order followed by lanes and syntax. `gg config populate` renders the
// [themes.<name>] blocks from this plus AsOverride.
func RoleDocs() []RoleDoc {
	out := make([]RoleDoc, 0, len(roleFields)+2)
	for _, f := range roleFields {
		out = append(out, RoleDoc{Key: f.key, Doc: f.doc})
	}
	out = append(out, RoleDoc{Key: "lanes", Doc: lanesDoc}, RoleDoc{Key: "syntax", Doc: syntaxDoc})
	return out
}
