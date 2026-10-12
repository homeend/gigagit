package domain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// Final review #1: `gg note clear --all` enumerates NoteAddresses and clears
// each — an overview is neither enumerated nor swept (R9).
func TestNotesClearKeepsOverviews(t *testing.T) {
	t.Parallel()
	dir, svc := permOvRepo(t)
	ctx := context.Background()
	got, err := saveOv(t, svc, ovDoc("T", "[a](a.go:1)", ""))
	if err != nil {
		t.Fatal(err)
	}
	addrs, err := svc.NoteAddresses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(addrs) != 0 {
		t.Fatalf("NoteAddresses enumerates the overview: %+v", addrs)
	}
	head := revParse(t, dir, "HEAD")
	n, err := svc.NotesClear(ctx, model.FileAddress{State: model.StateCommitted, Commit: head})
	if err != nil || n != 0 {
		t.Fatalf("NotesClear(commit) = %d %v, want 0 removed", n, err)
	}
	if _, err := svc.Overview(ctx, got.ID); err != nil {
		t.Fatalf("the overview is gone after a clear: %v", err)
	}
}

// Final review #4: a reply or an edit would corrupt a stored overview.
func TestNoteEditAndReplyRefuseOverviews(t *testing.T) {
	t.Parallel()
	_, svc := permOvRepo(t)
	ctx := context.Background()
	got, err := saveOv(t, svc, ovDoc("T", "[a](a.go:1)", ""))
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.NoteEdit(ctx, got.ID, "x", "y"); !errors.Is(err, ErrOverviewNote) {
		t.Fatalf("NoteEdit = %v, want ErrOverviewNote", err)
	}
	if _, err := svc.NoteReply(ctx, got.ID, model.Note{Source: model.NoteSourceUser, Summary: "r"}); !errors.Is(err, ErrOverviewNote) {
		t.Fatalf("NoteReply = %v, want ErrOverviewNote", err)
	}
	ov, err := svc.Overview(ctx, got.ID)
	if err != nil || ov.Doc == nil || ov.Title != "T" {
		t.Fatalf("overview after the refusals = %+v %v", ov, err)
	}
}

// Final review #2: a disk check that cannot run never lets a save through.
func TestSaveOverviewRefusesWhenTheDiskCheckFails(t *testing.T) {
	t.Parallel()
	dir, svc := permOvRepo(t)
	if err := os.WriteFile(filepath.Join(dir, ".git", "index"), []byte("not an index"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := saveOv(t, svc, ovDoc("T", "[a](a.go:1)", ""))
	p := problems(t, err)
	if len(p) != 1 || !strings.HasPrefix(p[0], "could not check the files on disk: ") {
		t.Fatalf("problems = %q", p)
	}
	if all, _ := svc.Overviews(context.Background()); len(all) != 0 {
		t.Fatal("stored although the disk check failed")
	}
}
