package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCompareListIsEmptyArrayNotNull(t *testing.T) {
	e := newTestEnv(t)
	out := e.call(t, "gg_compare_list", map[string]any{})
	saved, ok := out["saved"].([]any)
	if !ok {
		t.Fatalf("saved = %#v, want an ARRAY (null breaks a client that iterates it)", out["saved"])
	}
	if len(saved) != 0 {
		t.Errorf("saved = %v, want empty on a fresh repo", saved)
	}
}

// TestCompareListNamesEveryKind: one row per saved entry, each telling the
// agent WHAT it is — the three kinds are opened differently (a preview and a
// pair are what the note tools' `preview` argument takes; a comparison's two
// links are what gg_compare_links takes), so a list that only printed links
// would leave the caller to re-derive the kind from link grammar.
func TestCompareListNamesEveryKind(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	a := gitRun(t, e.dir, "rev-parse", "HEAD")
	gitRun(t, e.dir, "checkout", "-b", "feat/x")
	if err := os.WriteFile(filepath.Join(e.dir, "a.txt"), []byte("hello\nchanged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, e.dir, "commit", "-am", "change a.txt")
	b := gitRun(t, e.dir, "rev-parse", "HEAD")
	gitRun(t, e.dir, "checkout", "main")

	pv, err := e.svc.PreviewAdd(ctx, "feat/x", "main", "the preview")
	if err != nil {
		t.Fatalf("PreviewAdd: %v", err)
	}
	pr, err := e.svc.PairAdd(ctx, a, b, "the pair")
	if err != nil {
		t.Fatalf("PairAdd: %v", err)
	}
	// The LOCAL form (gg:///abs/…) on one side: its checkout and file ride
	// undivided until located, which is where a description goes wrong.
	left, right := linkTo(e, "/a.txt@"+a), linkTo(e, "/a.txt@"+b)
	cmp, err := e.svc.SavedCompareAdd(ctx, left, right, "the comparison")
	if err != nil {
		t.Fatalf("SavedCompareAdd: %v", err)
	}

	out := e.call(t, "gg_compare_list", map[string]any{})
	byID := map[string]map[string]any{}
	for _, r := range out["saved"].([]any) {
		row := r.(map[string]any)
		byID[row["id"].(string)] = row
	}
	if len(byID) != 3 {
		t.Fatalf("saved = %v, want 3 rows", out["saved"])
	}
	for _, tc := range []struct {
		id, kind, label string
		hasRight        bool
	}{
		{pv.ID, "preview", "the preview", false},
		{pr.ID, "pair", "the pair", false},
		{cmp.ID, "comparison", "the comparison", true},
	} {
		row, ok := byID[tc.id]
		if !ok {
			t.Errorf("%s: id %s missing from %v", tc.kind, tc.id, byID)
			continue
		}
		if row["kind"] != tc.kind || row["label"] != tc.label {
			t.Errorf("row = %v, want kind %q label %q", row, tc.kind, tc.label)
		}
		if row["left"] == "" || row["left_desc"] == "" || row["created"] == "" {
			t.Errorf("%s: left, left_desc and created must be reported, got %v", tc.kind, row)
		}
		if _, has := row["right"]; has != tc.hasRight {
			t.Errorf("%s: right present = %v, want %v (%v)", tc.kind, has, tc.hasRight, row)
		}
	}
	c := byID[cmp.ID]
	if c["left"] != cmp.Left || c["right"] != cmp.Right {
		t.Errorf("comparison links = %v / %v, want the two saved links as stored", c["left"], c["right"])
	}
	if c["left_desc"] != "file: a.txt" || c["right_desc"] != "file: a.txt" {
		t.Errorf("descs = %v / %v, want the file description on both sides", c["left_desc"], c["right_desc"])
	}
}

func TestCompareListIsReadOnly(t *testing.T) {
	e := newTestEnv(t)
	a, ok := e.listTools(t)["gg_compare_list"]
	if !ok {
		t.Fatal("gg_compare_list is not registered")
	}
	if !a.ReadOnlyHint {
		t.Error("gg_compare_list must be annotated read-only")
	}
}
