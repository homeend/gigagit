package config

import (
	"path/filepath"
	"testing"
)

func TestToolFingerprintIgnoresFormatting(t *testing.T) {
	t.Parallel()
	a := ToolCommand{Mode: "capture", Frontends: []string{"tui", "web"}, Command: "claude -p \\\n  \"x  y\"\n"}
	b := ToolCommand{Mode: "capture", Frontends: []string{"web", "tui"}, Command: "claude   -p \\\r\n\r\n\t\"x y\"  "}
	if ToolFingerprint(a) != ToolFingerprint(b) {
		t.Fatal("formatting-only differences must fingerprint equal")
	}
	c := a
	c.Command = "claude -p --dangerously-skip-permissions"
	if ToolFingerprint(a) == ToolFingerprint(c) {
		t.Fatal("a flag change must change the fingerprint")
	}
	d := a
	d.Frontends = []string{"web"}
	if ToolFingerprint(a) == ToolFingerprint(d) {
		t.Fatal("a frontends change must change the fingerprint")
	}
}

func TestAppendWritesStampThatRoundTrips(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	in := ToolCommand{Category: "review", Name: "R", Mode: "capture", Command: "x --y", TemplateVersion: 3, AgentRange: ">=2.1"}
	if err := AppendToolCommands(path, []ToolCommand{in}); err != nil {
		t.Fatal(err)
	}
	got, err := ToolCommandsIn(path)
	if err != nil || len(got) != 1 {
		t.Fatalf("read back: %v %+v", err, got)
	}
	tc := got[0]
	if tc.TemplateVersion != 3 || tc.AgentRange != ">=2.1" || tc.Fingerprint == "" {
		t.Fatalf("stamp lost: %+v", tc)
	}
	if tc.Edited() {
		t.Fatal("a freshly written block must read as not edited")
	}
}

func TestUnstampedBlockHasNoStampKeys(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := AppendToolCommands(path, []ToolCommand{{Category: "review", Name: "U", Mode: "capture", Command: "x"}}); err != nil {
		t.Fatal(err)
	}
	got, _ := ToolCommandsIn(path)
	if got[0].Stamped() || !got[0].Edited() {
		t.Fatalf("hand-authored block: %+v", got[0])
	}
}
