package tui

import (
	"os/exec"
	"strings"
	"testing"
)

func tuiTagType(t *testing.T, dir, name string) string {
	t.Helper()
	out, _ := exec.Command("git", "-C", dir, "cat-file", "-t", name).Output()
	return strings.TrimSpace(string(out))
}

// commitCreateTagRow appears only on the Commits panel and opens a tagPopup
// targeting the selected commit.
func TestCommitCreateTagRowGating(t *testing.T) {
	t.Parallel()
	_, repo := newRepoDir(t)
	m := loadModel(t, repo)
	m.focus = panelBranches
	if _, ok := m.commitCreateTagRow(); ok {
		t.Fatal("create-tag row must not appear off the Commits panel")
	}
	m.focus = panelCommits
	m.sel[panelCommits] = 0
	row, ok := m.commitCreateTagRow()
	if !ok {
		t.Fatal("create-tag row must appear on the Commits panel")
	}
	updated, _ := row.run(m)
	if p := layerOf[*tagPopup](updated.(Model)); p == nil || p.commit != m.commits[0].Hash {
		t.Fatalf("row must push a tagPopup targeting the selected commit")
	}
}

// Typing a name then enter creates a lightweight tag at the commit.
func TestTagPopupCreatesLightweightTag(t *testing.T) {
	t.Parallel()
	dir, repo := newRepoDir(t)
	m := loadModel(t, repo)
	hash := m.commits[0].Hash
	m = m.pushLayer(&tagPopup{commit: hash})

	for _, r := range "v1.0.0" {
		u, _ := m.Update(keyMsg(string(r)))
		m = u.(Model)
	}
	updated, cmd := m.Update(keyMsg("enter"))
	m = updated.(Model)
	if layerOf[*tagPopup](m) != nil {
		t.Fatal("enter should close the popup")
	}
	for i := 0; i < 100 && m.running; i++ {
		if cmd == nil {
			t.Fatal("no command but op still running")
		}
		u, next := m.Update(cmd())
		m = u.(Model)
		cmd = next
	}
	if typ := tuiTagType(t, dir, "v1.0.0"); typ != "commit" {
		t.Fatalf("v1.0.0 type = %q, want commit (lightweight)", typ)
	}
}

// Tab to the message field + typing makes the tag annotated.
func TestTagPopupTabMakesAnnotated(t *testing.T) {
	t.Parallel()
	dir, repo := newRepoDir(t)
	m := loadModel(t, repo)
	m = m.pushLayer(&tagPopup{commit: m.commits[0].Hash})

	for _, r := range "v2.0.0" {
		u, _ := m.Update(keyMsg(string(r)))
		m = u.(Model)
	}
	u, _ := m.Update(keyMsg("tab")) // switch to the message field
	m = u.(Model)
	for _, r := range "rel" {
		uu, _ := m.Update(keyMsg(string(r)))
		m = uu.(Model)
	}
	updated, cmd := m.Update(keyMsg("enter"))
	m = updated.(Model)
	for i := 0; i < 100 && m.running; i++ {
		uu, next := m.Update(cmd())
		m = uu.(Model)
		cmd = next
	}
	if typ := tuiTagType(t, dir, "v2.0.0"); typ != "tag" {
		t.Fatalf("v2.0.0 type = %q, want tag (annotated)", typ)
	}
}

func TestTagPopupEnterEmptyNameNoOp(t *testing.T) {
	t.Parallel()
	_, repo := newRepoDir(t)
	m := loadModel(t, repo)
	m = m.pushLayer(&tagPopup{commit: m.commits[0].Hash})
	u, _ := m.Update(keyMsg("enter"))
	m = u.(Model)
	if layerOf[*tagPopup](m) == nil || m.running {
		t.Fatal("enter with an empty name must keep the popup open and start no op")
	}
}
