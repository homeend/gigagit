package notes

import (
	"errors"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/model"
)

// forgeReply is a local draft answer to GitHub thread root PRRC_1.
func forgeReply(id string, sec int) model.Note {
	n := commitNote(id, "", sec)
	n.ParentID = model.ForgeNoteIDPrefix + "PRRC_1"
	return n
}

func TestForgeRootedReplySurvivesMutations(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir()) // uncapped: only the orphan prune is under test
	for _, n := range []model.Note{forgeReply("r0000001", 1), commitNote("c0000001", "", 2), commitNote("c0000002", "", 3)} {
		if err := fs.Put(n); err != nil {
			t.Fatal(err)
		}
	}
	if err := fs.Remove("c0000001"); err != nil { // another note's removal runs the orphan prune
		t.Fatal(err)
	}
	all, err := fs.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range all {
		found = found || n.ID == "r0000001"
	}
	if !found {
		t.Fatalf("the draft reply to a GitHub thread was pruned: %+v", all)
	}
}

func TestForgeRootedReplyCountsAsItsOwnThreadUnderTheCap(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	fs.SetPolicy(Policy{MaxEntries: 2})
	for _, n := range []model.Note{forgeReply("r0000001", 1), commitNote("c0000001", "", 2), commitNote("c0000002", "", 3)} {
		if err := fs.Put(n); err != nil {
			t.Fatal(err)
		}
	}
	all, _ := fs.LoadAll()
	if len(all) != 2 {
		t.Fatalf("cap kept %d records, want 2", len(all))
	}
	for _, n := range all {
		if n.ID == "r0000001" {
			t.Fatalf("the OLDEST thread (the draft reply) should have gone first: %+v", all)
		}
	}
}

func TestEditIsAtomicPerRecord(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	if err := fs.Put(commitNote("c0000001", "", 1)); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	if err := fs.Edit("c0000001", func(n *model.Note) error {
		n.Send = &model.NoteSend{PR: 7, Review: "PRR_1", At: at}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	all, _ := fs.LoadAll()
	if len(all) != 1 || all[0].Send == nil || all[0].Send.Review != "PRR_1" || !all[0].Send.At.Equal(at) {
		t.Fatalf("after Edit: %+v", all)
	}
	if err := fs.Edit("nope", func(*model.Note) error { return nil }); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Edit of a missing id = %v", err)
	}
	boom := errors.New("boom")
	if err := fs.Edit("c0000001", func(n *model.Note) error { n.Summary = "x"; return boom }); !errors.Is(err, boom) {
		t.Fatalf("Edit's own error = %v", err)
	}
	if all, _ := fs.LoadAll(); all[0].Summary == "x" {
		t.Fatal("a failed edit was written")
	}
}
