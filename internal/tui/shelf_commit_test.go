package tui

import "testing"

func TestCommitShelfRowPresentOnCommits(t *testing.T) {
	t.Parallel()
	m := loadedModelLinearCommits(t, 2)
	m.focus = panelCommits
	r, ok := m.commitShelfRow()
	if !ok {
		t.Fatal("Shelf this commit should be offered on the Commits panel")
	}
	if r.id != "commit-shelf" || r.label != "Shelf this commit" || r.run == nil {
		t.Fatalf("bad row: %+v", r)
	}
	m.focus = panelBranches
	if _, ok := m.commitShelfRow(); ok {
		t.Fatal("must not be offered off the Commits panel")
	}
}

func TestCommitShelfRowInMenu(t *testing.T) {
	t.Parallel()
	m := loadedModelLinearCommits(t, 2)
	m.focus = panelCommits
	found := false
	for _, r := range availableActions(m) {
		if r.id == "commit-shelf" {
			found = true
		}
	}
	if !found {
		t.Fatal("commit-shelf must appear in the Commits . menu")
	}
}
