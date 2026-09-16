package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// TestVersionsPopupRendersRows: push a versionsPopup in modeVersions with two
// fabricated model.BranchVersion rows; render; assert the box contains the
// short sha, the translated op label, and the subject.
func TestVersionsPopupRendersRows(t *testing.T) {
	t.Parallel()
	m := Model{width: 120, height: 40}
	p := &versionsPopup{
		mode:   versionsModeVersions,
		branch: "main",
		rows: []model.BranchVersion{
			{Ref: "refs/gg/versions/main/1753100000-rebase", Hash: "a1b2c3d4e5f6", Subject: "did a rebase", Op: "rebase", Unix: 1753100000},
			{Ref: "refs/gg/versions/main/1753100100-merge", Hash: "1122334455667788", Subject: "merged a thing", Op: "merge", Unix: 1753100100},
		},
	}
	out := p.box(m)
	if !strings.Contains(out, "a1b2c3d4") {
		t.Fatalf("box must show the 8-char short sha, got:\n%s", out)
	}
	if !strings.Contains(out, opDisplayName("rebase")) {
		t.Fatalf("box must show the translated op label %q, got:\n%s", opDisplayName("rebase"), out)
	}
	if !strings.Contains(out, "did a rebase") {
		t.Fatalf("box must show the subject, got:\n%s", out)
	}
	if !strings.Contains(out, "11223344") || !strings.Contains(out, "merged a thing") {
		t.Fatalf("box must show the second row, got:\n%s", out)
	}
}

// TestVersionsPopupEnterOpensCompare: a version row with recorded endpoints
// (Base/Ours) must open the files view in compare mode anchored at the
// FROZEN preview — Base/Ours, exactly what domain.VersionPreview would
// resolve for this ref — never the branch's live tip.
func TestVersionsPopupEnterOpensCompare(t *testing.T) {
	t.Parallel()
	m := Model{width: 120, height: 40}
	p := &versionsPopup{
		mode:   versionsModeVersions,
		branch: "main",
		rows: []model.BranchVersion{
			{
				Ref: "refs/gg/versions/main/1753100000-rebase", Hash: "tiphash0000000000",
				Subject: "did a rebase", Op: "rebase", Unix: 1753100000,
				Base: "1111111111111111", Ours: "2222222222222222",
			},
		},
	}
	m = m.pushLayer(p)

	mm, _ := m.Update(keyMsg("enter"))
	m = mm.(Model)

	if m.filesView == nil || !m.inCompareMode() {
		t.Fatal("enter on a version row with recorded endpoints should open the files view in compare mode")
	}
	if m.filesLeft.Hash() != "1111111111111111" {
		t.Fatalf("left endpoint = %q, want the recorded Base (the frozen preview) — never the live tip", m.filesLeft.Hash())
	}
	if m.filesRight.Hash() != "2222222222222222" {
		t.Fatalf("right endpoint = %q, want the recorded Ours", m.filesRight.Hash())
	}
	if layerOf[*versionsPopup](m) != nil {
		t.Fatal("the versions popup should close (clearLayers) once the compare opens")
	}
}

// TestBranchVersionRowTextSingularPlural pins the two-key singular/plural fix
// (the push-tip-tags convention): a branch with exactly one recorded version
// must render "1 version", never the grammatically wrong "1 versions", while
// a branch with more than one keeps the plural form.
func TestBranchVersionRowTextSingularPlural(t *testing.T) {
	t.Parallel()
	one := branchVersionRowText(model.VersionedBranch{Branch: "main", Count: 1})
	if !strings.Contains(one, i18n.T("%d version", 1)) {
		t.Fatalf("Count=1 row = %q, want it to contain the singular form %q", one, i18n.T("%d version", 1))
	}
	if strings.Contains(one, i18n.T("%d versions", 1)) {
		t.Fatalf("Count=1 row = %q, must not use the plural form", one)
	}

	many := branchVersionRowText(model.VersionedBranch{Branch: "main", Count: 3})
	if !strings.Contains(many, i18n.T("%d versions", 3)) {
		t.Fatalf("Count=3 row = %q, want it to contain the plural form %q", many, i18n.T("%d versions", 3))
	}
}

// TestVersionsPopupRestoreOpensModal: pressing 'r' sets m.modal with options
// ["Reset branch","New branch at version","Cancel"].
func TestVersionsPopupRestoreOpensModal(t *testing.T) {
	t.Parallel()
	m := Model{width: 120, height: 40}
	p := &versionsPopup{
		mode:   versionsModeVersions,
		branch: "main",
		rows: []model.BranchVersion{
			{Ref: "refs/gg/versions/main/1753100000-rebase", Hash: "verhash0000000000", Subject: "did a rebase", Op: "rebase", Unix: 1753100000},
		},
	}
	m = m.pushLayer(p)

	mm, _ := m.Update(keyMsg("r"))
	m = mm.(Model)

	if m.modal == nil {
		t.Fatal("r should open the restore-choice modal")
	}
	want := []string{"Reset branch", "New branch at version", "Cancel"}
	got := m.modal.req.Options
	if len(got) != len(want) {
		t.Fatalf("modal options = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("modal options = %v, want %v", got, want)
		}
	}
}

// TestVersionsPopupDeletedBranchFieldlessOpensCommitView: a modeVersions
// popup whose branch is deleted, entering on a fieldless row (a one-branch
// op: no Base/Ours) must still open today's commit view — a frozen snapshot
// (and a fieldless record's own commit) stays reachable via the version ref
// regardless of whether the branch itself still exists, so this no longer
// refuses the way the old live-tip compare had to.
func TestVersionsPopupDeletedBranchFieldlessOpensCommitView(t *testing.T) {
	t.Parallel()
	m := Model{width: 120, height: 40}
	p := &versionsPopup{
		mode:    versionsModeVersions,
		branch:  "gone",
		deleted: true,
		rows: []model.BranchVersion{
			{Ref: "refs/gg/versions/gone/1753100000-restore", Hash: "verhash0000000000", Subject: "did a restore", Op: "restore", Unix: 1753100000},
		},
	}
	m = m.pushLayer(p)

	mm, _ := m.Update(keyMsg("enter"))
	m = mm.(Model)

	if m.filesView == nil || m.inCompareMode() {
		t.Fatal("a fieldless row should open today's commit view, deleted branch or not")
	}
	if m.filesHash != "verhash0000000000" {
		t.Fatalf("filesHash = %q, want the version's own commit hash", m.filesHash)
	}
	if layerOf[*versionsPopup](m) != nil {
		t.Fatal("the versions popup should close once the commit view opens")
	}
}

// TestVersionsPopupDeletedBranchWithEndpointsOpensFrozenPreview: same as
// above, but the row HAS recorded endpoints (a two-branch op, e.g. a rebase
// snapshot) — the frozen preview opens exactly as it would for a live
// branch, since Base/Ours never depended on refs/heads/<branch> existing.
func TestVersionsPopupDeletedBranchWithEndpointsOpensFrozenPreview(t *testing.T) {
	t.Parallel()
	m := Model{width: 120, height: 40}
	p := &versionsPopup{
		mode:    versionsModeVersions,
		branch:  "gone",
		deleted: true,
		rows: []model.BranchVersion{
			{
				Ref: "refs/gg/versions/gone/1753100000-rebase", Hash: "tiphash0000000000",
				Subject: "did a rebase", Op: "rebase", Unix: 1753100000,
				Base: "1111111111111111", Ours: "2222222222222222",
			},
		},
	}
	m = m.pushLayer(p)

	mm, _ := m.Update(keyMsg("enter"))
	m = mm.(Model)

	if m.filesView == nil || !m.inCompareMode() {
		t.Fatal("a deleted branch's frozen preview should still open — it needs no live tip")
	}
	if m.filesLeft.Hash() != "1111111111111111" || m.filesRight.Hash() != "2222222222222222" {
		t.Fatalf("endpoints = %+v/%+v, want the recorded Base/Ours", m.filesLeft, m.filesRight)
	}
}
