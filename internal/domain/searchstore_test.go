package domain

import (
	"context"
	"testing"

	"github.com/gigagit/gg/internal/searchhist"
)

func TestRecordSearchAndSearchHistoryAll(t *testing.T) {
	s := New(nil)
	s.SetSearchStore(searchhist.NewFileStore(t.TempDir()))
	ctx := context.Background()

	s.RecordSearch(ctx, "panel", "fix login", 20)
	s.RecordSearch(ctx, "panel", "TODO", 20)
	s.RecordSearch(ctx, "bookmark", "handler", 20)

	all := s.SearchHistoryAll(ctx)
	if got := all["panel"]; len(got) != 2 || got[0] != "TODO" || got[1] != "fix login" {
		t.Fatalf("panel ring = %v, want [TODO, fix login]", got)
	}
	if got := all["bookmark"]; len(got) != 1 || got[0] != "handler" {
		t.Fatalf("bookmark ring = %v, want [handler]", got)
	}
}

func TestRecordSearchClampsSize(t *testing.T) {
	s := New(nil)
	s.SetSearchStore(searchhist.NewFileStore(t.TempDir()))
	ctx := context.Background()
	// rawSize 5000 must clamp to 1000; record one phrase, no panic.
	s.RecordSearch(ctx, "panel", "x", 5000)
	if got := s.SearchHistoryAll(ctx)["panel"]; len(got) != 1 || got[0] != "x" {
		t.Fatalf("ring = %v, want [x]", got)
	}
}

func TestSearchHistoryAllEmptyWhenNothingRecorded(t *testing.T) {
	// Pointing SearchStatePath at a temp dir uses it directly, skipping the
	// git-common-dir resolution (so New(nil)'s nil repo is never touched).
	old := SearchStatePath
	SearchStatePath = t.TempDir()
	defer func() { SearchStatePath = old }()

	s := New(nil)
	got := s.SearchHistoryAll(context.Background())
	if got == nil {
		t.Fatal("SearchHistoryAll must return a non-nil (possibly empty) map")
	}
	if len(got) != 0 {
		t.Fatalf("fresh store should be empty, got %v", got)
	}
}
