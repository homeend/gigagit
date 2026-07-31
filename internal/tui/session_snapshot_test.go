package tui

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

func TestBuildSessionSnapshotFields(t *testing.T) {
	m := newTestModel(t)
	m.snapshotCommonDir = "/repo/.git"
	m.snapshotWorktree = "/repo"
	m.focus = panelCommits
	m.commits = []model.Commit{{Hash: "aaa111", Subject: "one"}, {Hash: "bbb222", Subject: "two"}}
	m.sel[panelCommits] = 0
	m.commitCompareSet = map[string]bool{"bbb222": true}
	m.fileMarks = map[string]bool{"b.go": true, "a.go": true}
	m.conflict = domain.ConflictState{Op: "merge", Source: "feat", Target: "main"}
	m.statusMsg = "hello"

	s := buildSessionSnapshot(m)
	if s.Version != 1 {
		t.Fatalf("version = %d", s.Version)
	}
	if s.PID != os.Getpid() {
		t.Fatalf("pid = %d", s.PID)
	}
	if s.Repo.CommonDir != "/repo/.git" || s.Repo.Worktree != "/repo" {
		t.Fatalf("repo = %+v", s.Repo)
	}
	if s.Focus.Panel != "commits" {
		t.Fatalf("focus.panel = %q", s.Focus.Panel)
	}
	if s.Cursor.Commit == nil || s.Cursor.Commit.Hash != "aaa111" || s.Cursor.Commit.Subject != "one" {
		t.Fatalf("cursor.commit = %+v", s.Cursor.Commit)
	}
	if !slices.Equal(s.MarkedCommits, []string{"bbb222"}) {
		t.Fatalf("marked_commits = %v", s.MarkedCommits)
	}
	if !slices.Equal(s.MarkedFiles, []string{"a.go", "b.go"}) {
		t.Fatalf("marked_files must be sorted: %v", s.MarkedFiles)
	}
	if s.Conflict == nil || s.Conflict.Op != "merge" || s.Conflict.Target != "main" {
		t.Fatalf("conflict = %+v", s.Conflict)
	}
	if s.Status != "hello" {
		t.Fatalf("status = %q", s.Status)
	}
	if s.FilesView != nil || s.Switcher != nil || s.Filter != nil {
		t.Fatalf("closed surfaces must be nil: %+v %+v %+v", s.FilesView, s.Switcher, s.Filter)
	}
	if s.WrittenAt != "" {
		t.Fatal("builder must not stamp written_at (stamped at write time)")
	}
}

func TestBuildSessionSnapshotFilterAndScope(t *testing.T) {
	m := newTestModel(t)
	m.filterPanel = panelCommits
	m.filterQuery = "fix"
	m.highlightQuery = "wip"
	m.commitScopeBranches = []string{"main", "feat"}
	s := buildSessionSnapshot(m)
	if s.Filter == nil || s.Filter.Panel != "commits" || s.Filter.Query != "fix" || s.Filter.Highlight != "wip" {
		t.Fatalf("filter = %+v", s.Filter)
	}
	if !slices.Equal(s.CommitScope, []string{"main", "feat"}) {
		t.Fatalf("commit_scope = %v", s.CommitScope)
	}
}

// TestEndpointProtoShelf covers a frozen shelved-commit compare endpoint
// (model.EndpointShelf): it must serialize as kind "shelf" carrying the
// shelf entry id, never fall into the default "commit" branch with an
// empty hash (protocol misinformation for gg mcp's gg_ui_state).
func TestEndpointProtoShelf(t *testing.T) {
	e := model.Endpoint{Kind: model.EndpointShelf, ShelfID: "commit-abc1234-deadbeef"}
	got := endpointProto(e)
	if got == nil || got.Kind != "shelf" || got.ShelfID != "commit-abc1234-deadbeef" {
		t.Fatalf("endpointProto(shelf) = %+v, want kind=shelf shelf_id=commit-abc1234-deadbeef", got)
	}
	if got.Kind == "commit" {
		t.Fatal("shelf endpoint must not serialize as kind commit")
	}
}

// TestBuildSessionSnapshotCompareShelfEndpoint covers the full path: a
// compare-mode files view whose right side is a frozen shelved commit must
// come through buildSessionSnapshot as FilesView.Right.Kind == "shelf" with
// the shelf id, not the misclassified default "commit"/empty-hash shape.
func TestBuildSessionSnapshotCompareShelfEndpoint(t *testing.T) {
	m := newTestModel(t)
	m.filesView = &contentPopup{}
	m.filesMode = filesModeCompare
	m.filesLeft = model.Endpoint{Kind: model.EndpointCommit, Hash: "aaa111"}
	m.filesRight = model.Endpoint{Kind: model.EndpointShelf, ShelfID: "commit-abc1234-deadbeef"}

	s := buildSessionSnapshot(m)
	if s.FilesView == nil || s.FilesView.Right == nil {
		t.Fatalf("files_view.right must be set: %+v", s.FilesView)
	}
	right := s.FilesView.Right
	if right.Kind != "shelf" || right.ShelfID != "commit-abc1234-deadbeef" {
		t.Fatalf("files_view.right = %+v, want kind=shelf shelf_id=commit-abc1234-deadbeef", right)
	}
	if right.Kind == "commit" {
		t.Fatal("frozen shelf compare side must not serialize as kind commit")
	}
}

func TestSnapshotJSONKeys(t *testing.T) {
	m := newTestModel(t)
	s := buildSessionSnapshot(m)
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"version", "pid", "repo", "focus", "cursor"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("marshalled snapshot missing %q: %s", key, data)
		}
	}
}

func TestMaybeWriteSnapshotOnlyOnChange(t *testing.T) {
	m := newTestModel(t)
	m.snapshotPath = filepath.Join(t.TempDir(), "ui-state.json")

	m = m.maybeWriteSnapshot()
	data1, err := os.ReadFile(m.snapshotPath)
	if err != nil {
		t.Fatalf("first heartbeat must write: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data1, &raw); err != nil {
		t.Fatalf("snapshot is not valid JSON: %v", err)
	}
	if raw["written_at"] == "" {
		t.Fatal("written file must carry written_at")
	}

	m = m.maybeWriteSnapshot() // nothing changed
	data2, _ := os.ReadFile(m.snapshotPath)
	if !bytes.Equal(data1, data2) {
		t.Fatal("unchanged state must not rewrite the snapshot")
	}

	m.statusMsg = "changed"
	m = m.maybeWriteSnapshot()
	data3, _ := os.ReadFile(m.snapshotPath)
	if bytes.Equal(data2, data3) {
		t.Fatal("changed state must rewrite the snapshot")
	}

	removeSnapshotFile(m.snapshotPath)
	if _, err := os.Stat(m.snapshotPath); !os.IsNotExist(err) {
		t.Fatal("removeSnapshotFile must delete the file")
	}
}

func TestMaybeWriteSnapshotDisabled(t *testing.T) {
	m := newTestModel(t)
	m.snapshotPath = "" // no state root / not a repo
	m = m.maybeWriteSnapshot()
	// Nothing to assert beyond "no panic": disabled means no write target.
}

// TestSnapshotTargetMsgStaleServiceDropped covers reRoot's async snapshot-target
// resolution (snapshotTargetCmd/snapshotTargetMsg): a resolve whose svc no
// longer matches the model's current *domain.Service (a later repo switch
// superseded it, pointer identity is the staleness guard) must be dropped
// without touching the snapshot fields; a resolve for the CURRENT svc must
// apply.
func TestSnapshotTargetMsgStaleServiceDropped(t *testing.T) {
	m := newTestModel(t)
	m.snapshotCommonDir = "/old/.git"
	m.snapshotWorktree = "/old"
	m.snapshotPath = "/old/path"
	m.lastSnapshot = []byte("stale-payload")

	other := newTestModel(t) // a distinct *domain.Service pointer
	if other.svc == m.svc {
		t.Fatal("test setup: expected two distinct *domain.Service instances")
	}

	updated, _ := m.Update(snapshotTargetMsg{svc: other.svc, commonDir: "/new/.git", worktree: "/new"})
	mm, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want Model", updated)
	}
	if mm.snapshotCommonDir != "/old/.git" || mm.snapshotWorktree != "/old" || mm.snapshotPath != "/old/path" {
		t.Fatalf("stale snapshotTargetMsg must not change snapshot fields: %+v", mm)
	}
	if string(mm.lastSnapshot) != "stale-payload" {
		t.Fatalf("stale snapshotTargetMsg must not clear lastSnapshot: %q", mm.lastSnapshot)
	}

	updated, _ = m.Update(snapshotTargetMsg{svc: m.svc, commonDir: "/new/.git", worktree: "/new"})
	mm, ok = updated.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want Model", updated)
	}
	if mm.snapshotCommonDir != "/new/.git" || mm.snapshotWorktree != "/new" {
		t.Fatalf("current-svc snapshotTargetMsg must apply: %+v", mm)
	}
	if want := config.SessionSnapshotPath("/new/.git"); mm.snapshotPath != want {
		t.Fatalf("snapshotPath = %q, want %q", mm.snapshotPath, want)
	}
	if mm.lastSnapshot != nil {
		t.Fatal("current-svc snapshotTargetMsg must clear lastSnapshot")
	}
}
