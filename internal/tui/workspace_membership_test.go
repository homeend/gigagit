package tui

import (
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/gittest"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/workspace"
)

// Probe round 2: who is a member — a hand-edited file past the cap or
// listing a linked worktree, an edit of a workspace another gg removed,
// a bare repository typed into Path….

// a hand-edited workspace whose 6th entry is the launch repository.
// The launch dialog (and --workspace) check membership on the RAW entry;
// openWorkspace then normalises (cap 5), the launch repo is no member any
// more, and gg re-roots AWAY into the first member.
func TestLaunchMemberBeyondCapReRootsAway(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	home := m.homeRepo().mainPath
	var rs []string
	for range 5 {
		rs = append(rs, secondRepo(t))
	}
	rs = append(rs, home)
	m = withWorkspacesFile(t, m, []workspace.Entry{{Name: "six", Repos: rs}})

	t.Run("dialog", func(t *testing.T) {
		nm, _ := m.maybeAskWorkspace()
		p, ok := nm.topLayer().(*workspaceLaunchPopup)
		if !ok {
			t.Skipf("dialog not offered (fine): top=%T", nm.topLayer())
		}
		_ = p
		nm, _ = wsPress(t, nm, "enter") // "Open workspace six" — the cmd is NOT run (no re-root load, no watchers)
		if nm.ws == nil && nm.switchTarget != "" {
			t.Fatalf("picking six from its member %s re-rooted away to %s (the launch repo was cut by Normalized)", filepath.Base(home), nm.switchTarget)
		}
	})
	t.Run("flag", func(t *testing.T) {
		e, err := resolveStartWorkspace(m.svc, m.workspacesPath(), "six")
		if err != nil {
			t.Skipf("refused up front (fine): %v", err)
		}
		mm := m
		mm.startWorkspace = &e
		nm, _ := mm.startWorkspaceIfAsked()
		if nm.ws == nil && nm.switchTarget != "" {
			t.Fatalf("--workspace six accepted for %s, then re-rooted away to %s", filepath.Base(home), nm.switchTarget)
		}
	})
}

// an edit in a stale popup resurrects a workspace another gg
// deleted (Edit creates the entry when it is gone; saved()'s "deleted
// elsewhere" branch never fires for an entry-level edit).
func TestPopupEditResurrectsDeletedWorkspace(t *testing.T) {
	for _, key := range []string{"J", "s", "d"} {
		t.Run(key, func(t *testing.T) {
			m := loadedModel(t)
			home := m.homeRepo().mainPath
			other := secondRepo(t)
			m, _ = openWSPopup(t, m, []workspace.Entry{{Name: "shop", Repos: []string{home, other}}, {Name: "keep", Repos: []string{other}}})
			m, _ = wsPress(t, m, "enter") // shop's members
			if _, err := workspace.Update(m.workspacesPath(), func(ws []workspace.Entry) []workspace.Entry {
				return workspace.Delete(ws, "shop") // another gg process deletes it
			}); err != nil {
				t.Fatal(err)
			}
			m, _ = wsPress(t, m, key)
			if e, ok := workspace.Find(workspace.Load(m.workspacesPath()), "shop"); ok {
				t.Fatalf("%q in a stale members list resurrected the deleted workspace: %+v", key, e)
			}
		})
	}
}

// rename in a stale list after another gg renamed the same
// workspace: Rename appends a NEW EMPTY workspace under the typed name.
func TestPopupRenameAfterExternalRenameCreatesEmpty(t *testing.T) {
	m := loadedModel(t)
	home := m.homeRepo().mainPath
	m, _ = openWSPopup(t, m, []workspace.Entry{{Name: "shop", Repos: []string{home, secondRepo(t)}}})
	if _, err := workspace.Update(m.workspacesPath(), func(ws []workspace.Entry) []workspace.Entry {
		return workspace.Rename(ws, "shop", "store")
	}); err != nil {
		t.Fatal(err)
	}
	m, _ = wsPress(t, m, "r")
	for range 4 {
		m, _ = wsPress(t, m, "backspace")
	}
	m = wsType(t, m, "mall")
	m, _ = wsPress(t, m, "enter")
	if e, ok := workspace.Find(workspace.Load(m.workspacesPath()), "mall"); ok && len(e.Repos) == 0 {
		t.Fatalf("renaming a workspace another gg renamed created an empty one: %+v", workspace.Load(m.workspacesPath()))
	}
}

// Path… refuses a bare repository ("not a git repository"), although
// a bare main is a valid member identity (spec Terms; RepoMainPath handles it):
// repoPathPopup validates with TopLevel, which fails outside a work tree.
func TestPathPickerRefusesBareRepo(t *testing.T) {
	m := loadedModel(t)
	home := m.homeRepo().mainPath
	bare, _ := bareRepoWithWorktree(t, true)
	m, p := openWSPopup(t, m, []workspace.Entry{{Name: "shop", Repos: []string{home}}})
	m, _ = wsPress(t, m, "enter")
	m, _ = wsPress(t, m, "a")
	rows := p.pickerRows(m)
	p.psel = len(rows) - 1 // Path…
	m, _ = wsPress(t, m, "enter")
	pp, ok := m.topLayer().(*repoPathPopup)
	if !ok {
		t.Fatalf("top = %T", m.topLayer())
	}
	pp.input = newTextField(bare)
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(Model)
	if cmd == nil {
		t.Fatal("no resolve cmd")
	}
	nm, _ = m.Update(cmd())
	m = nm.(Model)
	if pp.err != "" {
		t.Fatalf("a bare repository typed into Path… was refused: %q", pp.err)
	}
}

// a hand-edited workspaces.toml listing a member's LINKED worktree
// beside its main checkout: one repository, one group, no ring duplicates.
func TestLinkedWorktreeListedBesideMainIsOneMember(t *testing.T) {
	dir := gittest.BasicRepo(t, "hi\n")
	linked := filepath.Join(t.TempDir(), "a-linked")
	gittest.Run(t, dir, "worktree", "add", "-b", "lk", linked)
	m := loadedModelAt(t, dir)
	m.width, m.height = 160, 50
	home := m.homeRepo()
	other := secondRepo(t)
	nm, cmd := m.openWorkspace(workspace.Entry{Name: "w", Repos: []string{home.mainPath, linked, other}})
	m = settleWorkspace(t, nm, cmd)
	seen := map[*repoView]int{}
	for _, r := range m.ws.members {
		seen[r]++
	}
	for r, n := range seen {
		if n > 1 {
			t.Errorf("member %s appears %d times in ws.members", r.mainPath, n)
		}
	}
	stops := map[model.CheckoutKey]int{}
	for _, s := range m.worktreeRing() {
		stops[model.KeyOf(s.path)]++
	}
	for k, n := range stops {
		if n > 1 {
			t.Errorf("ring stop %s appears %d times", k, n)
		}
	}
}

// same, with home in ANOTHER member: the main and the linked path
// become two members over one repository.
func TestLinkedWorktreeListedBesideMainElsewhere(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 50
	home := m.homeRepo()
	dir := gittest.BasicRepo(t, "hi\n")
	linked := filepath.Join(t.TempDir(), "a-linked")
	gittest.Run(t, dir, "worktree", "add", "-b", "lk", linked)
	nm, cmd := m.openWorkspace(workspace.Entry{Name: "w", Repos: []string{home.mainPath, dir, linked}})
	m = settleWorkspace(t, nm, cmd)
	stops := map[model.CheckoutKey]int{}
	for _, s := range m.worktreeRing() {
		stops[model.KeyOf(s.path)]++
	}
	for k, n := range stops {
		if n > 1 {
			t.Errorf("ring stop %s appears %d times (members=%d)", k, n, len(m.ws.members))
		}
	}
}
