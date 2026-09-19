package tui

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// stashLinkModel is a real repo holding two stashes — "first" (so it sits at
// stash@{1}) then "second" — behind a Model whose clipboard is a slice and
// whose link history is a temp dir.
func stashLinkModel(t *testing.T) (Model, string, *[]string) {
	t.Helper()
	dir, repo := newRepoDir(t)
	for _, name := range []string{"first", "second"} {
		if err := os.WriteFile(filepath.Join(dir, name+".txt"), []byte(name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitT(t, dir, "stash", "push", "-u", "-m", name)
	}
	m := New(domain.New(repo))
	m.linkRepoName = "r"
	m.svc.UseLinkHistDir(t.TempDir())
	var wrote []string
	m.clipWrite = func(_ io.Writer, s string) (string, error) {
		wrote = append(wrote, s)
		return "fake", nil
	}
	return m, dir, &wrote
}

// runCmds executes a command and, recursively, every member of a batch.
func runCmds(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, runCmds(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func TestStashLinkIsTheResolvedPairAndSurvivesARenumbering(t *testing.T) {
	t.Parallel()
	m, dir, _ := stashLinkModel(t)
	firstSha := gitT(t, dir, "rev-parse", "stash@{1}")
	firstParent := gitT(t, dir, "rev-parse", "stash@{1}^1")
	if firstSha == gitT(t, dir, "rev-parse", "stash@{0}") {
		t.Fatal("fixture broken: the two stashes are one commit")
	}

	msg := m.stashLinkCmd("stash@{1}")().(stashLinkMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	text, ok := m.pairLinkFor(msg.parent, msg.sha)
	if !ok {
		t.Fatal("refused")
	}
	if strings.Contains(text, "stash") || strings.ContainsAny(text, "{}") {
		t.Fatalf("a positional ref leaked into the link: %s", text)
	}

	// RENUMBER: a third stash pushes "first" to stash@{2}. The text copied
	// BEFORE that must still name it.
	if err := os.WriteFile(filepath.Join(dir, "third.txt"), []byte("3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitT(t, dir, "stash", "push", "-u", "-m", "third")
	if gitT(t, dir, "rev-parse", "stash@{1}") == firstSha {
		t.Fatal("fixture broken: the renumbering did not move stash@{1}")
	}
	l, err := model.ParseLink(text)
	if err != nil {
		t.Fatal(err)
	}
	if l.Target.Pair == nil || l.Target.Pair.A != firstParent || l.Target.Pair.B != firstSha {
		t.Fatalf("pair = %+v, want %s..%s", l.Target.Pair, firstParent, firstSha)
	}
}

func TestPairLinkRefusesAShortSha(t *testing.T) {
	t.Parallel()
	m := refRowsModel(panelBranches)
	full := strings.Repeat("a", 40)
	if s, ok := m.pairLinkFor("abc1234", full); ok {
		t.Errorf("short a accepted: %s", s)
	}
	if s, ok := m.pairLinkFor(full, "abc1234"); ok {
		t.Errorf("short b accepted: %s", s)
	}
	if _, ok := m.pairLinkFor(full, full); !ok {
		t.Error("two full shas refused")
	}
}

// The whole gesture: menu row → resolve → copy → the copy records, and the
// history row is recognisable as a stash although stash@{N} is gone from it.
func TestTheStashMenuRowCopiesAndRecordsAsAStash(t *testing.T) {
	t.Parallel()
	m, dir, wrote := stashLinkModel(t)
	m.stashView = &stashView{entries: []model.StashEntry{{Ref: "stash@{0}", Subject: "x"}, {Ref: "stash@{1}", Subject: "y"}}, sel: 1}
	m.focus = panelCommits
	row, ok := findRow(availableActions(m), "copy-link")
	if !ok {
		t.Fatal("the stash list menu has no copy-link row")
	}
	_, cmd := row.run(m)
	msgs := runCmds(cmd)
	if len(msgs) != 1 {
		t.Fatalf("msgs = %#v", msgs)
	}
	nm, cmd := m.Update(msgs[0])
	runCmds(cmd)
	_ = nm

	want := "gg://r@" + gitT(t, dir, "rev-parse", "stash@{1}^1") + ".." + gitT(t, dir, "rev-parse", "stash@{1}")
	h := m.svc.LinkHistory(context.Background())
	if len(*wrote) != 1 || (*wrote)[0] != want {
		t.Fatalf("wrote %v, want the SELECTED row's pair %s", *wrote, want)
	}
	if len(h) != 1 || h[0].Link != want || !strings.HasPrefix(h[0].Desc, "stash: ") || !strings.HasSuffix(h[0].Desc, ": first") {
		t.Fatalf("history = %+v", h)
	}
}

func TestADroppedStashCopiesNothing(t *testing.T) {
	t.Parallel()
	m, _, wrote := stashLinkModel(t)
	nm, cmd := m.Update(stashLinkMsg{ref: "stash@{9}", err: errors.New("gone")})
	runCmds(cmd)
	if len(*wrote) != 0 || !strings.Contains(nm.(Model).statusMsg, "stash@{9}") {
		t.Fatalf("wrote=%v status=%q", *wrote, nm.(Model).statusMsg)
	}
	if h := m.svc.LinkHistory(context.Background()); len(h) != 0 {
		t.Fatalf("history = %+v", h)
	}
}
