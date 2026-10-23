package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/forge/forgetest"
	"github.com/homeend/gigagit/internal/workspace"
)

// a member's service shares HOME's repository state (forge verdict,
// PR caches, base repo, PR disk store, tag cache): home A has no forge,
// member B is on GitHub — B's pull requests must still show. SERIAL
// (Setenv + the ForgeDisabled seam), as pr_read_serial_test.go.
func TestMemberForgeIsItsOwn(t *testing.T) {
	bin := forgetest.BuildFakeGH(t)
	t.Setenv(forgetest.EnvBin, bin)
	t.Setenv(forgetest.EnvFixtures, "")
	domain.ForgeDisabled = false
	t.Cleanup(func() { domain.ForgeDisabled = true })

	aDir, _ := newRepoDir(t) // no .git/fakegh: gh fails here, as on a repo with no GitHub remote
	bDir, _ := newRepoDir(t)
	files := map[string]string{}
	for _, name := range []string{"pr-list.json", "repo-view.json"} {
		b, err := os.ReadFile(filepath.Join("..", "forge", "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		files[name] = string(b)
	}
	forgetest.Seed(t, filepath.Join(bDir, ".git", "fakegh"), files)

	if st := domain.OpenTUI(bDir).ForgeStatus(context.Background()); !st.Available() {
		t.Fatalf("control: B alone has no forge: %+v", st)
	}
	m := New(domain.OpenTUI(aDir))
	u, cmd := m.Update(m.loadCmd()())
	m = settleLoad(t, u.(Model), cmd)
	m.width, m.height = 160, 50
	if st := m.svc.ForgeStatus(context.Background()); st.Available() {
		t.Fatalf("home A has a forge: %+v", st)
	}
	home := m.homeRepo()
	nm, c := m.openWorkspace(workspace.Entry{Name: "w", Repos: []string{home.mainPath, bDir}})
	m = settleWorkspace(t, nm, c)
	b := m.ws.members[1]
	if st := b.svc.ForgeStatus(context.Background()); !st.Available() {
		t.Errorf("member B's forge verdict is home A's: %+v", st)
	}
}

// the shared repository state includes the PR cache keyed by NUMBER:
// home A read its PR #7; member B's PR #7 (another pull request in another
// repository) is then served from A's cache. SERIAL, as above.
func TestMemberPRCacheIsItsOwn(t *testing.T) {
	bin := forgetest.BuildFakeGH(t)
	t.Setenv(forgetest.EnvBin, bin)
	t.Setenv(forgetest.EnvFixtures, "")
	domain.ForgeDisabled = false
	t.Cleanup(func() { domain.ForgeDisabled = true })

	seed := func(dir, title string) {
		files := map[string]string{}
		for _, name := range []string{"pr-list.json", "repo-view.json", "pr-view-7.json", "snapshot-7.json", "threads-7.json"} {
			b, err := os.ReadFile(filepath.Join("..", "forge", "testdata", name))
			if err != nil {
				t.Fatal(err)
			}
			files[name] = strings.NewReplacer("Add forge tab", title, "\"title\":\"Add x\"", "\"title\":\""+title+"\"").Replace(string(b))
		}
		forgetest.Seed(t, filepath.Join(dir, ".git", "fakegh"), files)
	}
	aDir, _ := newRepoDir(t)
	bDir, _ := newRepoDir(t)
	seed(aDir, "A seven")
	seed(bDir, "B seven")

	m := New(domain.OpenTUI(aDir))
	u, cmd := m.Update(m.loadCmd()())
	m = settleLoad(t, u.(Model), cmd)
	m.width, m.height = 160, 50
	ctx := context.Background()
	if rv, err := m.svc.PRRevalidate(ctx, 7); err != nil || rv.PR.Title != "A seven" {
		t.Fatalf("A's #7: %+v %v", rv.PR.Title, err)
	}
	home := m.homeRepo()
	nm, c := m.openWorkspace(workspace.Entry{Name: "w", Repos: []string{home.mainPath, bDir}})
	m = settleWorkspace(t, nm, c)
	b := m.ws.members[1]
	if pr, _, ok := b.svc.PRDetailsCached(7); ok && pr.Title != "B seven" {
		t.Errorf("member B's PR #7 is served from home A's cache: title %q", pr.Title)
	}
	// control: B alone has nothing cached for #7
	if _, _, ok := domain.OpenTUI(bDir).PRDetailsCached(7); ok {
		t.Errorf("control: B alone has a cached #7")
	}
}
