package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

// plantIgnored writes a .gitignore-excluded docs/specs/a.md into dir.
func plantIgnored(t *testing.T, dir string) {
	t.Helper()
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("docs/specs\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "docs", "specs"), 0o755)
	os.WriteFile(filepath.Join(dir, "docs", "specs", "a.md"), []byte("x\n"), 0o644)
}

// stageIgnoredModal drives a stage of an ignored path up to the frontend
// force-add modal and returns the model holding it.
func stageIgnoredModal(t *testing.T, m Model) Model {
	t.Helper()
	m.running = true
	msg := m.stageCmd(engine.Stage{Paths: []string{"docs/specs/a.md"}})()
	u, _ := m.Update(msg)
	m = u.(Model)
	if m.modal == nil {
		t.Fatalf("expected the stage.ignored modal, got msg %T (status %q)", msg, m.statusMsg)
	}
	if m.modal.req.ID != engine.IgnoredPathsDecisionID {
		t.Fatalf("modal decision = %q, want %q", m.modal.req.ID, engine.IgnoredPathsDecisionID)
	}
	if m.running {
		t.Fatal("running must clear while the modal waits")
	}
	return m
}

// The refused stage raises the force-add modal; enter (selection 0 =
// force-add) re-runs the stage with git add -f and the file lands in the
// index.
func TestStageIgnoredModalForceAdds(t *testing.T) {
	t.Parallel()
	dir, repo := newRepoDir(t)
	plantIgnored(t, dir)
	m := New(domain.New(repo))
	loaded, _ := m.Update(m.loadCmd()())
	m = stageIgnoredModal(t, loaded.(Model))

	u, cmd := m.Update(keyMsg("enter")) // selection 0 == "force-add"
	m = u.(Model)
	if m.modal != nil {
		t.Fatal("modal should close on enter")
	}
	if cmd == nil {
		t.Fatal("force-add must launch the forced stage")
	}
	u, _ = m.Update(cmd())
	m = u.(Model)
	if !strings.Contains(gitOut(t, dir, "diff", "--cached", "--name-only"), "docs/specs/a.md") {
		t.Fatal("docs/specs/a.md should be staged after force-add")
	}
	if m.running {
		t.Fatal("op should be finished")
	}
}

// esc answers abort: the modal closes, nothing is staged, no error lingers.
func TestStageIgnoredModalEscAborts(t *testing.T) {
	t.Parallel()
	dir, repo := newRepoDir(t)
	plantIgnored(t, dir)
	m := New(domain.New(repo))
	loaded, _ := m.Update(m.loadCmd()())
	m = stageIgnoredModal(t, loaded.(Model))

	u, _ := m.Update(keyMsg("esc"))
	m = u.(Model)
	if m.modal != nil {
		t.Fatal("modal should close on esc")
	}
	if out := gitOut(t, dir, "diff", "--cached", "--name-only"); strings.Contains(out, "docs/specs/a.md") {
		t.Fatalf("nothing should be staged on abort; cached names = %q", out)
	}
}
