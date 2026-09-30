package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const replaceFixture = `# my tools
[ui]
theme = "dark"

[[tools.command]]
category = "review"
name = "A"
mode = "capture"
frontends = ["web"]
command = '''
old a
[not a header]
'''

# keep me
[[tools.command]]
category = "review"
name = "B"
mode = "capture"
command = '''
b
'''
`

func writeFixture(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "c.toml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReplaceToolCommandRewritesOnlyThatBlock(t *testing.T) {
	t.Parallel()
	path := writeFixture(t, replaceFixture)
	nb := ToolCommand{Category: "review", Name: "A", Mode: "capture", Frontends: []string{"tui", "web"}, Command: "new a", TemplateVersion: 2}
	ok, err := ReplaceToolCommand(path, nb.Key(), nb)
	if err != nil || !ok {
		t.Fatalf("replace: %v %v", ok, err)
	}
	raw, _ := os.ReadFile(path)
	s := string(raw)
	for _, want := range []string{"# my tools", "theme = \"dark\"", "# keep me", "name = \"B\"", "new a", "template_version = 2"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
	if strings.Contains(s, "old a") || strings.Contains(s, "[not a header]") {
		t.Errorf("old body survived:\n%s", s)
	}
	got, _ := ToolCommandsIn(path)
	if len(got) != 2 || got[0].Name != "A" || got[0].Edited() || strings.TrimSpace(got[1].Command) != "b" {
		t.Fatalf("decoded: %+v", got)
	}
}

func TestReplaceToolCommandKeepsCRLF(t *testing.T) {
	t.Parallel()
	path := writeFixture(t, strings.ReplaceAll(replaceFixture, "\n", "\r\n"))
	nb := ToolCommand{Category: "review", Name: "A", Mode: "capture", Command: "new a", TemplateVersion: 2}
	if ok, err := ReplaceToolCommand(path, nb.Key(), nb); err != nil || !ok {
		t.Fatalf("replace: %v %v", ok, err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(strings.ReplaceAll(string(raw), "\r\n", ""), "\n") {
		t.Fatal("a bare LF crept into a CRLF file")
	}
	got, _ := ToolCommandsIn(path)
	if len(got) != 2 || got[0].Edited() {
		t.Fatalf("CRLF block must read back as unedited: %+v", got)
	}
}

func TestReplaceToolCommandRefusesDelimiterAndMissing(t *testing.T) {
	t.Parallel()
	path := writeFixture(t, replaceFixture)
	bad := ToolCommand{Category: "review", Name: "A", Command: "x ''' y"}
	if _, err := ReplaceToolCommand(path, bad.Key(), bad); err == nil {
		t.Fatal("a ''' body must be refused")
	}
	miss := ToolCommand{Category: "review", Name: "Z", Command: "z"}
	if ok, err := ReplaceToolCommand(path, miss.Key(), miss); ok || err != nil {
		t.Fatalf("missing key: %v %v", ok, err)
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != replaceFixture {
		t.Fatal("a refused/missed replace must not touch the file")
	}
}

// Hand-written blocks close or open their strings in every place TOML
// allows. A replace must never lose a byte outside the block — and when it
// cannot find the block it must say so, not report success.
func TestReplaceToolCommandNeverLosesOtherTables(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"close on content line": "[[tools.command]]\ncategory = \"review\"\nname = \"A\"\nmode = \"capture\"\ncommand = '''\nold a'''\n\n[ui]\ntheme = \"dark\"\n",
		"indented close":        "[[tools.command]]\ncategory = \"review\"\nname = \"A\"\nmode = \"capture\"\ncommand = '''\nold a\n  '''\n\n[ui]\ntheme = \"dark\"\n",
		"close with comment":    "[[tools.command]]\ncategory = \"review\"\nname = \"A\"\nmode = \"capture\"\ncommand = '''\nold a\n''' # c\n\n[ui]\ntheme = \"dark\"\n",
		"basic multiline":       "[[tools.command]]\ncategory = \"review\"\nname = \"A\"\nmode = \"capture\"\ncommand = \"\"\"\n[x]\nold a\n\"\"\"\n\n[ui]\ntheme = \"dark\"\n",
		"content on open line":  "[[tools.command]]\ncategory = \"review\"\nname = \"A\"\nmode = \"capture\"\ncommand = '''old\n[x]\n'''\n\n[ui]\ntheme = \"dark\"\n",
		"header comment":        "[[tools.command]] # mine\ncategory = \"review\"\nname = \"A\"\nmode = \"capture\"\ncommand = \"old a\"\n\n[ui]\ntheme = \"dark\"\n",
		"spaced header":         "[[ tools.command ]]\ncategory = \"review\"\nname = \"A\"\nmode = \"capture\"\ncommand = \"old a\"\n\n[ui]\ntheme = \"dark\"\n",
	}
	for name, in := range cases {
		path := writeFixture(t, in)
		nb := ToolCommand{Category: "review", Name: "A", Mode: "capture", Command: "new a", TemplateVersion: 2}
		ok, err := ReplaceToolCommand(path, nb.Key(), nb)
		raw, _ := os.ReadFile(path)
		if !strings.Contains(string(raw), `theme = "dark"`) {
			t.Errorf("%s: [ui] lost:\n%s", name, raw)
			continue
		}
		if err == nil && !ok {
			t.Errorf("%s: silent no-op (ok=false, err=nil)", name)
			continue
		}
		if ok {
			got, derr := ToolCommandsIn(path)
			if derr != nil || len(got) != 1 || strings.TrimSpace(got[0].Command) != "new a" {
				t.Errorf("%s: after replace: %v %+v\n%s", name, derr, got, raw)
			}
		}
	}
}

// The safety net on its own: a result that loses a table, changes another
// tool block, or drops a block is refused.
func TestSameOutsideToolBlockRefusesLoss(t *testing.T) {
	t.Parallel()
	before := "[[tools.command]]\nname = \"A\"\ncommand = \"a\"\n\n[[tools.command]]\nname = \"B\"\ncommand = \"b\"\n\n[ui]\ntheme = \"dark\"\n"
	ok := strings.Replace(before, `command = "a"`, `command = "new"`, 1)
	if err := sameOutsideToolBlock([]byte(before), []byte(ok), 0); err != nil {
		t.Fatalf("a change inside block 0 must pass: %v", err)
	}
	for name, after := range map[string]string{
		"lost table":      strings.Replace(before, "\n[ui]\ntheme = \"dark\"\n", "", 1),
		"other block":     strings.Replace(before, `command = "b"`, `command = "x"`, 1),
		"dropped a block": strings.Replace(before, "[[tools.command]]\nname = \"B\"\ncommand = \"b\"\n\n", "", 1),
	} {
		if err := sameOutsideToolBlock([]byte(before), []byte(after), 0); err == nil {
			t.Errorf("%s: must be refused", name)
		}
	}
}
