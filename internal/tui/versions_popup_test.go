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

// TestVersionsPopupEnterOpensCompare: fixture with m.branches containing the
// branch tip; enter on a row must set filesMode compare with both endpoints
// resolved to HASHES (assert m.filesLeft/filesRight Endpoint hashes — never
// a branch name).
func TestVersionsPopupEnterOpensCompare(t *testing.T) {
	t.Parallel()
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

// TestVersionsPopupDeletedBranchDisablesCompare: a modeVersions popup whose
// branch is deleted (no tip): enter sets a statusMsg instead of a compare.
func TestVersionsPopupDeletedBranchDisablesCompare(t *testing.T) {
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

// TestVersionsPopupMissingFromBranchesDisablesCompare: branch is NOT marked
// deleted (p.deleted == false, e.g. a stale/incomplete m.branches load), but
// it is absent from m.branches — branchTipHash's documented fallback returns
// the branch NAME itself in that case (branch_compare.go). onEnter must
// detect that fallback (resolved tip == the branch name, not a hash) and
// refuse the compare exactly like the deleted case, rather than letting a
// name slip into Endpoint.Hash and poison the immutable diff cache.
func TestVersionsPopupMissingFromBranchesDisablesCompare(t *testing.T) {
	t.Parallel()
	m := Model{width: 120, height: 40}
	m.branches = []model.Branch{{Name: "other", Hash: "otherhash00000000"}} // "main" absent
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

	if m.filesView != nil {
		t.Fatal("branch missing from m.branches has no resolvable tip — the files view must not open")
	}
	want := i18n.T("branch no longer exists — restore it to compare")
	if m.statusMsg != want {
		t.Fatalf("statusMsg = %q, want %q", m.statusMsg, want)
	}
	if layerOf[*versionsPopup](m) == nil {
		t.Fatal("the popup should stay open on a refused compare")
	}
}
