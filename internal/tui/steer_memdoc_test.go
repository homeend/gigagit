package tui

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

const memdocTestID = "20261102T031500-a1b2"

// memdocModel is a steering Model with its own store, and an inbox resolver
// that hands out one temp directory per worktree.
func memdocModel(t *testing.T) (Model, string) {
	t.Helper()
	m, _ := steerModel(t)
	m.docs = agentdocs.New()
	t.Cleanup(m.docs.Close)
	inbox := t.TempDir()
	m.memdocs.resolve = func(context.Context, string) (string, error) { return inbox, nil }
	return m, inbox
}

func memdocAdd(wt string) steer.Command {
	plain := true
	return steer.Command{ID: "c1", Cmd: "memdoc_add", FileID: memdocTestID, Title: "T", Text: "text", Worktree: wt, Plain: &plain}
}

func TestMemdocAddFromOtherWorktreeNoScreenChange(t *testing.T) {
	t.Parallel()
	m, _ := memdocModel(t)
	m.width, m.height = 100, 30
	m.currentWorktree = t.TempDir()
	before := m.View()
	other := t.TempDir()
	got, cmd := m.applySteer(memdocAdd(other))
	m = drainCmd(t, got, cmd)
	if v := m.View(); v != before {
		t.Fatalf("the screen moved:\n%s", v)
	}
	if m.steerAsk != nil {
		t.Fatal("a memory document raised the switch ask")
	}
	o, ok := m.docs.OverviewIn(domain.CheckoutKey(other), memdocTestID)
	if !ok || !o.Memory || !o.Plain || o.Text != "text" {
		t.Fatalf("doc = %+v, %v", o, ok)
	}
}

func TestMemdocAnsweredWhileBlocked(t *testing.T) {
	t.Parallel()
	m, _ := memdocModel(t)
	m.webActive = true
	m, _ = m.applySteer(memdocAdd(t.TempDir()))
	if len(m.docs.MemoryWorktrees()) != 1 {
		t.Fatal("a blocked terminal refused a memory document")
	}
}

func TestMemdocAnsweredWhileUserBusy(t *testing.T) {
	t.Parallel()
	m, _ := memdocModel(t)
	m.loading = true // an operation is running: every screen verb is refused
	m, _ = m.applySteer(memdocAdd(t.TempDir()))
	if len(m.docs.MemoryWorktrees()) != 1 {
		t.Fatal("an operation in flight refused a memory document")
	}
}

func TestMemdocPresenceWrittenBeforeReply(t *testing.T) {
	t.Parallel()
	m, inbox := memdocModel(t)
	wt := t.TempDir()
	c := memdocAdd(wt)
	c.Wait, c.From = true, inbox
	_, cmd := m.applySteer(c)
	if cmd == nil {
		t.Fatal("no reply command")
	}
	if _, ok := steer.Live(inbox, steer.MemdocsPresence); ok {
		t.Fatal("presence written on the update goroutine")
	}
	cmd()
	if _, ok := steer.Live(inbox, steer.MemdocsPresence); !ok {
		t.Fatal("no presence once the reply was written")
	}
	if _, err := os.Stat(filepath.Join(inbox, steer.TUIPresence)); err == nil {
		t.Fatal("a memory document must not write the TUI presence")
	}
}

func TestMemdocPresenceRemovedWithLastDoc(t *testing.T) {
	t.Parallel()
	m, inbox := memdocModel(t)
	wt := t.TempDir()
	got, cmd := m.applySteer(memdocAdd(wt))
	m = drainCmd(t, got, cmd)
	m = m.tendMemdocPresence()
	if _, ok := steer.Live(inbox, steer.MemdocsPresence); !ok {
		t.Fatal("no presence")
	}
	m, _ = m.applySteer(steer.Command{ID: "c2", Cmd: "memdoc_rm", FileID: memdocTestID, Worktree: wt})
	m = m.tendMemdocPresence()
	if _, ok := steer.Live(inbox, steer.MemdocsPresence); ok {
		t.Fatal("presence outlived the last document")
	}
	if len(m.memdocs.byWorktree) != 0 {
		t.Fatal("bookkeeping left behind")
	}
}

func TestMemdocOnlyInboxRefusesOtherVerbs(t *testing.T) {
	t.Parallel()
	m, inbox := memdocModel(t)
	got, cmd := m.applySteer(memdocAdd(t.TempDir()))
	m = drainCmd(t, got, cmd)
	if _, err := steer.Post(inbox, steer.Command{Cmd: "focus", Panel: "branches", Wait: true}); err != nil {
		t.Fatal(err)
	}
	focused := m.focus
	m, cmd = m.drainSteer()
	if cmd != nil {
		cmd()
	}
	if m.focus != focused {
		t.Fatal("a verb posted to a memdoc-only inbox moved the screen")
	}
}

func TestMemdocNotWrittenToDocsDir(t *testing.T) {
	t.Parallel()
	m, _ := memdocModel(t)
	wt, docs := t.TempDir(), t.TempDir()
	if err := m.docs.Attach(domain.CheckoutKey(wt), wt, docs); err != nil {
		t.Fatal(err)
	}
	m, _ = m.applySteer(memdocAdd(wt))
	es, _ := os.ReadDir(docs)
	if len(es) != 0 {
		t.Fatalf("on disk: %v", es)
	}
}

func TestAnotherLiveGGWinsMemdocInbox(t *testing.T) {
	t.Parallel()
	m, inbox := memdocModel(t)
	if err := steer.Touch(inbox, steer.TUIPresence, steer.Presence{PID: os.Getpid() + 1}); err != nil {
		t.Fatal(err)
	}
	got, cmd := m.applySteer(memdocAdd(t.TempDir()))
	m = drainCmd(t, got, cmd)
	m = m.tendMemdocPresence()
	if _, ok := steer.Live(inbox, steer.MemdocsPresence); ok {
		t.Fatal("wrote a presence in another live gg's inbox")
	}
	if len(m.memdocOnlyInboxes()) != 0 {
		t.Fatal("drains another live gg's inbox")
	}
}

func TestMemdocVerbsShowListRm(t *testing.T) {
	t.Parallel()
	s := agentdocs.New()
	wt := t.TempDir()
	root := domain.CheckoutKey(wt)
	if r := agentdocs.AnswerMemdoc(s, root, memdocAdd(wt)); !r.OK {
		t.Fatal(r.Error)
	}
	if r := agentdocs.AnswerMemdoc(s, root, steer.Command{Cmd: "memdoc_list", Worktree: wt}); !r.OK || len(r.Overviews) != 1 || r.Overviews[0].Text != "" || r.Overviews[0].Backing != agentdocs.BackingMemory {
		t.Fatalf("list = %+v", r)
	}
	if r := agentdocs.AnswerMemdoc(s, root, steer.Command{Cmd: "memdoc_show", FileID: memdocTestID, Worktree: wt}); !r.OK || r.Overviews[0].Text != "text" {
		t.Fatalf("show = %+v", r)
	}
	if r := agentdocs.AnswerMemdoc(s, root, steer.Command{Cmd: "memdoc_show", FileID: "nope", Worktree: wt}); r.OK || r.Error != "no overview nope" {
		t.Fatalf("show none = %+v", r)
	}
	if r := agentdocs.AnswerMemdoc(s, root, steer.Command{Cmd: "memdoc_rm", FileID: memdocTestID, Worktree: wt}); !r.OK {
		t.Fatal(r.Error)
	}
}
