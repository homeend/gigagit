package promptstate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tempStore(t *testing.T) *FileStore {
	t.Helper()
	return NewFileStore(filepath.Join(t.TempDir(), "prompts.toml"))
}

func TestEmptyStoreHasNoRecords(t *testing.T) {
	st := tempStore(t)
	if got := st.SuppressedPrompts(); len(got) != 0 {
		t.Fatalf("fresh store: SuppressedPrompts = %v, want empty", got)
	}
	if got := st.DismissedNotices("repo-a"); len(got) != 0 {
		t.Fatalf("fresh store: DismissedNotices = %v, want empty", got)
	}
}

func TestSuppressPromptRoundTrip(t *testing.T) {
	st := tempStore(t)
	if err := st.SuppressPrompt("show_graph_off.commit_sort_plain"); err != nil {
		t.Fatalf("SuppressPrompt: %v", err)
	}
	if !st.SuppressedPrompts()["show_graph_off.commit_sort_plain"] {
		t.Fatal("suppressed id must be reported by the same store")
	}
	// A fresh store over the same file sees the persisted record.
	st2 := NewFileStore(st.path)
	if !st2.SuppressedPrompts()["show_graph_off.commit_sort_plain"] {
		t.Fatal("suppressed id must survive a reload from disk")
	}
	if st2.SuppressedPrompts()["other.prompt"] {
		t.Fatal("an id never suppressed must not be reported")
	}
}

func TestSuppressPromptIsIdempotent(t *testing.T) {
	st := tempStore(t)
	for i := 0; i < 3; i++ {
		if err := st.SuppressPrompt("p1"); err != nil {
			t.Fatalf("SuppressPrompt #%d: %v", i, err)
		}
	}
	raw, err := os.ReadFile(st.path)
	if err != nil {
		t.Fatalf("reading store file: %v", err)
	}
	if strings.Count(string(raw), "p1") != 1 {
		t.Fatalf("id must be stored once, file:\n%s", raw)
	}
}

func TestDismissNoticePerRepo(t *testing.T) {
	st := tempStore(t)
	if err := st.DismissNotice("repo-a", "commit_graph"); err != nil {
		t.Fatalf("DismissNotice: %v", err)
	}
	if !st.DismissedNotices("repo-a")["commit_graph"] {
		t.Fatal("dismissed notice must be reported for its repo")
	}
	if st.DismissedNotices("repo-b")["commit_graph"] {
		t.Fatal("a dismissal is per-repo: repo-b must not see repo-a's record")
	}
	st2 := NewFileStore(st.path)
	if !st2.DismissedNotices("repo-a")["commit_graph"] {
		t.Fatal("dismissal must survive a reload from disk")
	}
}

func TestRecordKindsCoexistInOneFile(t *testing.T) {
	st := tempStore(t)
	if err := st.SuppressPrompt("p1"); err != nil {
		t.Fatal(err)
	}
	if err := st.DismissNotice("repo-a", "n1"); err != nil {
		t.Fatal(err)
	}
	// Writing one kind must not clobber the other (read-merge before write).
	if !st.SuppressedPrompts()["p1"] || !st.DismissedNotices("repo-a")["n1"] {
		t.Fatal("both record kinds must coexist after interleaved writes")
	}
}

func TestApproveToolCommand(t *testing.T) {
	fs := NewFileStore(filepath.Join(t.TempDir(), "prompts.toml"))
	if fs.ApprovedToolCommands("/repo/a")["abc123"] {
		t.Fatal("empty store must not approve")
	}
	if err := fs.ApproveToolCommand("/repo/a", "abc123"); err != nil {
		t.Fatal(err)
	}
	if err := fs.ApproveToolCommand("/repo/a", "abc123"); err != nil { // idempotent
		t.Fatal(err)
	}
	if !fs.ApprovedToolCommands("/repo/a")["abc123"] {
		t.Error("approval not persisted")
	}
	if fs.ApprovedToolCommands("/repo/b")["abc123"] {
		t.Error("approval leaked across repos")
	}
	// Survives reopen (it's on disk).
	fs2 := NewFileStore(fs.path)
	if !fs2.ApprovedToolCommands("/repo/a")["abc123"] {
		t.Error("approval lost on reload")
	}
}

func TestCorruptFileTreatedAsEmpty(t *testing.T) {
	st := tempStore(t)
	if err := os.WriteFile(st.path, []byte("not [ toml"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := st.SuppressedPrompts(); len(got) != 0 {
		t.Fatalf("corrupt file must read as empty, got %v", got)
	}
	// And a write from that state still succeeds (rewrites clean).
	if err := st.SuppressPrompt("p1"); err != nil {
		t.Fatalf("SuppressPrompt over corrupt file: %v", err)
	}
	if !NewFileStore(st.path).SuppressedPrompts()["p1"] {
		t.Fatal("write after corruption must persist")
	}
}
