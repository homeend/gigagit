package tui

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/forge/forgetest"
)

// prReadRepo is a real repo whose .git/fakegh answers the forge CLI. The tests
// using it are SERIAL on purpose: they set process env (GG_GH_BIN) and lift
// the package-wide domain.ForgeDisabled seam, neither of which a parallel test
// may do. Go runs a package's serial tests before it resumes the parallel
// ones, so the window is theirs alone.
func prReadRepo(t *testing.T, bin string, fixtures ...string) Model {
	t.Helper()
	dir, _ := newRepoDir(t)
	t.Setenv(forgetest.EnvBin, bin)
	t.Setenv(forgetest.EnvFixtures, "")
	domain.ForgeDisabled = false
	t.Cleanup(func() { domain.ForgeDisabled = true })
	files := map[string]string{}
	for _, name := range fixtures {
		b, err := os.ReadFile(filepath.Join("..", "forge", "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		files[name] = string(b)
	}
	forgetest.Seed(t, filepath.Join(dir, ".git", "fakegh"), files)
	return New(domain.OpenTUI(dir))
}

func TestReadPRsAgainstTheFakeForge(t *testing.T) {
	m := prReadRepo(t, forgetest.BuildFakeGH(t), "pr-list.json")
	m, cmd := m.kickForgeProbe()
	if cmd == nil || !m.prsInflight {
		t.Fatal("the startup probe must dispatch a read")
	}
	if _, again := m.kickForgeProbe(); again != nil {
		t.Fatal("the probe runs once per repo session")
	}
	nm, _ := m.Update(cmd())
	m = nm.(Model)
	if !m.forgeShown || m.forgeProvider != "github" || len(m.prs) == 0 || m.prsErr != "" {
		t.Fatalf("shown=%v provider=%q prs=%d err=%q", m.forgeShown, m.forgeProvider, len(m.prs), m.prsErr)
	}
	if rows := m.prRows(); len(rows) != len(m.prs) || rows[0] == "" {
		t.Fatalf("rows = %q", rows)
	}
}

func TestReadPRsWithoutAForgeIsSilent(t *testing.T) {
	m := prReadRepo(t, filepath.Join(t.TempDir(), "no-such-gh"))
	m, cmd := m.readPRsCmd(context.Background(), false, false)
	if cmd == nil {
		t.Fatal("want a read")
	}
	nm, _ := m.Update(cmd())
	m = nm.(Model)
	if m.forgeShown || m.statusMsg != "" || len(m.leftTabs()) != 4 {
		t.Fatalf("no forge must leave no trace: shown=%v status=%q tabs=%d", m.forgeShown, m.statusMsg, len(m.leftTabs()))
	}
}
