package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// ignoreModel builds an idle, sized model focused on the Files panel with the
// given files, cursor on row sel.
func ignoreModel(files []model.FileStatus, sel int) Model {
	m := New(nil)
	m.width, m.height = 80, 30
	m.loading = false
	m.focus = panelFiles
	m.status = model.WorkingTreeStatus{Files: files}
	m.sel[panelFiles] = sel
	return m
}

func untracked(path string) model.FileStatus {
	return model.FileStatus{Path: path, Kind: model.KindUntracked}
}

func TestFileIgnoreRowOnUntracked(t *testing.T) {
	m := ignoreModel([]model.FileStatus{untracked("build/out.log")}, 0)
	r, ok := m.fileIgnoreRow()
	if !ok {
		t.Fatal("want ignore row on an untracked file")
	}
	if r.id != "ignore-file" || r.label != "Add to .gitignore" {
		t.Fatalf("row = %+v", r)
	}
}

func TestFileIgnoreExtRowRequiresExtension(t *testing.T) {
	m := ignoreModel([]model.FileStatus{untracked("foo.log")}, 0)
	r, ok := m.fileIgnoreExtRow()
	if !ok {
		t.Fatal("want ext row on untracked foo.log")
	}
	if r.id != "ignore-ext" || !strings.Contains(r.label, "*.log") {
		t.Fatalf("row = %+v", r)
	}

	// No extension → no ext row.
	m2 := ignoreModel([]model.FileStatus{untracked("Makefile")}, 0)
	if _, ok := m2.fileIgnoreExtRow(); ok {
		t.Fatal("ext row must be hidden when the file has no extension")
	}
	// ...but the plain ignore row is still offered.
	if _, ok := m2.fileIgnoreRow(); !ok {
		t.Fatal("plain ignore row should still apply to an extensionless untracked file")
	}
}

func TestAvailableActionsIncludesIgnoreRows(t *testing.T) {
	m := ignoreModel([]model.FileStatus{untracked("foo.log")}, 0)
	var ids []string
	for _, r := range availableActions(m) {
		ids = append(ids, r.id)
	}
	wantIDs := []string{"ignore-file", "ignore-ext"}
	for _, want := range wantIDs {
		found := false
		for _, id := range ids {
			if id == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("availableActions missing %q; got %v", want, ids)
		}
	}
}

func TestFileIgnoreRowsGating(t *testing.T) {
	// Tracked file → neither row.
	tracked := model.FileStatus{Path: "edit.go", Kind: model.KindTracked, Unstaged: 'M'}
	m := ignoreModel([]model.FileStatus{tracked}, 0)
	if _, ok := m.fileIgnoreRow(); ok {
		t.Fatal("no ignore row on a tracked file")
	}
	if _, ok := m.fileIgnoreExtRow(); ok {
		t.Fatal("no ext row on a tracked file")
	}

	// Staged panel → neither row.
	m2 := ignoreModel([]model.FileStatus{untracked("foo.log")}, 0)
	m2.focus = panelStaged
	if _, ok := m2.fileIgnoreRow(); ok {
		t.Fatal("no ignore row on the Staged panel")
	}

	// Op running → neither row.
	m3 := ignoreModel([]model.FileStatus{untracked("foo.log")}, 0)
	m3.running = true
	if _, ok := m3.fileIgnoreRow(); ok {
		t.Fatal("no ignore row while an op is running")
	}
}
