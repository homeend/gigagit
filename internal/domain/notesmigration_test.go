package domain

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notes"
)

// A legacy notes.toml is split WITHOUT asking, and every note is still found
// by the reader that showed it before.
func TestRunAutoMigrationsSplitsTheNoteStore(t *testing.T) {
	t.Parallel()
	dir := noteSideRepo(t)
	svc := svcIn(t, dir)
	root := t.TempDir()
	svc.UseNotesDir(root)
	ctx := context.Background()
	top, err := svc.TopLevel(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sha := headSHA(t, dir)
	live := model.Note{ID: "w0000000", Source: model.NoteSourceUser, Side: model.NoteSideNew, Range: [2]int{1, 1},
		Address: model.FileAddress{State: model.StateUnstaged, Worktree: top, Path: "a.go"}, Summary: "live"}
	review := model.Note{ID: "r0000000", Source: model.NoteSourceAgent, Side: model.NoteSideNew,
		Address: model.FileAddress{State: model.StateCommitted, Commit: sha}, Tags: []string{model.ReviewTag},
		Summary: "Review: main", Rationale: "looks fine"}
	data, err := toml.Marshal(struct {
		Notes []model.Note `toml:"notes"`
	}{[]model.Note{live, review}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, notes.LegacyFile), data, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := svc.RunAutoMigrations(ctx); err != nil {
		t.Fatalf("RunAutoMigrations: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, notes.LegacyFile)); !os.IsNotExist(err) {
		t.Fatalf("notes.toml survived the split (stat err = %v)", err)
	}
	if got, err := svc.NoteGet(ctx, live.ID); err != nil || got.Summary != "live" {
		t.Fatalf("NoteGet(live) = %+v, %v", got, err)
	}
	if rs, err := svc.reviewNotes(ctx); err != nil || len(rs) != 1 || rs[0].ID != review.ID {
		t.Fatalf("reviewNotes = %+v, %v", rs, err)
	}
	svc.InvalidateNoteCounts()
	if c, err := svc.NoteCounts(ctx); err != nil || len(c.Reviews) != 1 {
		t.Fatalf("NoteCounts = %+v, %v", c, err)
	}
	// Nothing left to do: a second run is a no-op, not an error.
	if err := svc.RunAutoMigrations(ctx); err != nil {
		t.Fatalf("second RunAutoMigrations: %v", err)
	}
}

func TestNotesFeatureDeclaresALosslessSilentMigration(t *testing.T) {
	t.Parallel()
	for _, f := range Features() {
		if f.ID != FeatureNotes {
			continue
		}
		m := f.Migrate
		if m == nil || m.Store != StoreNotes || m.From != 1 || m.To != NotesFormat || !m.Lossless || m.Action != "split-notes" {
			t.Fatalf("notes migration = %+v", m)
		}
		if !f.Silent {
			t.Fatal("a recreated notes.toml mid-session must not raise a notice")
		}
		if m.Describe == nil || m.Describe().Format == "" {
			t.Fatal("Describe is required")
		}
		return
	}
	t.Fatal("no notes feature registered")
}
