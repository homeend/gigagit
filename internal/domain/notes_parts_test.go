package domain

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notes"
)

// partsStore records which parts every read touched.
type partsStore struct {
	notes.Store
	mu    sync.Mutex
	parts []notes.Part
	all   int
}

func (p *partsStore) Load(part notes.Part) ([]model.Note, error) {
	p.mu.Lock()
	p.parts = append(p.parts, part)
	p.mu.Unlock()
	return p.Store.Load(part)
}

func (p *partsStore) LoadAll() ([]model.Note, error) {
	p.mu.Lock()
	p.all++
	p.mu.Unlock()
	return p.Store.LoadAll()
}

func (p *partsStore) take() ([]notes.Part, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	ps, all := p.parts, p.all
	p.parts, p.all = nil, 0
	return ps, all
}

func TestNoteReadersLoadOnlyTheirParts(t *testing.T) {
	t.Parallel()
	dir := noteSideRepo(t)
	svc := svcIn(t, dir)
	ps := &partsStore{Store: notes.NewFileStore(t.TempDir())}
	svc.SetNotesStore(ps)
	ctx := context.Background()
	top, err := svc.TopLevel(ctx)
	if err != nil {
		t.Fatal(err)
	}
	wt := notes.WorktreePart(top)
	live := model.FileAddress{State: model.StateUnstaged, Worktree: top, Path: "a.go"}
	commit := model.FileAddress{State: model.StateCommitted, Commit: headSHA(t, dir), Path: "a.go"}
	for _, a := range []model.FileAddress{live, commit} {
		if _, err := svc.NoteAdd(ctx, model.Note{Address: a, Side: model.NoteSideNew, Range: [2]int{1, 1}, Summary: "x"}); err != nil {
			t.Fatalf("NoteAdd %v: %v", a, err)
		}
	}
	ps.take()

	check := func(name string, want []notes.Part) {
		t.Helper()
		got, all := ps.take()
		if all != 0 {
			t.Errorf("%s read every part (LoadAll ×%d)", name, all)
		}
		slices.Sort(got)
		got = slices.Compact(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("%s loaded %v, want %v", name, got, want)
		}
	}
	if _, err := svc.NotesAt(ctx, live); err != nil {
		t.Fatal(err)
	}
	check("NotesAt(live)", []notes.Part{wt})
	if _, err := svc.NotesAt(ctx, commit); err != nil {
		t.Fatal(err)
	}
	check("NotesAt(commit)", []notes.Part{notes.PartCommits, notes.PartPreviews})
	svc.InvalidateNoteCounts()
	if c, err := svc.NoteCounts(ctx); err != nil || c.ByPath["a.go"] != 1 {
		t.Fatalf("NoteCounts = %+v, %v", c, err)
	}
	visible := []notes.Part{notes.PartCommits, notes.PartPreviews, notes.PartShelf, wt}
	check("NoteCounts", visible)
	if _, err := svc.NoteAddresses(ctx); err != nil {
		t.Fatal(err)
	}
	check("NoteAddresses", visible)
	if _, _, err := svc.reviewNotes(ctx); err != nil {
		t.Fatal(err)
	}
	check("reviewNotes", []notes.Part{notes.PartCommits, notes.PartPreviews, wt}) // commit + merge-preview reviews + this worktree's working reviews
}

func TestPreviewPartsByScope(t *testing.T) {
	t.Parallel()
	cases := []struct {
		scope string
		want  []notes.Part
	}{
		{"main...feat", []notes.Part{notes.PartPreviews}},
		{"aaaaaaa..bbbbbbb", []notes.Part{notes.PartCommits}},
		{"", []notes.Part{notes.PartCommits, notes.PartPreviews}},
	}
	for _, c := range cases {
		if got := scopeParts(c.scope); !slices.Equal(got, c.want) {
			t.Errorf("scopeParts(%q) = %v, want %v", c.scope, got, c.want)
		}
	}
}
