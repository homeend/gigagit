package domain

import (
	"context"
	"reflect"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// A note remembers the scope it was written in: a merge preview by its
// branch NAMES (portable, like its gg:// link), a commit pair by its shas.
func TestPreviewNoteSetPair(t *testing.T) {
	t.Parallel()
	pv := PreviewNoteSet{Source: "feat", Target: "main", Tip: "t", Base: "b"}
	if got := pv.Pair(); got != "main...feat" {
		t.Fatalf("merge preview Pair = %q", got)
	}
	pair := PreviewNoteSet{Tip: "bbbbbbbbbbbb", Base: "aaaaaaaaaaaa"}
	if got := pair.Pair(); got != "aaaaaaa..bbbbbbb" {
		t.Fatalf("commit pair Pair = %q", got)
	}
	if got := (PreviewNoteSet{}).Pair(); got != "" {
		t.Fatalf("zero set Pair = %q", got)
	}
}

// NoteCounts names, per commit:path, the scopes its notes were written in —
// what a commit's Notes row shows beside a file the commit does not change.
func TestNoteCountsPreviewsByCommitPath(t *testing.T) {
	t.Parallel()
	svc, dir := newPreviewRepo(t)
	ctx := context.Background()
	tip := revParse(t, dir, "feat")
	add := func(path, preview string) {
		t.Helper()
		if _, err := svc.NoteAdd(ctx, model.Note{
			Source: model.NoteSourceAgent, Author: "ada", Preview: preview,
			Address: model.FileAddress{State: model.StateCommitted, Commit: tip, Path: path},
			Side:    model.NoteSideNew, Range: [2]int{1, 1}, Summary: "s",
		}); err != nil {
			t.Fatal(err)
		}
	}
	add("a.txt", "main...feat")
	add("a.txt", "main...feat") // one name, however many notes
	add("a.txt", "other...feat")
	add("b.txt", "") // written outside a preview
	c, err := svc.NoteCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := c.PreviewsByCommitPath[tip+":a.txt"], []string{"main...feat", "other...feat"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("a.txt previews = %q, want %q", got, want)
	}
	if got := c.PreviewsByCommitPath[tip+":b.txt"]; got != nil {
		t.Fatalf("b.txt previews = %q, want none", got)
	}
}
