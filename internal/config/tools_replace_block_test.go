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
