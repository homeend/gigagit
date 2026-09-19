package domain

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/savedcompare"
)

// The façade's two conversions are two arms that look alike and must undo
// each other. A round trip alone cannot see a DOUBLE swap, so this asserts
// the link text in the middle: target first.
func TestPreviewEntryConversionPutsTargetFirst(t *testing.T) {
	t.Parallel()
	repo := model.LinkRepo{Name: "gigagit"}
	p := model.MergePreview{ID: "abcd1234", Source: "feat/login", Target: "main", Label: "login work"}

	e, err := entryFromPreview(repo, p)
	if err != nil {
		t.Fatalf("entryFromPreview: %v", err)
	}
	if want := "gg://gigagit@main...feat/login"; e.Left.String() != want {
		t.Fatalf("Left = %q, want %q (target first)", e.Left.String(), want)
	}
	if !e.IsSet() {
		t.Fatal("a preview must convert to a SET, not a pair")
	}

	back, ok := previewFromEntry(e)
	if !ok {
		t.Fatal("previewFromEntry refused a set-shaped entry")
	}
	if back.Source != "feat/login" || back.Target != "main" {
		t.Fatalf("round trip = %+v, want source=feat/login target=main", back)
	}
	if back.ID != p.ID || back.Label != p.Label {
		t.Fatalf("round trip lost id/label: %+v", back)
	}
}

// A PAIR-shaped entry is not a merge preview, and the façade must say so
// rather than invent one. This is the arm that has to DISAGREE.
func TestPreviewFromEntryRefusesAPairShapedEntry(t *testing.T) {
	t.Parallel()
	left, err := model.ParseLink("gg://gigagit@main...feat/login")
	if err != nil {
		t.Fatal(err)
	}
	right, err := model.ParseLink("gg://gigagit@abc1234")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := previewFromEntry(savedcompare.Entry{Left: left, Right: &right}); ok {
		t.Fatal("previewFromEntry accepted a pair-shaped entry as a merge preview")
	}
}

// A set-shaped entry whose left half is NOT a three-dot preview link (a saved
// bounded set from `gg compare --save`) is also not a merge preview.
func TestPreviewFromEntryRefusesANonPreviewSet(t *testing.T) {
	t.Parallel()
	left, err := model.ParseLink("gg://gigagit@abc1234..def5678")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := previewFromEntry(savedcompare.Entry{Left: left}); ok {
		t.Fatal("previewFromEntry accepted a change-set as a merge preview")
	}
}

// THE END-TO-END GUARANTEE the mid-plan bug broke: a preview added through
// the façade, then converted-and-forgotten by nothing, is still listed. Add
// writes the NEW store, so List finds it.
func TestPreviewAddIsVisibleThroughTheFacade(t *testing.T) {
	t.Parallel()
	_, svc := previewRepo(t)
	ctx := context.Background()

	p, err := svc.PreviewAdd(ctx, "feat/x", "main", "")
	if err != nil {
		t.Fatalf("PreviewAdd: %v", err)
	}
	if p.Label != "feat/x → main" {
		t.Fatalf("default label = %q, want the preview vocabulary's own", p.Label)
	}
	ps, err := svc.PreviewList(ctx)
	if err != nil {
		t.Fatalf("PreviewList: %v", err)
	}
	if len(ps) != 1 || ps[0].Source != "feat/x" || ps[0].Target != "main" {
		t.Fatalf("PreviewList = %+v, want the added preview", ps)
	}
}

// A CONVERTED preview is visible too — the case that was dark between Task 6
// and Task 7: the migration deleted previews.toml while PreviewList still
// read it, so every saved preview vanished from every surface.
func TestAConvertedPreviewIsListedByTheFacade(t *testing.T) {
	t.Parallel()
	_, svc := previewRepo(t)
	seedLegacyPreviews(t, svc, legacyPreviewsTOML)
	ctx := context.Background()

	if err := svc.RunAutoMigrations(ctx); err != nil {
		t.Fatalf("RunAutoMigrations: %v", err)
	}
	ps, err := svc.PreviewList(ctx)
	if err != nil {
		t.Fatalf("PreviewList: %v", err)
	}
	if len(ps) != 1 {
		t.Fatalf("PreviewList = %d entries after the conversion, want 1 — the preview went dark", len(ps))
	}
	got := ps[0]
	if got.ID != "abcd1234" || got.Source != "feat/x" || got.Target != "main" || got.Label != "login work" {
		t.Fatalf("converted preview = %+v, want id=abcd1234 source=feat/x target=main label='login work'", got)
	}
	// And it is reachable by the id the user already knows.
	byID, err := svc.PreviewGet(ctx, "abcd1234")
	if err != nil {
		t.Fatalf("PreviewGet by the legacy id: %v", err)
	}
	if byID.Source != "feat/x" {
		t.Fatalf("PreviewGet = %+v", byID)
	}
}

// PreviewRemove must refuse an id that names a saved COMPARISON, or a pair
// could be deleted through a surface that never shows it.
func TestPreviewRemoveRefusesASavedComparison(t *testing.T) {
	t.Parallel()
	_, svc := previewRepo(t)
	ctx := context.Background()
	st := svc.savedCompareStore(ctx)
	if st == nil {
		t.Fatal("no store")
	}
	left, _ := model.ParseLink("gg://gigagit@abc1234")
	right, _ := model.ParseLink("gg://gigagit@def5678")
	e, err := st.Add(savedcompare.Entry{Left: left, Right: &right, Label: "a comparison"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	if err := svc.PreviewRemove(ctx, e.ID); err == nil {
		t.Fatal("PreviewRemove deleted a saved comparison")
	}
	if _, err := st.Get(e.ID); err != nil {
		t.Fatalf("the saved comparison was removed anyway: %v", err)
	}
	if err := svc.PreviewRename(ctx, e.ID, "hijacked"); err == nil {
		t.Fatal("PreviewRename renamed a saved comparison")
	}
}
