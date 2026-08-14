package promptstate

import (
	"path/filepath"
	"testing"
)

func TestWebUIStateUnsavedThenRoundTrips(t *testing.T) {
	fs := NewFileStore(filepath.Join(t.TempDir(), "prompts.toml"))

	if _, saved := fs.WebUIState(); saved {
		t.Fatal("a fresh store must report NO saved layout (the client then applies its first-run defaults)")
	}

	want := WebUI{
		Sections:      []string{"tags", "reflog"},
		SidebarHidden: true,
		SidebarWidth:  310,
		FilesWidth:    420,
		Graph:         "off",
	}
	if err := fs.SetWebUIState(want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, saved := fs.WebUIState()
	if !saved {
		t.Fatal("saved = false right after a save")
	}
	if len(got.Sections) != 2 || got.Sections[0] != "tags" || got.Sections[1] != "reflog" {
		t.Fatalf("sections = %v", got.Sections)
	}
	if !got.SidebarHidden || got.SidebarWidth != 310 || got.FilesWidth != 420 || got.Graph != "off" {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
}

// An empty section list is a DECISION ("I unfolded everything"), not the
// absence of one — the client must not re-apply its defaults over it.
func TestWebUIStateEmptySectionsIsStillSaved(t *testing.T) {
	fs := NewFileStore(filepath.Join(t.TempDir(), "prompts.toml"))
	if err := fs.SetWebUIState(WebUI{Sections: []string{}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, saved := fs.WebUIState()
	if !saved {
		t.Fatal("an empty layout must still count as saved")
	}
	if len(got.Sections) != 0 {
		t.Fatalf("sections = %v, want empty", got.Sections)
	}
}

// The layout shares one file with the prompt/notice records: neither may
// clobber the other.
func TestWebUIStateCoexistsWithPromptRecords(t *testing.T) {
	fs := NewFileStore(filepath.Join(t.TempDir(), "prompts.toml"))
	if err := fs.SuppressPrompt("commit-sort-plain"); err != nil {
		t.Fatal(err)
	}
	if err := fs.DismissNotice("/repo/.git", "commit-graph"); err != nil {
		t.Fatal(err)
	}
	if err := fs.SetWebUIState(WebUI{Sections: []string{"stashes"}, Graph: "svg"}); err != nil {
		t.Fatal(err)
	}
	if err := fs.SuppressPrompt("another-prompt"); err != nil {
		t.Fatal(err)
	}

	if got, saved := fs.WebUIState(); !saved || len(got.Sections) != 1 || got.Sections[0] != "stashes" {
		t.Fatalf("layout after a later prompt write: %+v saved=%v", got, saved)
	}
	if s := fs.SuppressedPrompts(); !s["commit-sort-plain"] || !s["another-prompt"] {
		t.Fatalf("suppressed prompts lost: %v", s)
	}
	if !fs.DismissedNotices("/repo/.git")["commit-graph"] {
		t.Fatal("dismissed notice lost")
	}
}
