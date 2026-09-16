package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/branchfilter"
)

func TestBranchFiltersDecodeAndOverlayBySlot(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	global := writeCfg(t, dir, "global.toml", `
[[branches.filter]]
slot = 1
name = "stale"
older_than = "90d"

[[branches.filter]]
slot = 2
name = "feat"
mode = "show"
prefix = "feat/"
`)
	repo := writeCfg(t, dir, "repo.toml", `
[[branches.filter]]
slot = 2
name = "repo-feat"
prefix = "feature/"
`)
	cfg, err := Load(global, repo)
	if err != nil {
		t.Fatal(err)
	}
	all, _ := branchfilter.CompileAll(cfg.Branches.Filter)
	if all[0].Name != "stale" || all[0].OlderThan != "90d" {
		t.Errorf("slot 1 should fall through from global: %+v", all[0].Slot)
	}
	if all[1].Name != "repo-feat" || all[1].Mode != branchfilter.ModeHide || all[1].Prefix != "feature/" {
		t.Errorf("slot 2 should be replaced WHOLE by the repo block: %+v", all[1].Slot)
	}
	if !all[2].Empty {
		t.Errorf("slot 3 should be empty")
	}
}

func TestBranchFiltersMissingSectionIsEmpty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := writeCfg(t, dir, "g.toml", "[ui]\nwheel_step = 2\n")
	cfg, err := Load(p, filepath.Join(dir, "absent.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Branches.Filter) != 0 {
		t.Errorf("Filter = %v; want none", cfg.Branches.Filter)
	}
}

func TestSetBranchFilterAppendsThenReplacesInPlace(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := writeCfg(t, dir, "repo.toml", "[ui]\nwheel_step = 2\n\n# trailing comment\n")
	if err := SetBranchFilter(p, branchfilter.Slot{Slot: 2, Name: "feat", Mode: "show", Prefix: "feat/"}); err != nil {
		t.Fatal(err)
	}
	if err := SetBranchFilter(p, branchfilter.Slot{Slot: 4, Name: "old", OlderThan: "90d", Regex: `^x"y$`}); err != nil {
		t.Fatal(err)
	}
	// Replace slot 2; slot 4 and the original lines must survive byte-for-byte.
	if err := SetBranchFilter(p, branchfilter.Slot{Slot: 2, Name: "feat2", Prefix: "feature/", Suffix: "-x"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(p)
	got := string(raw)
	if !strings.HasPrefix(got, "[ui]\nwheel_step = 2\n\n# trailing comment\n") {
		t.Errorf("original content not preserved:\n%s", got)
	}
	if strings.Count(got, "[[branches.filter]]") != 2 {
		t.Errorf("want exactly two blocks:\n%s", got)
	}
	if strings.Contains(got, `name = "feat"`) || !strings.Contains(got, `name = "feat2"`) {
		t.Errorf("slot 2 not replaced:\n%s", got)
	}
	if strings.Index(got, "slot = 2") > strings.Index(got, "slot = 4") {
		t.Errorf("replaced block moved:\n%s", got)
	}
	cfg, err := Load(p, filepath.Join(dir, "absent.toml"))
	if err != nil {
		t.Fatalf("written file must decode: %v\n%s", err, got)
	}
	all, _ := branchfilter.CompileAll(cfg.Branches.Filter)
	if all[1].Prefix != "feature/" || all[1].Suffix != "-x" || all[1].Mode != branchfilter.ModeHide {
		t.Errorf("slot 2 = %+v", all[1].Slot)
	}
	if all[3].Regex != `^x"y$` || all[3].OlderThan != "90d" {
		t.Errorf("slot 4 = %+v (quoting broke?)", all[3].Slot)
	}
}

func TestSetBranchFilterRefusesEmptyPathAndBadSlot(t *testing.T) {
	t.Parallel()
	if err := SetBranchFilter("", branchfilter.Slot{Slot: 1, Prefix: "x"}); err == nil {
		t.Error("empty path accepted")
	}
	p := filepath.Join(t.TempDir(), "c.toml")
	if err := SetBranchFilter(p, branchfilter.Slot{Slot: 9, Prefix: "x"}); err == nil {
		t.Error("slot 9 accepted")
	}
	if err := SetBranchFilter(p, branchfilter.Slot{Slot: 1, Regex: "("}); err == nil {
		t.Error("bad regex accepted by the writer")
	}
	if err := SetBranchFilter(p, branchfilter.Slot{Slot: 1, Prefix: "a\x01"}); err == nil {
		t.Error("control character accepted by the writer")
	}
	if _, err := os.Stat(p); err == nil {
		t.Error("a refused write created the file")
	}
}

func TestBranchFilterScopes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	g := writeCfg(t, dir, "g.toml", "[[branches.filter]]\nslot = 1\nprefix = \"a\"\n\n[[branches.filter]]\nslot = 2\nprefix = \"b\"\n")
	r := writeCfg(t, dir, "r.toml", "[[branches.filter]]\nslot = 2\nprefix = \"c\"\n")
	if ig, ir := BranchFilterScopes(g, r, 1); !ig || ir {
		t.Errorf("slot 1: global=%v repo=%v", ig, ir)
	}
	if ig, ir := BranchFilterScopes(g, r, 2); !ig || !ir {
		t.Errorf("slot 2: global=%v repo=%v", ig, ir)
	}
	if ig, ir := BranchFilterScopes(g, r, 3); ig || ir {
		t.Errorf("slot 3: global=%v repo=%v", ig, ir)
	}
	if ig, ir := BranchFilterScopes(g, "", 1); !ig || ir {
		t.Errorf("empty repo path: global=%v repo=%v", ig, ir)
	}
}

func TestRemoveBranchFilter(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := writeCfg(t, dir, "repo.toml", "[ui]\nwheel_step = 2\n")
	_ = SetBranchFilter(p, branchfilter.Slot{Slot: 1, Prefix: "a"})
	_ = SetBranchFilter(p, branchfilter.Slot{Slot: 3, Prefix: "c"})
	if err := RemoveBranchFilter(p, 1); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(p)
	got := string(raw)
	if strings.Contains(got, "slot = 1") || !strings.Contains(got, "slot = 3") {
		t.Errorf("wrong block removed:\n%s", got)
	}
	if strings.Count(got, "[[branches.filter]]") != 1 {
		t.Errorf("want one block left:\n%s", got)
	}
	if strings.Contains(got, "\n\n\n") {
		t.Errorf("removal left a double blank line:\n%s", got)
	}
	if err := RemoveBranchFilter(p, 5); err != nil {
		t.Errorf("removing an absent slot must be a no-op, got %v", err)
	}
	if err := RemoveBranchFilter(filepath.Join(dir, "absent.toml"), 1); err != nil {
		t.Errorf("missing file must be a no-op, got %v", err)
	}
}

func TestBranchFilterSettingDocPresent(t *testing.T) {
	t.Parallel()
	tpl := Template()
	if !strings.Contains(tpl, "[branches]") || !strings.Contains(tpl, "[[branches.filter]]") {
		t.Errorf("template lacks the branch-filter doc row:\n%s", tpl)
	}
}

// A trailing commented `# slot = …` line (left behind by a hand edit, or by
// an older gg write) must never be mistaken for the block's real slot: only
// the first ACTIVE `slot = N` assignment counts. Regression for a bug where
// the commented line re-triggered the match and overwrote the tracked slot,
// so SetBranchFilter/RemoveBranchFilter targeted the wrong (or no) span.
func TestSetBranchFilterIgnoresTrailingCommentedSlotLine(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := writeCfg(t, dir, "repo.toml", "[[branches.filter]]\nslot = 2\nprefix = \"x\"\n# slot = 1  (old)\n")
	if err := SetBranchFilter(p, branchfilter.Slot{Slot: 2, Prefix: "y"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(p)
	got := string(raw)
	if strings.Count(got, "[[branches.filter]]") != 1 {
		t.Errorf("want exactly one block (replaced in place, not appended):\n%s", got)
	}
	if strings.Contains(got, `prefix = "x"`) || !strings.Contains(got, `prefix = "y"`) {
		t.Errorf("slot 2 not replaced in place:\n%s", got)
	}
	cfg, err := Load(p, filepath.Join(dir, "absent.toml"))
	if err != nil {
		t.Fatalf("written file must decode: %v\n%s", err, got)
	}
	all, _ := branchfilter.CompileAll(cfg.Branches.Filter)
	if all[1].Prefix != "y" {
		t.Errorf("slot 2 = %+v", all[1].Slot)
	}
}

func TestRemoveBranchFilterIgnoresTrailingCommentedSlotLine(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := writeCfg(t, dir, "repo.toml", "[[branches.filter]]\nslot = 2\nprefix = \"x\"\n# slot = 1  (old)\n")
	if err := RemoveBranchFilter(p, 2); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(p)
	got := string(raw)
	if strings.Contains(got, "[[branches.filter]]") {
		t.Errorf("block not removed:\n%s", got)
	}
}
