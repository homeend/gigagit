package tui

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/theme"
)

// These tests drive the colour editor, which swaps the process-global styles
// pointer (setTheme) and writes the global config file, so they are SERIAL (no
// t.Parallel), restore the previous theme, and redirect XDG_CONFIG_HOME.

// themeEditorModel opens Settings on a model whose global config lives in a
// scratch dir, with `name` as the active theme, and pushes the colour editor.
func themeEditorModel(t *testing.T, name string) (Model, *themeEditorPopup) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m, _ := settingsModel(t)
	m.cfg.UI.Theme = name
	m, _ = m.applyTheme()
	m, _ = m.openSettings()
	sp := layerOf[*settingsPopup](m)
	if sp == nil {
		t.Fatal("settings popup not open")
	}
	i := slices.Index(settingsMenu, settingsMenuThemeColours)
	if i < 0 {
		t.Fatal("Theme colours row missing from settingsMenu")
	}
	sp.menuSel = i
	um, _ := m.Update(keyMsg("enter"))
	m = um.(Model)
	p := layerOf[*themeEditorPopup](m)
	if p == nil {
		t.Fatal("enter on the Theme colours row must push the editor")
	}
	return m, p
}

func TestThemeColoursRowFollowsTheme(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	iTheme := slices.Index(settingsMenu, settingsMenuTheme)
	iColours := slices.Index(settingsMenu, settingsMenuThemeColours)
	if iTheme < 0 || iColours != iTheme+1 {
		t.Fatalf("Theme colours must sit right after Theme (theme=%d colours=%d)", iTheme, iColours)
	}
	m, _ := settingsModel(t)
	if got := settingsMenuLabel(m, iColours); !strings.Contains(got, "olours") && !strings.Contains(got, "olors") {
		t.Fatalf("row label = %q", got)
	}
}

func TestThemeEditorListsEveryRole(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	_, p := themeEditorModel(t, "light")
	if len(p.roles) != 55 {
		t.Fatalf("roles = %d, want 55", len(p.roles))
	}
	if p.roles[0].Key != "bg" {
		t.Fatalf("first role = %q", p.roles[0].Key)
	}
	if p.roles[len(p.roles)-1].Key != "syntax[Attr]" {
		t.Fatalf("last role = %q", p.roles[len(p.roles)-1].Key)
	}
	if p.base.Name != theme.NameLight {
		t.Fatalf("base = %q, want the active theme", p.base.Name)
	}
}

func TestThemeEditorFilterNarrowsAndEdits(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m, p := themeEditorModel(t, "light")

	um, _ := m.Update(keyMsg("/"))
	m = um.(Model)
	if !p.filtering {
		t.Fatal("/ must enter the filter sub-mode")
	}
	for _, r := range "diff" {
		um, _ = m.Update(keyMsg(string(r)))
		m = um.(Model)
	}
	vis := p.visible()
	if len(vis) == 0 || len(vis) >= len(p.roles) {
		t.Fatalf("filter %q kept %d of %d rows", p.filter.Value(), len(vis), len(p.roles))
	}
	for _, r := range vis {
		if !strings.Contains(strings.ToLower(r.Key+" "+r.Doc), "diff") {
			t.Fatalf("row %q does not match the filter", r.Key)
		}
	}
	// enter leaves the filter sub-mode keeping the query, and a second enter
	// edits the row the filtered cursor is on.
	um, _ = m.Update(keyMsg("enter"))
	m = um.(Model)
	if p.filtering {
		t.Fatal("enter must leave the filter sub-mode")
	}
	um, _ = m.Update(keyMsg("enter"))
	m = um.(Model)
	if !p.editing {
		t.Fatal("enter must open the editor on the filtered selection")
	}
	cur, _ := p.current()
	if !strings.Contains(strings.ToLower(cur.Key+" "+cur.Doc), "diff") {
		t.Fatalf("editing %q, want a filtered row", cur.Key)
	}
	if p.field.Value() != cur.Get(p.effective()) {
		t.Fatalf("the field must be prefilled with the effective value (%q vs %q)", p.field.Value(), cur.Get(p.effective()))
	}
}

func TestThemeEditorSaveWritesGlobalAndPreviews(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m, p := themeEditorModel(t, "light")
	p.sel = slices.IndexFunc(p.roles, func(r theme.RoleRef) bool { return r.Key == "dim" })

	um, _ := m.Update(keyMsg("enter"))
	m = um.(Model)
	if !p.editing {
		t.Fatal("enter must start editing")
	}
	p.field = newTextField("")
	for _, r := range "#123456" {
		um, _ = m.Update(keyMsg(string(r)))
		m = um.(Model)
	}
	// Live preview: the styles already carry the typed colour, before saving.
	if got := string(st().dim.GetForeground().(lipgloss.Color)); got != "#123456" {
		t.Fatalf("live preview: st().dim = %q, want #123456", got)
	}
	if p.statusErr || !strings.Contains(p.status, "#123456") {
		t.Fatalf("status = %q (err=%v)", p.status, p.statusErr)
	}

	um, _ = m.Update(keyMsg("enter"))
	m = um.(Model)
	if p.editing {
		t.Fatal("enter must leave editing after a save")
	}
	raw, err := os.ReadFile(config.DefaultGlobalPath())
	if err != nil {
		t.Fatalf("the global config was not written: %v", err)
	}
	if !strings.Contains(string(raw), "[themes.light]") || !strings.Contains(string(raw), `dim = "#123456"`) {
		t.Fatalf("global config:\n%s", raw)
	}
	// applyTheme ran on the merged config, so the colour SURVIVES the reapply.
	if got := string(st().dim.GetForeground().(lipgloss.Color)); got != "#123456" {
		t.Fatalf("after save st().dim = %q", got)
	}
	if m.cfg.Themes["light"].Dim != "#123456" {
		t.Fatalf("in-memory config not updated: %+v", m.cfg.Themes["light"])
	}
	if p.statusErr {
		t.Fatalf("save reported an error: %q", p.status)
	}
}

func TestThemeEditorInvalidValueRevertsPreview(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m, p := themeEditorModel(t, "light")
	p.sel = slices.IndexFunc(p.roles, func(r theme.RoleRef) bool { return r.Key == "dim" })
	um, _ := m.Update(keyMsg("enter"))
	m = um.(Model)
	p.field = newTextField("")
	for _, r := range "zz" {
		um, _ = m.Update(keyMsg(string(r)))
		m = um.(Model)
	}
	if !p.statusErr {
		t.Fatalf("an invalid colour must set the error status, got %q", p.status)
	}
	if got := string(st().dim.GetForeground().(lipgloss.Color)); got != theme.Light.Dim {
		t.Fatalf("an invalid value must leave the preview at the previous theme, got %q", got)
	}
	// enter does NOT save an invalid value.
	um, _ = m.Update(keyMsg("enter"))
	m = um.(Model)
	if !p.editing {
		t.Fatal("enter on an invalid value must stay in the editor")
	}
	if _, err := os.Stat(config.DefaultGlobalPath()); err == nil {
		raw, _ := os.ReadFile(config.DefaultGlobalPath())
		if strings.Contains(string(raw), "dim = ") {
			t.Fatalf("an invalid value must never be written:\n%s", raw)
		}
	}
}

func TestThemeEditorEscRevertsPreview(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m, p := themeEditorModel(t, "dark")
	p.sel = slices.IndexFunc(p.roles, func(r theme.RoleRef) bool { return r.Key == "dim" })
	um, _ := m.Update(keyMsg("enter"))
	m = um.(Model)
	p.field = newTextField("")
	for _, r := range "#ABCDEF" {
		um, _ = m.Update(keyMsg(string(r)))
		m = um.(Model)
	}
	if got := string(st().dim.GetForeground().(lipgloss.Color)); got != "#ABCDEF" {
		t.Fatalf("preview did not apply, st().dim = %q", got)
	}
	um, _ = m.Update(keyMsg("esc"))
	m = um.(Model)
	if p.editing {
		t.Fatal("esc must leave editing")
	}
	if layerOf[*themeEditorPopup](m) == nil {
		t.Fatal("esc in editing must NOT close the popup")
	}
	if got := string(st().dim.GetForeground().(lipgloss.Color)); got != theme.Dark.Dim {
		t.Fatalf("esc must revert the preview, st().dim = %q", got)
	}
}

func TestThemeEditorDefaultKeyRemovesOverride(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m, p := themeEditorModel(t, "light")
	p.sel = slices.IndexFunc(p.roles, func(r theme.RoleRef) bool { return r.Key == "dim" })

	// Save an override first…
	um, _ := m.Update(keyMsg("enter"))
	m = um.(Model)
	p.field = newTextField("")
	for _, r := range "#123456" {
		um, _ = m.Update(keyMsg(string(r)))
		m = um.(Model)
	}
	um, _ = m.Update(keyMsg("enter"))
	m = um.(Model)

	// …then d restores the built-in.
	um, _ = m.Update(keyMsg("d"))
	m = um.(Model)
	raw, _ := os.ReadFile(config.DefaultGlobalPath())
	if strings.Contains(string(raw), "#123456") {
		t.Fatalf("d must remove the key:\n%s", raw)
	}
	if got := string(st().dim.GetForeground().(lipgloss.Color)); got != theme.Light.Dim {
		t.Fatalf("d must restore the built-in value, got %q", got)
	}
	if p.global.Dim != "" {
		t.Fatalf("the popup's global override still carries %q", p.global.Dim)
	}
}

func TestThemeEditorRepoRowIsReadOnly(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m, p := themeEditorModel(t, "light")
	// Pretend the repo .gg.toml pins dim.
	p.repo = theme.Override{Dim: "#0F0F0F"}
	p.sel = slices.IndexFunc(p.roles, func(r theme.RoleRef) bool { return r.Key == "dim" })

	um, _ := m.Update(keyMsg("enter"))
	m = um.(Model)
	if p.editing {
		t.Fatal("a repo-pinned row must refuse the editor")
	}
	if !p.statusErr || !strings.Contains(p.status, ".gg.toml") {
		t.Fatalf("status = %q (err=%v), want the repo refusal", p.status, p.statusErr)
	}
	um, _ = m.Update(keyMsg("d"))
	m = um.(Model)
	if !p.statusErr {
		t.Fatal("d must refuse a repo-pinned row too")
	}
	if !strings.Contains(p.box(m), "(repo)") {
		t.Fatal("a repo-pinned row must be tagged (repo)")
	}
}

func TestThemeEditorCyclesTheme(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m, p := themeEditorModel(t, "light")
	um, _ := m.Update(keyMsg("t"))
	m = um.(Model)
	if p.base.Name != theme.NameTerminal {
		t.Fatalf("t must cycle light → terminal, base = %q", p.base.Name)
	}
	if activeTheme().Name != theme.NameTerminal {
		t.Fatalf("t must apply the new theme, active = %q", activeTheme().Name)
	}
	if !strings.Contains(p.box(m), themeDisplayName(theme.NameTerminal)) {
		t.Fatal("the header must name the new theme")
	}
}

// The swatch stays painted on the SELECTED row: renderWindow applies a row
// style around the whole line, and a reverse-video row would otherwise swallow
// (or be broken by) the sample's own SGR run.
func TestThemeEditorSwatchPaintedOnSelectedRow(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	profPrev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(profPrev)

	m, p := themeEditorModel(t, "light")
	p.sel = slices.IndexFunc(p.roles, func(r theme.RoleRef) bool { return r.Key == "diff_add_bg" })
	body := p.box(m)
	// theme.Light.DiffAddBg is #D8EAD0 → an SGR background run of that colour.
	want := regexp.MustCompile(`\x1b\[[0-9;]*48;2;216;234;208`)
	if !want.MatchString(body) {
		t.Fatalf("no painted background swatch for diff_add_bg in:\n%s", body)
	}
	if !strings.Contains(body, "\x1b[7m") {
		t.Fatal("the selected row should still be reverse-video")
	}
}

// styles.go is the only file allowed to name a colour: everything else reads
// st(). The editor's swatch goes through swatchStyle for exactly this reason.
func TestNoLipglossColourLiteralOutsideStyles(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "styles.go" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), `lipgloss.Color("`) {
			t.Fatalf(`%s names a lipgloss colour literal; colours belong in styles.go`, name)
		}
	}
}

// esc closes the editor and reveals the Settings popup it was opened over.
func TestThemeEditorEscReturnsToSettings(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m, _ := themeEditorModel(t, "light")
	um, _ := m.Update(keyMsg("esc"))
	m = um.(Model)
	if layerOf[*themeEditorPopup](m) != nil {
		t.Fatal("esc must close the editor")
	}
	if _, ok := m.topLayer().(*settingsPopup); !ok {
		t.Fatalf("esc must reveal the Settings popup, got %T", m.topLayer())
	}
}

// The popup swallows global keys: `p` (push) must not start an operation.
func TestThemeEditorSwallowsGlobalKeys(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m, _ := themeEditorModel(t, "light")
	um, cmd := m.Update(keyMsg("p"))
	m = um.(Model)
	if m.running {
		t.Fatal("a global key must not leak out of the editor")
	}
	if layerOf[*themeEditorPopup](m) == nil {
		t.Fatal("the editor must stay open")
	}
	_ = cmd
}

// ctrl+t goes through the shared popupMax mechanism, so the box gets wider.
func TestThemeEditorMaximizes(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m, p := themeEditorModel(t, "light")
	m.width, m.height = 160, 50 // maximizing only buys room on a wide terminal
	narrow := lipgloss.Width(p.box(m))
	um, _ := m.Update(keyMsg("ctrl+t"))
	m = um.(Model)
	if !p.maxed() {
		t.Fatal("ctrl+t must maximize the editor")
	}
	if wide := lipgloss.Width(p.box(m)); wide <= narrow {
		t.Fatalf("maximized width %d should exceed %d", wide, narrow)
	}
}

// A lane entry writes the WHOLE lanes array line.
func TestThemeEditorSavesListRole(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m, p := themeEditorModel(t, "dark")
	p.sel = slices.IndexFunc(p.roles, func(r theme.RoleRef) bool { return r.Key == "lanes[1]" })
	um, _ := m.Update(keyMsg("enter"))
	m = um.(Model)
	p.field = newTextField("")
	for _, r := range "#ff5f00" {
		um, _ = m.Update(keyMsg(string(r)))
		m = um.(Model)
	}
	um, _ = m.Update(keyMsg("enter"))
	m = um.(Model)
	raw, err := os.ReadFile(config.DefaultGlobalPath())
	if err != nil {
		t.Fatalf("global config not written: %v", err)
	}
	if !strings.Contains(string(raw), `lanes = [`) || !strings.Contains(string(raw), `"#ff5f00"`) {
		t.Fatalf("lanes line missing:\n%s", raw)
	}
	if string(st().lane(1)) != "#ff5f00" {
		t.Fatalf("lane 1 = %q after save", st().lane(1))
	}
	if string(st().lane(0)) != theme.Dark.Lanes[0] {
		t.Fatalf("lane 0 must keep the built-in value, got %q", st().lane(0))
	}
}

// Emptying the field on a lane row is a REMOVAL, not a write of the whole
// palette: the six lanes the user never touched must not be pinned to today's
// built-in colours behind their back.
func TestThemeEditorClearingListEntryDoesNotPinTheRest(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m, p := themeEditorModel(t, "dark")
	p.sel = slices.IndexFunc(p.roles, func(r theme.RoleRef) bool { return r.Key == "lanes[1]" })

	um, _ := m.Update(keyMsg("enter"))
	m = um.(Model)
	p.field = newTextField("")
	um, _ = m.Update(keyMsg("enter"))
	m = um.(Model)

	if p.global.Lanes != nil {
		t.Fatalf("clearing an unset lane must leave the list unset, got %#v", p.global.Lanes)
	}
	if raw, err := os.ReadFile(config.DefaultGlobalPath()); err == nil && strings.Contains(string(raw), "lanes = [") {
		t.Fatalf("no lanes line should have been written:\n%s", raw)
	}
	if string(st().lane(1)) != theme.Dark.Lanes[1] {
		t.Fatalf("lane 1 = %q, want the built-in", st().lane(1))
	}

	// With one lane already overridden, clearing it drops the whole line again.
	p.sel = slices.IndexFunc(p.roles, func(r theme.RoleRef) bool { return r.Key == "lanes[1]" })
	um, _ = m.Update(keyMsg("enter"))
	m = um.(Model)
	p.field = newTextField("")
	for _, r := range "#ff5f00" {
		um, _ = m.Update(keyMsg(string(r)))
		m = um.(Model)
	}
	um, _ = m.Update(keyMsg("enter"))
	m = um.(Model)
	if p.global.Lanes == nil {
		t.Fatal("the save should have set the lanes list")
	}
	um, _ = m.Update(keyMsg("d"))
	m = um.(Model)
	raw, _ := os.ReadFile(config.DefaultGlobalPath())
	if strings.Contains(string(raw), "lanes = [") {
		t.Fatalf("d must drop the all-empty lanes line:\n%s", raw)
	}
	if string(st().lane(1)) != theme.Dark.Lanes[1] {
		t.Fatalf("lane 1 = %q after d, want the built-in", st().lane(1))
	}
}

// The repo layer comes off DISK: a [themes.<name>] table in the repo .gg.toml
// makes its roles read-only here, because the repo layer wins the merge and
// would shadow anything this editor wrote to the global file.
func TestThemeEditorReadsRepoOverrideFromDisk(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m, dir := settingsModel(t)
	if m.repoConfigPath == "" {
		t.Fatal("the model has no repo config path")
	}
	if err := os.WriteFile(filepath.Join(dir, ".gg.toml"),
		[]byte("[themes.light]\ndim = \"#123456\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.cfg.UI.Theme = "light"
	m, _ = m.applyTheme()
	m, _ = m.openSettings()
	sp := layerOf[*settingsPopup](m)
	sp.menuSel = slices.Index(settingsMenu, settingsMenuThemeColours)
	um, _ := m.Update(keyMsg("enter"))
	m = um.(Model)
	p := layerOf[*themeEditorPopup](m)
	if p == nil {
		t.Fatal("editor not open")
	}

	if p.repo.Dim != "#123456" {
		t.Fatalf("themeOverrideIn did not read the repo file: %+v", p.repo)
	}
	p.sel = slices.IndexFunc(p.roles, func(r theme.RoleRef) bool { return r.Key == "dim" })
	body := p.box(m)
	if !strings.Contains(body, "(repo)") || !strings.Contains(body, "#123456") {
		t.Fatalf("the dim row must show the repo value tagged (repo):\n%s", body)
	}
	um, _ = m.Update(keyMsg("enter"))
	m = um.(Model)
	if p.editing {
		t.Fatal("a repo-pinned row must refuse the editor")
	}
	if !p.statusErr || !strings.Contains(p.status, ".gg.toml") {
		t.Fatalf("status = %q (err=%v)", p.status, p.statusErr)
	}
}

// A GLOBAL override under a repo-pinned role is stale — the repo value wins
// either way — so `d` must still be able to remove it, saying the repo value
// still applies rather than pretending nothing happened.
func TestThemeEditorDefaultClearsStaleGlobalUnderRepo(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m, p := themeEditorModel(t, "light")
	p.sel = slices.IndexFunc(p.roles, func(r theme.RoleRef) bool { return r.Key == "dim" })

	// Save a global override, then let the repo pin the same role.
	um, _ := m.Update(keyMsg("enter"))
	m = um.(Model)
	p.field = newTextField("")
	for _, r := range "#123456" {
		um, _ = m.Update(keyMsg(string(r)))
		m = um.(Model)
	}
	um, _ = m.Update(keyMsg("enter"))
	m = um.(Model)
	p.repo = theme.Override{Dim: "#0F0F0F"}

	um, _ = m.Update(keyMsg("d"))
	m = um.(Model)
	if p.global.Dim != "" {
		t.Fatalf("d must clear the stale global override, got %q", p.global.Dim)
	}
	raw, _ := os.ReadFile(config.DefaultGlobalPath())
	if strings.Contains(string(raw), "#123456") {
		t.Fatalf("the global line survived:\n%s", raw)
	}
	if p.statusErr || !strings.Contains(p.status, ".gg.toml") {
		t.Fatalf("status = %q (err=%v), want a non-error note that the repo value still applies", p.status, p.statusErr)
	}
}

// esc is two-step once a filter is committed, like every other filtering popup.
func TestThemeEditorEscClearsFilterFirst(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m, p := themeEditorModel(t, "light")
	um, _ := m.Update(keyMsg("/"))
	m = um.(Model)
	for _, r := range "diff" {
		um, _ = m.Update(keyMsg(string(r)))
		m = um.(Model)
	}
	um, _ = m.Update(keyMsg("enter")) // commit the filter, leave input mode
	m = um.(Model)

	um, _ = m.Update(keyMsg("esc"))
	m = um.(Model)
	if layerOf[*themeEditorPopup](m) == nil {
		t.Fatal("the first esc must clear the filter, not close the popup")
	}
	if p.filter.Value() != "" || len(p.visible()) != len(p.roles) {
		t.Fatalf("the filter survived: %q (%d rows)", p.filter.Value(), len(p.visible()))
	}
	um, _ = m.Update(keyMsg("esc"))
	m = um.(Model)
	if layerOf[*themeEditorPopup](m) != nil {
		t.Fatal("the second esc must close the popup")
	}
}

// Mouse parity with the other list popups: the wheel scrolls, a double-click is
// enter, a middle-click is esc — but never while the inline field is open, where
// enter would SAVE a colour the user never confirmed.
func TestThemeEditorMouse(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m, p := themeEditorModel(t, "light")

	um, _ := m.Update(wheelMsg(false))
	m = um.(Model)
	if p.sel == 0 {
		t.Fatal("the wheel must scroll the list")
	}
	before := p.sel
	um, _ = m.Update(wheelMsg(true))
	m = um.(Model)
	if p.sel >= before {
		t.Fatalf("wheel up must move back, %d → %d", before, p.sel)
	}

	click := tea.MouseMsg{X: 40, Y: 10, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	um, _ = m.Update(click)
	m = um.(Model)
	um, _ = m.Update(click)
	m = um.(Model)
	if !p.editing {
		t.Fatal("a double-click must act as enter on a browse row")
	}
	if clickEnterLayer(p) {
		// While the inline field is open enter SAVES; a stray double-click must
		// never commit a colour the user did not confirm.
		t.Fatal("clickEnterLayer must go inert while the inline field is open")
	}
	um, _ = m.Update(keyMsg("esc"))
	m = um.(Model)
	um, _ = m.Update(tea.MouseMsg{X: 40, Y: 10, Action: tea.MouseActionPress, Button: tea.MouseButtonMiddle})
	m = um.(Model)
	if layerOf[*themeEditorPopup](m) != nil {
		t.Fatal("a middle-click must close the popup")
	}
}
