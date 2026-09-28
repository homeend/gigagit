package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReplaceToolCommandBodiesLeavesTheRestAlone(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.toml")
	in := "# mine\n[ui]\ntheme = \"dark\"\n\n[[tools.command]]\ncategory = \"review\"\nname = \"Claude\"\ncommand = '''\nOLD \\\n  --flag\n'''\n\n[[tools.command]]\ncategory = \"review\"\nname = \"Mine\"\ncommand = '''\nKEEP\n'''\n\n[notes]\nmax_entries = 5\n"
	if err := os.WriteFile(p, []byte(in), 0o644); err != nil {
		t.Fatal(err)
	}
	var seen []string
	n, err := ReplaceToolCommandBodies(p, func(b string) (string, bool) {
		seen = append(seen, b)
		return "NEW", b == "OLD \\\n  --flag"
	})
	got, _ := os.ReadFile(p)
	want := strings.Replace(in, "\nOLD \\\n  --flag\n", "\nNEW\n", 1)
	if err != nil || n != 1 || string(got) != want {
		t.Fatalf("%d %v\n%s", n, err, got)
	}
	if len(seen) != 2 {
		t.Fatalf("bodies seen %q", seen)
	}
	cmds, err := ToolCommandsIn(p)
	if err != nil || len(cmds) != 2 || strings.TrimSpace(cmds[0].Command) != "NEW" {
		t.Fatalf("%+v %v", cmds, err)
	}
}

func TestReplaceToolCommandBodiesWritesNothingWithoutAMatch(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.toml")
	in := "[[tools.command]]\ncategory = \"review\"\ncommand = '''\nKEEP\n'''\n"
	os.WriteFile(p, []byte(in), 0o600)
	before, _ := os.Stat(p)
	n, err := ReplaceToolCommandBodies(p, func(string) (string, bool) { return "", false })
	after, _ := os.Stat(p)
	if err != nil || n != 0 || !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("%d %v", n, err)
	}
	if n, err := ReplaceToolCommandBodies(filepath.Join(t.TempDir(), "none.toml"), func(string) (string, bool) { return "", true }); err != nil || n != 0 {
		t.Fatalf("a missing file is no match: %d %v", n, err)
	}
}

func TestReplaceToolCommandBodiesRefusesADelimiter(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.toml")
	os.WriteFile(p, []byte("[[tools.command]]\ncommand = '''\nOLD\n'''\n"), 0o600)
	if _, err := ReplaceToolCommandBodies(p, func(string) (string, bool) { return "a'''b", true }); err == nil {
		t.Fatal("a body with ''' must be refused")
	}
}
