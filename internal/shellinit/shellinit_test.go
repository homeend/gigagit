package shellinit

import (
	"strings"
	"testing"
)

func TestScriptPosix(t *testing.T) {
	for _, sh := range []string{"bash", "zsh"} {
		s, err := Script(sh)
		if err != nil {
			t.Fatalf("%s: %v", sh, err)
		}
		if !strings.Contains(s, "command gg --cwd-file") {
			t.Errorf("%s: wrapper must invoke `command gg --cwd-file`:\n%s", sh, s)
		}
		if !strings.Contains(s, "gg()") {
			t.Errorf("%s: wrapper must define gg():\n%s", sh, s)
		}
		if !strings.Contains(s, "cd ") {
			t.Errorf("%s: wrapper must cd:\n%s", sh, s)
		}
	}
}

func TestScriptFish(t *testing.T) {
	s, err := Script("fish")
	if err != nil {
		t.Fatalf("fish: %v", err)
	}
	if !strings.Contains(s, "function gg") {
		t.Errorf("fish wrapper must define `function gg`:\n%s", s)
	}
	if !strings.Contains(s, "command gg --cwd-file") {
		t.Errorf("fish wrapper must invoke `command gg --cwd-file`:\n%s", s)
	}
	if !strings.Contains(s, "cd (cat") {
		t.Errorf("fish wrapper must `cd (cat ...)`:\n%s", s)
	}
}

func TestScriptUnknownShell(t *testing.T) {
	if _, err := Script("powershell"); err == nil {
		t.Fatal("unknown shell should error")
	}
}
