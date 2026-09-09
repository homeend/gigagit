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
