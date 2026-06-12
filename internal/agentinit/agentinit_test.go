package agentinit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/agentskill"
)

// fixture creates proj+home dirs with the named agent-detect paths present.
func fixture(t *testing.T, projPaths, homePaths []string) (string, string) {
	t.Helper()
	proj, home := t.TempDir(), t.TempDir()
	for _, p := range projPaths {
		full := filepath.Join(proj, p)
		if filepath.Ext(p) == ".md" || strings.HasSuffix(p, "rules") {
			os.MkdirAll(filepath.Dir(full), 0o755)
			os.WriteFile(full, []byte("existing\n"), 0o644)
		} else {
			os.MkdirAll(full, 0o755)
		}
	}
	for _, p := range homePaths {
		full := filepath.Join(home, p)
		os.MkdirAll(full, 0o755)
	}
	return proj, home
}

func byID(dets []Detection, id string) (Detection, bool) {
	for _, d := range dets {
		if d.Agent.ID == id {
			return d, true
		}
	}
	return Detection{}, false
}

func TestDetectFindsOnlyPresentAgents(t *testing.T) {
	proj, home := fixture(t, []string{".claude", ".junie"}, []string{".claude"})
	dets := Detect(proj, home)
	for _, want := range []string{"claude-project", "claude-global", "junie"} {
		if _, ok := byID(dets, want); !ok {
			t.Errorf("missing detection %q in %+v", want, dets)
		}
	}
	if _, ok := byID(dets, "codex"); ok {
		t.Error("codex must not be detected without ~/.codex")
	}
	if _, ok := byID(dets, "cursor"); ok {
		t.Error("cursor must not be detected without .cursor")
	}
}

func TestDetectEmptyHomeSkipsHomeAgents(t *testing.T) {
	proj, _ := fixture(t, []string{".claude"}, nil)
	dets := Detect(proj, "")
	if _, ok := byID(dets, "claude-project"); !ok {
		t.Error("project agent should be detected")
	}
	if _, ok := byID(dets, "claude-global"); ok {
		t.Error("home agents must be skipped when homeDir is empty (hermeticity)")
	}
}

func TestStatusLifecycle(t *testing.T) {
	proj, home := fixture(t, []string{".claude"}, nil)
	dets := Detect(proj, home)
	d, ok := byID(dets, "claude-project")
	if !ok {
		t.Fatal("claude-project not detected")
	}
	if d.Status != StatusNew {
		t.Fatalf("fresh target should be StatusNew, got %v", d.Status)
	}
	if err := Install(d); err != nil {
		t.Fatal(err)
	}
	d2, _ := byID(Detect(proj, home), "claude-project")
	if d2.Status != StatusUpToDate {
		t.Fatalf("after install: %v, want StatusUpToDate", d2.Status)
	}
	// Simulate an older install: stamp v0… by writing an old marker.
	if err := os.WriteFile(d.Target, []byte("<!-- gg:using-gg:v0 -->\nold"), 0o644); err != nil {
		t.Fatal(err)
	}
	d3, _ := byID(Detect(proj, home), "claude-project")
	if d3.Status != StatusOutdated {
		t.Fatalf("old marker: %v, want StatusOutdated", d3.Status)
	}
}

func TestInstallWholeFileCreatesParents(t *testing.T) {
	proj, home := fixture(t, []string{".claude"}, nil)
	d, _ := byID(Detect(proj, home), "claude-project")
	if err := Install(d); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(proj, ".claude", "skills", "using-gg", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "name: using-gg") {
		t.Error("SKILL.md missing frontmatter")
	}
}

func TestInstallBlockPreservesSurroundingContent(t *testing.T) {
	proj, home := fixture(t, []string{"AGENTS.md"}, nil)
	target := filepath.Join(proj, "AGENTS.md")
	if err := os.WriteFile(target, []byte("# My rules\n\nkeep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d, ok := byID(Detect(proj, home), "agents-md")
	if !ok {
		t.Fatal("agents-md not detected")
	}
	if err := Install(d); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(target)
	s := string(data)
	if !strings.Contains(s, "keep me") {
		t.Error("surrounding content lost")
	}
	if agentskill.InstalledVersion(data) != agentskill.Version {
		t.Error("block not stamped with current version")
	}

	// Replace an OLD block in place: surrounding bytes stay identical.
	old := "# My rules\n\nkeep me\n\n<!-- gg:using-gg:v0:begin -->\n\nancient\n<!-- gg:using-gg:end -->\n\ntail stays\n"
	if err := os.WriteFile(target, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	d2, _ := byID(Detect(proj, home), "agents-md")
	if d2.Status != StatusOutdated {
		t.Fatalf("status = %v, want outdated", d2.Status)
	}
	if err := Install(d2); err != nil {
		t.Fatal(err)
	}
	data2, _ := os.ReadFile(target)
	s2 := string(data2)
	if !strings.HasPrefix(s2, "# My rules\n\nkeep me\n\n") || !strings.HasSuffix(s2, "\n\ntail stays\n") {
		t.Errorf("surrounding bytes changed:\n%s", s2)
	}
	if strings.Contains(s2, "ancient") {
		t.Error("old block content not replaced")
	}
	if strings.Count(s2, "gg:using-gg") != 2 { // one begin + one end marker
		t.Errorf("expected exactly one block, got:\n%s", s2)
	}
}

func TestInstallBlockCreatesMissingFile(t *testing.T) {
	proj, home := fixture(t, []string{".junie"}, nil)
	d, ok := byID(Detect(proj, home), "junie")
	if !ok {
		t.Fatal("junie not detected")
	}
	if err := Install(d); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(proj, ".junie", "guidelines.md"))
	if err != nil {
		t.Fatal(err)
	}
	if agentskill.InstalledVersion(data) != agentskill.Version {
		t.Error("created file missing block")
	}
}

func TestInstallIdempotent(t *testing.T) {
	proj, home := fixture(t, []string{"AGENTS.md"}, nil)
	d, _ := byID(Detect(proj, home), "agents-md")
	if err := Install(d); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(d.Target)
	d2, _ := byID(Detect(proj, home), "agents-md")
	if err := Install(d2); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(d.Target)
	if string(first) != string(second) {
		t.Error("double install must be byte-identical")
	}
}

func TestCheckedDefaults(t *testing.T) {
	if StatusNew.Checked() {
		t.Error("new targets must default unchecked")
	}
	if !StatusUpToDate.Checked() || !StatusOutdated.Checked() {
		t.Error("installed targets must default checked")
	}
}
