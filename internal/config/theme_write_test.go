package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readLines is the per-test view of the written file.
func readLines(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
}

// fileBody is the whole written file. EVERY test here also round-trips the
// result through Load: a missed header that appends a SECOND [themes.<name>]
// table is a TOML parse error — i.e. a gg that refuses to start — and that is
// the one fatal failure mode of a line-oriented writer.
func fileBody(t *testing.T, path string) string {
	t.Helper()
	raw, _ := os.ReadFile(path)
	return string(raw)
}

func TestSetThemeRoleCreatesSection(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[ui]\ntheme = \"light\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := SetThemeRole(path, "light", "bg", "#101010"); err != nil {
		t.Fatalf("set: %v", err)
	}
	body := fileBody(t, path)
	if !strings.Contains(body, "[themes.light]") || !strings.Contains(body, `bg = "#101010"`) {
		t.Fatalf("section not appended:\n%s", body)
	}
	cfg, err := Load(path, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := cfg.Themes["light"].Bg; got != "#101010" {
		t.Fatalf("Themes[light].Bg = %q", got)
	}
	if cfg.UI.Theme != "light" {
		t.Fatalf("the [ui] section was disturbed: %q", cfg.UI.Theme)
	}

	// A second key lands INSIDE the section, not after whatever follows it.
	if err := SetThemeRole(path, "light", "fg", "#EEEEEE"); err != nil {
		t.Fatalf("set 2: %v", err)
	}
	lines := readLines(t, path)
	hdr, fg := -1, -1
	for i, ln := range lines {
		switch strings.TrimSpace(ln) {
		case "[themes.light]":
			hdr = i
		case `fg = "#EEEEEE"`:
			fg = i
		}
	}
	if hdr < 0 || fg < hdr {
		t.Fatalf("fg landed outside [themes.light] (hdr=%d fg=%d):\n%s", hdr, fg, fileBody(t, path))
	}
	cfg, _ = Load(path, "")
	if cfg.Themes["light"].Fg != "#EEEEEE" || cfg.Themes["light"].Bg != "#101010" {
		t.Fatalf("both keys must survive: %+v", cfg.Themes["light"])
	}
}

func TestSetThemeRoleReplacesExistingValue(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte("[themes.dark]\nbg = \"#000000\"\nfg = \"#FFFFFF\"\n"), 0o644)

	if err := SetThemeRole(path, "dark", "bg", "#123456"); err != nil {
		t.Fatalf("set: %v", err)
	}
	body := fileBody(t, path)
	if strings.Contains(body, "#000000") {
		t.Fatalf("old value survived:\n%s", body)
	}
	if strings.Count(body, "[themes.dark]") != 1 {
		t.Fatalf("the section was duplicated:\n%s", body)
	}
	cfg, err := Load(path, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Themes["dark"].Bg != "#123456" || cfg.Themes["dark"].Fg != "#FFFFFF" {
		t.Fatalf("themes.dark = %+v", cfg.Themes["dark"])
	}
}

func TestSetThemeRoleUncommentsPopulatedBlock(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte(Template()), 0o644)
	if _, err := PopulateFile(path); err != nil {
		t.Fatalf("populate: %v", err)
	}
	before := readLines(t, path)
	darkBgCommented := 0
	for _, ln := range before {
		if strings.HasPrefix(strings.TrimSpace(ln), "# bg = ") {
			darkBgCommented++
		}
	}
	if darkBgCommented < 2 {
		t.Fatalf("populate should have written a commented bg line per theme, got %d", darkBgCommented)
	}

	if err := SetThemeRole(path, "light", "bg", "#ABCDEF"); err != nil {
		t.Fatalf("set: %v", err)
	}

	body := fileBody(t, path)
	if strings.Count(body, "[themes.light]") != 1 {
		t.Fatalf("light section duplicated (a second table breaks TOML):\n%s", body)
	}
	lines := readLines(t, path)
	hdr := -1
	for i, ln := range lines {
		if strings.TrimSpace(ln) == "[themes.light]" {
			hdr = i
		}
	}
	if hdr < 0 {
		t.Fatalf("the commented [themes.light] header was not uncommented in place:\n%s", body)
	}
	// The bg line inside the light block is the ACTIVE one now; the rest of the
	// block stays commented, and the dark block is untouched.
	active := 0
	for _, ln := range lines[hdr+1:] {
		trimmed := strings.TrimSpace(ln)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			break
		}
		active++
		if trimmed != `bg = "#ABCDEF"` {
			t.Fatalf("unexpected active line in the light block: %q", trimmed)
		}
	}
	if active != 1 {
		t.Fatalf("want exactly one active line in [themes.light], got %d:\n%s", active, body)
	}
	if !strings.Contains(body, "# [themes.dark]") {
		t.Fatalf("the dark block must stay commented:\n%s", body)
	}

	cfg, err := Load(path, "")
	if err != nil {
		t.Fatalf("populated file no longer parses: %v\n%s", err, body)
	}
	if cfg.Themes["light"].Bg != "#ABCDEF" {
		t.Fatalf("light bg = %q", cfg.Themes["light"].Bg)
	}
	if cfg.Themes["dark"].Bg != "" {
		t.Fatalf("the dark block must still be inert, got bg=%q", cfg.Themes["dark"].Bg)
	}
}

func TestSetThemeRoleWritesList(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte("[ui]\ntheme = \"dark\"\n"), 0o644)

	lanes := []string{"#111111", "", "33", "#222222", "40", "51", "220"}
	if err := SetThemeRole(path, "dark", "lanes", lanes...); err != nil {
		t.Fatalf("set: %v", err)
	}
	body := fileBody(t, path)
	want := `lanes = ["#111111", "", "33", "#222222", "40", "51", "220"]`
	if !strings.Contains(body, want) {
		t.Fatalf("want %s in:\n%s", want, body)
	}
	cfg, err := Load(path, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := cfg.Themes["dark"].Lanes; len(got) != 7 || got[0] != "#111111" || got[1] != "" {
		t.Fatalf("lanes = %#v", got)
	}
}

func TestSetThemeRoleRemovesActiveLine(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte("[themes.dark]\nbg = \"#000000\"\nfg = \"#FFFFFF\"\n\n[ui]\ntheme = \"dark\"\n"), 0o644)

	if err := SetThemeRole(path, "dark", "bg", ""); err != nil {
		t.Fatalf("remove: %v", err)
	}
	body := fileBody(t, path)
	if strings.Contains(body, "bg = ") {
		t.Fatalf("the bg line should be gone:\n%s", body)
	}
	cfg, err := Load(path, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Themes["dark"].Bg != "" || cfg.Themes["dark"].Fg != "#FFFFFF" {
		t.Fatalf("themes.dark = %+v", cfg.Themes["dark"])
	}
	if cfg.UI.Theme != "dark" {
		t.Fatalf("[ui] disturbed: %q", cfg.UI.Theme)
	}
}

func TestSetThemeRoleRemoveRecommentsPopulatedLine(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	// A hand-uncommented populate row still carries its doc tail.
	os.WriteFile(path, []byte("[themes.dark]\nbg = \"#000000\"   # frame background [populated]\n"), 0o644)

	if err := SetThemeRole(path, "dark", "bg"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	body := fileBody(t, path)
	if !strings.Contains(body, `# bg = "#000000"   # frame background [populated]`) {
		t.Fatalf("a populate-generated line must be RE-COMMENTED, not deleted:\n%s", body)
	}
	cfg, err := Load(path, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Themes["dark"].Bg != "" {
		t.Fatalf("bg should be inert again, got %q", cfg.Themes["dark"].Bg)
	}
}

func TestSetThemeRoleLeavesOtherSectionsByteIdentical(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte(Template()), 0o644)
	before := readLines(t, path)

	if err := SetThemeRole(path, "dark", "note_user", "200"); err != nil {
		t.Fatalf("set: %v", err)
	}
	after := readLines(t, path)

	slice := func(lines []string, header string) []string {
		var out []string
		in := false
		for _, ln := range lines {
			trimmed := strings.TrimSpace(ln)
			if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
				in = trimmed == header
				continue
			}
			if in {
				out = append(out, ln)
			}
		}
		return out
	}
	for _, sec := range []string{"[ui]", "[debug]"} {
		b, a := slice(before, sec), slice(after, sec)
		if strings.Join(b, "\n") != strings.Join(a, "\n") {
			t.Fatalf("%s changed:\nbefore:\n%s\nafter:\n%s", sec, strings.Join(b, "\n"), strings.Join(a, "\n"))
		}
	}
	if _, err := Load(path, ""); err != nil {
		t.Fatalf("load: %v", err)
	}
}

// The writer must not be fooled by a [[tools.command]] block's multi-line
// script — a "[ -d x ]" line inside it is not a section header, and the array
// table itself IS one.
func TestSetThemeRoleSurvivesToolsBlock(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte(`[themes.dark]
bg = "#000000"

[[tools.command]]
category = "conflict"
name = "x"
command = '''
[ -d x ] && echo bg = "nope"
'''
`), 0o644)

	if err := SetThemeRole(path, "dark", "fg", "#DDDDDD"); err != nil {
		t.Fatalf("set: %v", err)
	}
	cfg, err := Load(path, "")
	if err != nil {
		t.Fatalf("load: %v\n%s", err, fileBody(t, path))
	}
	if cfg.Themes["dark"].Fg != "#DDDDDD" || cfg.Themes["dark"].Bg != "#000000" {
		t.Fatalf("themes.dark = %+v", cfg.Themes["dark"])
	}
	if len(cfg.Tools.Command) != 1 || !strings.Contains(cfg.Tools.Command[0].Command, `bg = "nope"`) {
		t.Fatalf("the tools block was damaged: %+v", cfg.Tools.Command)
	}
}

// activeThemeHeaders counts the ACTIVE (uncommented) headers for a theme table.
func activeThemeHeaders(t *testing.T, path, header string) int {
	t.Helper()
	n := 0
	for _, ln := range readLines(t, path) {
		if strings.TrimSpace(ln) == header {
			n++
		}
	}
	return n
}

// A commented populate block BEFORE the real table must not be mistaken for the
// section: uncommenting it would leave two [themes.light] tables, which is a
// TOML parse error — gg would refuse to start.
func TestSetThemeRoleCommentedBlockBeforeActiveTable(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte(`# [themes.light]   # colour overrides [populated]
# bg = "#E9E9E5"   # frame background [populated]
# fg = "#33393F"   # frame foreground [populated]

[themes.light]
fg = "#222222"
`), 0o644)

	if err := SetThemeRole(path, "light", "bg", "#ABCDEF"); err != nil {
		t.Fatalf("set: %v", err)
	}
	cfg, err := Load(path, "")
	if err != nil {
		t.Fatalf("the file no longer parses: %v\n%s", err, fileBody(t, path))
	}
	if cfg.Themes["light"].Bg != "#ABCDEF" || cfg.Themes["light"].Fg != "#222222" {
		t.Fatalf("themes.light = %+v\n%s", cfg.Themes["light"], fileBody(t, path))
	}
	if n := activeThemeHeaders(t, path, "[themes.light]"); n != 1 {
		t.Fatalf("want exactly one active [themes.light], got %d:\n%s", n, fileBody(t, path))
	}
	if !strings.Contains(fileBody(t, path), `# [themes.light]`) {
		t.Fatalf("the commented example block must stay commented:\n%s", fileBody(t, path))
	}
}

// The mirror image: the real table comes FIRST, another section follows, and the
// commented block is last. Activating a `# bg = …` line under a commented header
// would land it in the PRECEDING active section ([debug].bg), where Load parses
// fine and the colour is silently gone on the next start.
func TestSetThemeRoleCommentedBlockAfterActiveTable(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte(`[themes.light]
fg = "#222222"

[debug]
log_operations = false

# [themes.light]   # colour overrides [populated]
# bg = "#E9E9E5"   # frame background [populated]
`), 0o644)

	if err := SetThemeRole(path, "light", "bg", "#ABCDEF"); err != nil {
		t.Fatalf("set: %v", err)
	}
	cfg, err := Load(path, "")
	if err != nil {
		t.Fatalf("load: %v\n%s", err, fileBody(t, path))
	}
	if cfg.Themes["light"].Bg != "#ABCDEF" {
		t.Fatalf("bg = %q — it did not reach the active table:\n%s", cfg.Themes["light"].Bg, fileBody(t, path))
	}
	if cfg.Debug.LogOperations {
		t.Fatal("the [debug] section was disturbed")
	}
	if n := activeThemeHeaders(t, path, "[themes.light]"); n != 1 {
		t.Fatalf("want exactly one active [themes.light], got %d:\n%s", n, fileBody(t, path))
	}
	lines := readLines(t, path)
	for _, ln := range lines {
		if strings.TrimSpace(ln) == `bg = "#E9E9E5"` {
			t.Fatalf("the commented example line was activated:\n%s", fileBody(t, path))
		}
	}
}

// A near-miss name must not collect the key.
func TestSetThemeRoleIgnoresNearMissSection(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte("[themes.lightx]\nfg = \"#111111\"\n"), 0o644)

	if err := SetThemeRole(path, "light", "bg", "#ABCDEF"); err != nil {
		t.Fatalf("set: %v", err)
	}
	cfg, err := Load(path, "")
	if err != nil {
		t.Fatalf("load: %v\n%s", err, fileBody(t, path))
	}
	if cfg.Themes["light"].Bg != "#ABCDEF" {
		t.Fatalf("light bg = %q:\n%s", cfg.Themes["light"].Bg, fileBody(t, path))
	}
	if cfg.Themes["lightx"].Bg != "" || cfg.Themes["lightx"].Fg != "#111111" {
		t.Fatalf("themes.lightx was disturbed: %+v", cfg.Themes["lightx"])
	}
}

// A quoted-name table is not a shape this writer understands, but it MUST still
// count as a boundary so keys never leak past it into the target table.
func TestSetThemeRoleQuotedSectionIsABoundary(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte("[themes.light]\nfg = \"#222222\"\n\n[themes.\"my theme\"]\nbg = \"#000000\"\n"), 0o644)

	if err := SetThemeRole(path, "light", "bg", "#ABCDEF"); err != nil {
		t.Fatalf("set: %v", err)
	}
	cfg, err := Load(path, "")
	if err != nil {
		t.Fatalf("load: %v\n%s", err, fileBody(t, path))
	}
	if cfg.Themes["light"].Bg != "#ABCDEF" {
		t.Fatalf("light bg = %q:\n%s", cfg.Themes["light"].Bg, fileBody(t, path))
	}
	if cfg.Themes["my theme"].Bg != "#000000" {
		t.Fatalf("the quoted table's own bg was rewritten: %+v\n%s", cfg.Themes["my theme"], fileBody(t, path))
	}
}

// The target section is last and the file has no trailing newline.
func TestSetThemeRoleTargetSectionLastNoTrailingNewline(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte("[ui]\ntheme = \"light\"\n\n[themes.light]\nfg = \"#222222\""), 0o644)

	if err := SetThemeRole(path, "light", "bg", "#ABCDEF"); err != nil {
		t.Fatalf("set: %v", err)
	}
	cfg, err := Load(path, "")
	if err != nil {
		t.Fatalf("load: %v\n%s", err, fileBody(t, path))
	}
	if cfg.Themes["light"].Bg != "#ABCDEF" || cfg.Themes["light"].Fg != "#222222" {
		t.Fatalf("themes.light = %+v\n%s", cfg.Themes["light"], fileBody(t, path))
	}
	if n := activeThemeHeaders(t, path, "[themes.light]"); n != 1 {
		t.Fatalf("headers = %d:\n%s", n, fileBody(t, path))
	}
}

// --- RemoveThemeTable: the editor's whole-theme reset (D). ---

func TestRemoveThemeTableDropsGgWrittenSection(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[ui]\ntheme = \"light\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetThemeRole(path, "light", "bg", "#101010"); err != nil {
		t.Fatal(err)
	}
	if err := SetThemeRole(path, "light", "lanes", "1", "", "", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := SetThemeRole(path, "dark", "fg", "#eeeeee"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveThemeTable(path, "light"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	want := "[ui]\ntheme = \"light\"\n\n[themes.dark]\nfg = \"#eeeeee\"\n"
	if got := fileBody(t, path); got != want {
		t.Fatalf("file after remove:\n%s\nwant:\n%s", got, want)
	}
	cfg, err := Load(path, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, ok := cfg.Themes["light"]; ok {
		t.Fatal("light table must be gone")
	}
	if cfg.Themes["dark"].Fg != "#eeeeee" {
		t.Fatal("dark table must survive")
	}
}

func TestRemoveThemeTableAtEndOfFileClosesUp(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[ui]\ntheme = \"light\"\n\n[themes.light]\nbg = \"#101010\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RemoveThemeTable(path, "light"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if got, want := fileBody(t, path), "[ui]\ntheme = \"light\"\n"; got != want {
		t.Fatalf("file after remove:\n%q\nwant:\n%q", got, want)
	}
}

// A populate-generated block that the editor uncommented in place goes back
// to being an inert, fully commented example: rows still carrying the
// [populated] marker are re-commented (a hand-uncommented row included), rows
// gg rewrote (marker gone) are dropped like `d` drops them, and the header is
// re-commented rather than deleted so the example block survives.
func TestRemoveThemeTableRecommentsPopulatedBlock(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	body := strings.Join([]string{
		"[ui]",
		`theme = "light"`,
		"",
		`# [themes.light]   # neutral light grey [populated]`,
		`# bg = "#E9E9E5"   # frame background [populated]`,
		`fg = "#111111"     # frame foreground [populated]`, // hand-uncommented
		`# dim = "#8A8F8A"   # dim text [populated]`,
		`# [themes.dark]   # Campbell [populated]`,
		`# bg = "#0C0C0C"   # frame background [populated]`,
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// The hand-uncommented fg made the header… still commented. The editor's
	// first write activates the header and replaces the bg row in place.
	if err := SetThemeRole(path, "light", "bg", "#101010"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveThemeTable(path, "light"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	want := strings.Join([]string{
		"[ui]",
		`theme = "light"`,
		"",
		`# [themes.light]`,
		`# fg = "#111111"     # frame foreground [populated]`,
		`# dim = "#8A8F8A"   # dim text [populated]`,
		`# [themes.dark]   # Campbell [populated]`,
		`# bg = "#0C0C0C"   # frame background [populated]`,
		"",
	}, "\n")
	if got := fileBody(t, path); got != want {
		t.Fatalf("file after remove:\n%s\nwant:\n%s", got, want)
	}
	if _, err := Load(path, ""); err != nil {
		t.Fatalf("load: %v", err)
	}
}

// A comment of the user's own inside the table (no [populated] marker) must
// not drift into the section above it once the header is gone: the header is
// re-commented so the comment stays inside an inert `# [themes.light]` block.
func TestRemoveThemeTableKeepsUserCommentInsideTheBlock(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	body := "[ui]\ntheme = \"light\"\n\n[themes.light]\n# bg = \"#101010\"   # my experiment\nfg = \"#222222\"\n\n[themes.dark]\nbg = \"#000000\"\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RemoveThemeTable(path, "light"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	want := "[ui]\ntheme = \"light\"\n\n# [themes.light]\n# bg = \"#101010\"   # my experiment\n\n[themes.dark]\nbg = \"#000000\"\n"
	if got := fileBody(t, path); got != want {
		t.Fatalf("file after remove:\n%s\nwant:\n%s", got, want)
	}
	cfg, err := Load(path, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, ok := cfg.Themes["light"]; ok {
		t.Fatal("light table must be inert")
	}
}

// The boundaries the scan must respect: a commented same-name header after the
// active one, a [[tools.command]] block with a multi-line script right after
// the table, and a file that is nothing but the table.
func TestRemoveThemeTableBoundaries(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cases := []struct{ name, body, want string }{
		{"commented-header-after",
			"[themes.light]\nbg = \"#101010\"\n# [themes.light]   # x [populated]\n# bg = \"#E9E9E5\"   # frame background [populated]\n",
			"# [themes.light]   # x [populated]\n# bg = \"#E9E9E5\"   # frame background [populated]\n"},
		{"tools-block-after",
			"[themes.light]\nbg = \"#101010\"\n\n[[tools.command]]\ncategory = \"review\"\nname = \"x\"\nmode = \"capture\"\ncommand = '''\n[ -d x ] && echo hi\n# not a comment line of the table\n'''\n",
			"[[tools.command]]\ncategory = \"review\"\nname = \"x\"\nmode = \"capture\"\ncommand = '''\n[ -d x ] && echo hi\n# not a comment line of the table\n'''\n"},
		{"only-the-table", "[themes.light]\nbg = \"#101010\"\n", ""},
	}
	for _, c := range cases {
		path := filepath.Join(dir, c.name+".toml")
		if err := os.WriteFile(path, []byte(c.body), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := RemoveThemeTable(path, "light"); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := fileBody(t, path); got != c.want {
			t.Fatalf("%s: file after remove:\n%q\nwant:\n%q", c.name, got, c.want)
		}
		if _, err := Load(path, ""); err != nil {
			t.Fatalf("%s: load: %v", c.name, err)
		}
	}
}

func TestRemoveThemeTableNoActiveTableIsNoop(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for name, body := range map[string]string{
		"commented": "[ui]\ntheme = \"light\"\n\n# [themes.light]   # x [populated]\n# bg = \"#E9E9E5\"   # frame background [populated]\n",
		"absent":    "[ui]\ntheme = \"light\"\n",
		"other":     "[themes.dark]\nbg = \"#000000\"\n",
	} {
		path := filepath.Join(dir, name+".toml")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := RemoveThemeTable(path, "light"); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := fileBody(t, path); got != body {
			t.Fatalf("%s: file must be byte-identical:\n%s", name, got)
		}
	}
	if err := RemoveThemeTable(filepath.Join(dir, "missing.toml"), "light"); err != nil {
		t.Fatalf("missing file must be a no-op, got %v", err)
	}
	if err := RemoveThemeTable("", "light"); err == nil {
		t.Fatal("empty path must refuse")
	}
}
