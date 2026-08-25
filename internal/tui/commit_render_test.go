package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// renderModelWithCommits builds a minimal Model whose Commits panel can render
// rows in list mode (no graph), with the given branches and commits.
func renderModelWithCommits(branches []model.Branch, commits []model.Commit) Model {
	m := Model{branches: branches, commits: commits, commitListMode: true}
	m.focus = panelCommits
	return m
}

func TestCommitRowShowsBothMarkersWhenInSync(t *testing.T) {
	t.Parallel()
	branches := []model.Branch{{Name: "main", IsHead: true, Upstream: "origin/main"}}
	commits := []model.Commit{{
		Hash:    "aaaa111",
		Subject: "in sync commit",
		Refs:    []model.Ref{{Name: "main", Kind: model.RefLocal, Head: true}, {Name: "origin/main", Kind: model.RefRemote}},
	}}
	m := renderModelWithCommits(branches, commits)
	row := m.commitIdentRowAt(0, m.commitIdentWidth(), false, -1)
	if !strings.Contains(row, "↓↑") {
		t.Fatalf("row = %q, want both ↓↑ markers", row)
	}
	if !strings.Contains(row, "*main") {
		t.Fatalf("row = %q, want *main label", row)
	}
}

func TestCommitRowRemoteOnlyTipNamesBranch(t *testing.T) {
	t.Parallel()
	branches := []model.Branch{{Name: "main", Upstream: "origin/main"}}
	commits := []model.Commit{{
		Hash:    "bbbb222",
		Subject: "remote tip ahead",
		Refs:    []model.Ref{{Name: "origin/main", Kind: model.RefRemote}},
		Source:  "main",
	}}
	m := renderModelWithCommits(branches, commits)
	row := m.commitIdentRowAt(0, m.commitIdentWidth(), false, -1)
	if !strings.Contains(row, "↑") || strings.Contains(row, "↓") {
		t.Fatalf("row = %q, want only the remote ↑ marker", row)
	}
	if !strings.Contains(row, "main") {
		t.Fatalf("row = %q, want the local branch name main", row)
	}
}
