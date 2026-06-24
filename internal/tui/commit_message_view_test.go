package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
)

const msgCommitHash = "abc1234def5678"

// commitMsgModel is a Commits-focused model whose svc answers the full-message
// read with subject + body (and a trailing newline, as git does).
func commitMsgModel(t *testing.T) Model {
	t.Helper()
	f := gitexec.NewFakeRunner()
	f.SetResponse("git log -1 --pretty=%B", gitexec.Result{Stdout: "the subject\n\nthe body line\n"})
	m := footerModel()
	m.svc = domain.New(&git.Repo{Runner: f})
	m.focus = panelCommits
	m.commits = []model.Commit{{Hash: msgCommitHash, Subject: "the subject"}}
	return m
}

// pressRuneCmd presses a rune key and keeps the returned cmd (the shared
// pressRune helper drops it).
func pressRuneCmd(m Model, r string) (Model, tea.Cmd) {
	u, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(r)})
	return u.(Model), cmd
}

// Pressing i opens a contentPopup and the async load (run through Update) fills
// it with the full message — body included, trailing blank row trimmed.
func TestCommitMessagePopupFillsThroughUpdate(t *testing.T) {
	m := commitMsgModel(t)
	m, cmd := pressRuneCmd(m, "i")
	if cmd == nil {
		t.Fatalf("i should kick off the async message load")
	}
	cp := layerOf[*contentPopup](m)
	if cp == nil {
		t.Fatalf("i should push a contentPopup")
	}
	if cp.title != commitMessageTitle(shortHash(msgCommitHash)) {
		t.Fatalf("popup title = %q, want the hash-tagged title", cp.title)
	}
	// Run the load through Update (a cmd-non-nil check would miss the tag-gate).
	u, _ := m.Update(cmd())
	m = u.(Model)
	cp = layerOf[*contentPopup](m)
	var sb strings.Builder
	for _, ln := range cp.lines {
		sb.WriteString(ln.text)
		sb.WriteString("\n")
	}
	got := sb.String()
	if !strings.Contains(got, "the body line") {
		t.Fatalf("popup should show the full body, got %q", got)
	}
	if n := len(cp.lines); n == 0 || cp.lines[n-1].text == "" {
		t.Fatalf("trailing blank row should be trimmed, lines = %q", got)
	}
}

// The popup shows a git-show-style metadata header (hash/author/date/refs) from
// the in-memory commit, plus a compact author·date footer line.
func TestCommitMessageHeaderAndFooter(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git log -1 --pretty=%B", gitexec.Result{Stdout: "the subject\n\nthe body line\n"})
	m := footerModel()
	m.svc = domain.New(&git.Repo{Runner: f})
	m.focus = panelCommits
	m.commits = []model.Commit{{
		Hash:     msgCommitHash,
		Author:   "Jane Dev",
		UnixTime: 1750684800, // 2025-06-23 in UTC-ish; we only assert the field is rendered
		Refs:     []model.Ref{{Name: "main", Kind: model.RefLocal, Head: true}, {Name: "v1.0", Kind: model.RefTag}},
		Parents:  []string{"aaaaaaa1111", "bbbbbbb2222"},
	}}
	m, cmd := pressRuneCmd(m, "i")
	cp := layerOf[*contentPopup](m)
	if cp.footer == "" || !strings.Contains(cp.footer, "Jane Dev") {
		t.Fatalf("footer should carry author · date, got %q", cp.footer)
	}
	u, _ := m.Update(cmd())
	m = u.(Model)
	cp = layerOf[*contentPopup](m)
	var sb strings.Builder
	for _, ln := range cp.lines {
		sb.WriteString(ln.text + "\n")
	}
	got := sb.String()
	for _, want := range []string{"commit " + msgCommitHash, "Author: Jane Dev", "Date:   ", "Refs:   main, tag: v1.0", "Merge:  aaaaaaa bbbbbbb", "the body line"} {
		if !strings.Contains(got, want) {
			t.Errorf("popup missing %q\nfull:\n%s", want, got)
		}
	}
}

// A commitMessageMsg whose short hash doesn't match the open popup must be
// dropped — a stale load from another commit can't overwrite the current view.
func TestCommitMessageTagGateRejectsMismatch(t *testing.T) {
	m := commitMsgModel(t)
	m, _ = pressRuneCmd(m, "i")
	before := layerOf[*contentPopup](m).lines
	u, _ := m.Update(commitMessageMsg{short: "deadbee", lines: []contentLine{{text: "WRONG"}}})
	m = u.(Model)
	cp := layerOf[*contentPopup](m)
	if len(cp.lines) != len(before) || cp.lines[0].text != before[0].text {
		t.Fatalf("mismatched-hash load must not fill the popup, lines = %v", cp.lines)
	}
}

// Pressing I resolves the message bytes into a read-only temp for $EDITOR
// (untrimmed — the editor shows it as git stores it).
func TestCommitMessageEditorWritesBytes(t *testing.T) {
	m := commitMsgModel(t)
	m, cmd := pressRuneCmd(m, "I")
	if cmd == nil {
		t.Fatalf("I should open the message in the editor")
	}
	msg, ok := cmd().(editorViewMsg)
	if !ok {
		t.Fatalf("want editorViewMsg, got %T", cmd())
	}
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	defer removeTempFile(msg.path)
	if msg.name != "COMMIT_EDITMSG" {
		t.Errorf("editor name = %q, want COMMIT_EDITMSG", msg.name)
	}
}

// Neither key fires off the Commits panel.
func TestCommitMessageKeysNoopOffCommits(t *testing.T) {
	for _, r := range []string{"i", "I"} {
		m := commitMsgModel(t)
		m.focus = panelBranches
		m, cmd := pressRuneCmd(m, r)
		if cmd != nil {
			t.Errorf("%q off the Commits panel should be a no-op, got a cmd", r)
		}
		if layerOf[*contentPopup](m) != nil {
			t.Errorf("%q off the Commits panel should not push a popup", r)
		}
	}
}

// On a ◇ WIP pseudo-row, i/e no-op — a WIP row is not a real commit. (Guards
// the display-vs-backing class: the suite otherwise runs clean, where wipCount
// is 0 and the offset is a no-op.)
func TestCommitMessageKeysNoopOnWipRow(t *testing.T) {
	for _, r := range []string{"i", "I"} {
		m := commitMsgModel(t)
		m.wipRows = []wipRow{{wipWorktree, 1}}
		m.sel[panelCommits] = 0 // the ◇ Working tree row
		if _, ok := m.commitForMessageView(); ok {
			t.Fatalf("%q: a WIP row must not resolve to a commit", r)
		}
		m, cmd := pressRuneCmd(m, r)
		if cmd != nil {
			t.Errorf("%q on a WIP row should be a no-op, got a cmd", r)
		}
		if layerOf[*contentPopup](m) != nil {
			t.Errorf("%q on a WIP row should not push a popup", r)
		}
	}
}

// The . menu surfaces both rows on the Commits panel, and neither elsewhere.
func TestCommitMessageMenuRows(t *testing.T) {
	m := commitMsgModel(t)
	got := ids(availableActions(m))
	if !got["commit-view-message"] || !got["commit-edit-message"] {
		t.Fatalf("Commits menu should offer both message rows, got %v", got)
	}
	m.focus = panelBranches
	got = ids(availableActions(m))
	if got["commit-view-message"] || got["commit-edit-message"] {
		t.Fatalf("message rows must not appear off the Commits panel, got %v", got)
	}
}
