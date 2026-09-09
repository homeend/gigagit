package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/theme"
)

// [themes.<name>] tables layer global -> repo per FIELD, like every other
// section: the repo file's fg wins, the global file's bg and lanes survive.
func TestLoadThemesOverlay(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	global := filepath.Join(dir, "global.toml")
	repo := filepath.Join(dir, "repo.toml")
	if err := os.WriteFile(global, []byte("[themes.light]\nbg = \"#111111\"\nfg = \"#999999\"\nlanes = [\"1\",\"2\",\"3\",\"4\",\"5\",\"6\",\"7\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(repo, []byte("[themes.light]\nfg = \"#222222\"\n\n[themes.solarized]\nbg = \"#002B36\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(global, repo)
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Themes["light"]
	if got.Bg != "#111111" {
		t.Errorf("global-only bg lost: %q", got.Bg)
	}
	if got.Fg != "#222222" {
		t.Errorf("repo fg must win: %q", got.Fg)
	}
	if len(got.Lanes) != 7 || got.Lanes[0] != "1" {
		t.Errorf("global lanes lost: %v", got.Lanes)
	}
	// An unknown theme name is kept verbatim — it simply never matches.
	if cfg.Themes["solarized"].Bg != "#002B36" {
		t.Errorf("unknown theme name dropped: %+v", cfg.Themes["solarized"])
	}
	if len(Defaults().Themes) != 0 {
		t.Errorf("Defaults() must leave Themes nil, got %+v", Defaults().Themes)
	}
}

// A config with no [themes] table leaves the map empty (and the TUI's
// Themes[name] lookup a zero Override, i.e. a no-op overlay).
func TestLoadNoThemesTable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	global := filepath.Join(dir, "global.toml")
	if err := os.WriteFile(global, []byte("[ui]\ntheme = \"dark\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(global, filepath.Join(dir, "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Themes) != 0 {
		t.Fatalf("Themes = %+v, want empty", cfg.Themes)
	}
	var zero theme.Override
	if !reflect.DeepEqual(cfg.Themes["dark"], zero) {
		t.Fatalf("absent name must read as a zero Override, got %+v", cfg.Themes["dark"])
	}
}

func TestPopulateEmitsThemeBlocks(t *testing.T) {
	t.Parallel()
	out := populate("")
	for _, want := range []string{
		"# [themes.light]",
		"# [themes.dark]",
		"# [themes.terminal]",
		`# bg = "#E9E9E5"`,
		`# bg = "#0C0C0C"`,
		`# lanes = ["#2F6FB8", "#C7641B", "#3E8E41", "#6B4FBB", "#2A8C8C", "#B08000", "#C0392B"]`,
		`# syntax = ["", "#6B4FBB", `,
		"frame background",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("populate(\"\") missing %q\n%s", want, out)
		}
	}
	// The terminal block documents every key with an empty (inert) value.
	if !strings.Contains(out, `# bg = ""`) {
		t.Errorf("terminal block must render empty values:\n%s", out)
	}
	// The blocks sit under [ui], before the next section header.
	ui := strings.Index(out, "\n[ui]\n")
	themes := strings.Index(out, "# [themes.light]")
	debug := strings.Index(out, "\n[debug]\n")
	if ui < 0 || themes < 0 || debug < 0 || !(ui < themes && themes < debug) {
		t.Errorf("theme blocks must sit between [ui] and [debug] (ui=%d themes=%d debug=%d)", ui, themes, debug)
	}
}

func TestPopulateThemeBlocksIdempotent(t *testing.T) {
	t.Parallel()
	once := populate("")
	twice := populate(once)
	if once != twice {
		t.Fatalf("theme blocks duplicated on a second populate:\n%s", twice)
	}
	if n := strings.Count(twice, "[themes.light]"); n != 1 {
		t.Fatalf("[themes.light] appears %d times, want 1", n)
	}
}

// An ACTIVE (uncommented) [themes.light] table the user wrote is left alone —
// no example block is added on top of it.
func TestPopulateSkipsExistingThemeTable(t *testing.T) {
	t.Parallel()
	out := populate("[themes.light]\nbg = \"#ABCDEF\"\n")
	if n := strings.Count(out, "[themes.light]"); n != 1 {
		t.Fatalf("[themes.light] appears %d times, want 1:\n%s", n, out)
	}
	if !strings.Contains(out, `bg = "#ABCDEF"`) {
		t.Fatalf("user's active value clobbered:\n%s", out)
	}
	if strings.Contains(out, `# bg = "#E9E9E5"`) {
		t.Fatalf("light example block added on top of the user's table:\n%s", out)
	}
	// The other two blocks are still offered.
	if !strings.Contains(out, "# [themes.dark]") {
		t.Fatalf("dark block missing:\n%s", out)
	}
}

// The Settings theme cycle line-edits [ui] theme in a file that may now carry
// an ACTIVE [themes.<name>] table right after [ui] — the layout populate steers
// users toward. The table must come through byte-intact, and the key must land
// inside [ui], not after the table.
func TestSetGlobalUIThemeKeepsThemeTables(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	before := "[ui]\ntheme = \"light\"\n\n[themes.light]\nbg = \"#111111\"\nlanes = [\"1\", \"2\", \"3\", \"4\", \"5\", \"6\", \"7\"]\n"
	if err := os.WriteFile(path, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetGlobalUITheme(path, "dark"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	if !strings.Contains(got, "[themes.light]\nbg = \"#111111\"\nlanes = [\"1\", \"2\", \"3\", \"4\", \"5\", \"6\", \"7\"]") {
		t.Fatalf("the theme table was not preserved verbatim:\n%s", got)
	}
	if !strings.HasPrefix(got, "[ui]\ntheme = \"dark\"\n") {
		t.Fatalf("theme must be rewritten in place under [ui]:\n%s", got)
	}
	if strings.Count(got, "theme = ") != 1 {
		t.Fatalf("theme assigned more than once:\n%s", got)
	}
	// And the result still decodes to what the user meant.
	cfg, err := Load(path, filepath.Join(t.TempDir(), "none.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.Theme != "dark" || cfg.Themes["light"].Bg != "#111111" {
		t.Fatalf("decoded = %q / %+v", cfg.UI.Theme, cfg.Themes["light"])
	}
}

// A file with NO [ui] section yet, but a theme table: the new key must open a
// [ui] section rather than land inside [themes.light].
func TestSetGlobalUIThemeWithOnlyThemeTable(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[themes.light]\nbg = \"#111111\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetGlobalUITheme(path, "dark"); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path, filepath.Join(t.TempDir(), "none.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.Theme != "dark" {
		raw, _ := os.ReadFile(path)
		t.Fatalf("theme = %q, want dark:\n%s", cfg.UI.Theme, raw)
	}
	if cfg.Themes["light"].Bg != "#111111" {
		t.Fatalf("theme table lost: %+v", cfg.Themes["light"])
	}
}

// A file that already carries every settingDocs key (what `gg config init`
// writes) still GAINS the three theme blocks — and PopulateFile must count
// them, or the CLI prints "already complete" while rewriting the file.
func TestPopulateFileCountsThemeBlocks(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(Template()), 0o644); err != nil {
		t.Fatal(err)
	}
	added, err := PopulateFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if added != 3 {
		t.Fatalf("added = %d, want 3 (one per [themes.<name>] block)", added)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# [themes.light]", "# [themes.dark]", "# [themes.terminal]"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("missing %q after populate:\n%s", want, raw)
		}
	}
	added2, err := PopulateFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if added2 != 0 {
		t.Fatalf("second populate added %d, want 0", added2)
	}
}

// Every generated line carries the [populated] marker, like the scalar keys.
func TestPopulateThemeBlockLinesMarked(t *testing.T) {
	t.Parallel()
	for _, ln := range strings.Split(populate(""), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(ln), "#") {
			continue
		}
		if !strings.Contains(ln, "[themes.") && !strings.Contains(ln, "frame background") && !strings.Contains(ln, "graph lane colours") {
			continue
		}
		if !strings.HasSuffix(ln, "[populated]") {
			t.Errorf("generated theme line is unmarked: %q", ln)
		}
	}
}

// The blocks are inert: populating an empty file and decoding it yields no
// theme overrides at all.
func TestPopulatedThemeBlocksDecodeToNothing(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), ".gg.toml")
	if err := os.WriteFile(path, []byte(populate("")), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, ok, err := decodeFile(path)
	if err != nil || !ok {
		t.Fatalf("populated file did not decode: ok=%v err=%v", ok, err)
	}
	if len(cfg.Themes) != 0 {
		t.Fatalf("commented blocks must decode to nothing, got %+v", cfg.Themes)
	}
}
