package agentinit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/agentskill"
)

func TestResolveCustomFileBecomesBlockTarget(t *testing.T) {
	p := filepath.Join(t.TempDir(), "instructions.md")
	ct := ResolveCustom(p)
	if ct.Path != p || ct.Mode != "block" {
		t.Fatalf("file path should resolve to a block target at itself, got %+v", ct)
	}
}

func TestResolveCustomExistingDirBecomesSkillFile(t *testing.T) {
	dir := t.TempDir()
	ct := ResolveCustom(dir)
	want := filepath.Join(dir, "using-gg", "SKILL.md")
	if ct.Path != want || ct.Mode != "skill" {
		t.Fatalf("dir should resolve to %s (skill), got %+v", want, ct)
	}
}

func TestResolveCustomTrailingSeparatorMeansDir(t *testing.T) {
	raw := filepath.Join(t.TempDir(), "notyet") + string(os.PathSeparator)
	ct := ResolveCustom(raw)
	if ct.Mode != "skill" || !strings.HasSuffix(ct.Path, filepath.Join("using-gg", "SKILL.md")) {
		t.Fatalf("trailing separator should mean directory intent, got %+v", ct)
	}
}

func TestCustomTargetsRoundTripAndMissingFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "agent-targets.toml")
	ts, err := LoadCustomTargets(file)
	if err != nil || len(ts) != 0 {
		t.Fatalf("missing file must load empty: %v %v", ts, err)
	}
	a := CustomTarget{Path: "/x/AGENTS.md", Mode: "block"}
	b := CustomTarget{Path: "/y/using-gg/SKILL.md", Mode: "skill"}
	if err := AddCustomTarget(file, a); err != nil {
		t.Fatal(err)
	}
	if err := AddCustomTarget(file, b); err != nil {
		t.Fatal(err)
	}
	if err := AddCustomTarget(file, a); err != nil { // dedupe by path
		t.Fatal(err)
	}
	ts, err = LoadCustomTargets(file)
	if err != nil || len(ts) != 2 {
		t.Fatalf("want 2 deduped targets, got %v %v", ts, err)
	}
}

func TestCustomDetectionsStatusAndInstall(t *testing.T) {
	dir := t.TempDir()
	ct := ResolveCustom(dir) // skill mode at dir/using-gg/SKILL.md
	dets := CustomDetections([]CustomTarget{ct})
	if len(dets) != 1 || dets[0].Status != StatusNew || dets[0].Agent.Label != "Custom" {
		t.Fatalf("fresh custom target should be a new Custom detection, got %+v", dets)
	}
	if err := Install(dets[0]); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(ct.Path)
	if err != nil || agentskill.InstalledVersion(data) != agentskill.Version {
		t.Fatalf("install should write the current skill: %v", err)
	}
	dets = CustomDetections([]CustomTarget{ct})
	if dets[0].Status != StatusUpToDate {
		t.Fatalf("installed custom target should be up to date, got %v", dets[0].Status)
	}
}
