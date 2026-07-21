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

// TestVersionsPopupEnterOpensCompare: fixture with m.branches containing the
// branch tip; enter on a row must set filesMode compare with both endpoints
// resolved to HASHES (assert m.filesLeft/filesRight Endpoint hashes — never
// a branch name).
func TestVersionsPopupEnterOpensCompare(t *testing.T) {
	m := Model{width: 120, height: 40}
	m.branches = []model.Branch{{Name: "main", Hash: "tiphash0000000000"}}
	p := &versionsPopup{
		mode:   versionsModeVersions,
		branch: "main",
		rows: []model.BranchVersion{
			{Ref: "refs/gg/versions/main/1753100000-rebase", Hash: "verhash0000000000", Subject: "did a rebase", Op: "rebase", Unix: 1753100000},
		},
	}
	m = m.pushLayer(p)

	mm, _ := m.Update(keyMsg("enter"))
	m = mm.(Model)

	if m.filesView == nil || !m.inCompareMode() {
		t.Fatal("enter on a version row should open the files view in compare mode")
	}
	if m.filesLeft.Hash != "verhash0000000000" {
		t.Fatalf("left endpoint = %q, want the version's hash (never a branch name)", m.filesLeft.Hash)
	}
	if m.filesRight.Hash != "tiphash0000000000" {
		t.Fatalf("right endpoint = %q, want the branch tip's hash (never a branch name)", m.filesRight.Hash)
	}
	if layerOf[*versionsPopup](m) != nil {
		t.Fatal("the versions popup should close (clearLayers) once the compare opens")
	}
}

// TestVersionsPopupRestoreOpensModal: pressing 'r' sets m.modal with options
// ["Reset branch","New branch at version","Cancel"].
func TestVersionsPopupRestoreOpensModal(t *testing.T) {
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

// TestVersionsPopupDeletedBranchDisablesCompare: a modeVersions popup whose
// branch is deleted (no tip): enter sets a statusMsg instead of a compare.
func TestVersionsPopupDeletedBranchDisablesCompare(t *testing.T) {
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

	if m.filesView != nil {
		t.Fatal("a deleted branch has no tip to compare against — the files view must not open")
	}
	want := i18n.T("branch no longer exists — restore it to compare")
	if m.statusMsg != want {
		t.Fatalf("statusMsg = %q, want %q", m.statusMsg, want)
	}
	if layerOf[*versionsPopup](m) == nil {
		t.Fatal("the popup should stay open on a refused compare")
	}
}
