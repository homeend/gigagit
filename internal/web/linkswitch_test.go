package web

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/linknav"
	"github.com/homeend/gigagit/internal/model"
)

// linkSwitchWT2 is a second worktree of sessOvServer's repository, in gg's
// history (a link resolves only to a checkout gg has opened).
func linkSwitchWT2(t *testing.T, s *Server, top string) string {
	t.Helper()
	wt2 := filepath.Join(filepath.Dir(top), filepath.Base(top)+"-wt2")
	if out, err := exec.Command("git", "-C", top, "worktree", "add", "-q", wt2).CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v %s", err, out)
	}
	t.Cleanup(func() { os.RemoveAll(wt2) })
	touchMRU(context.Background(), domain.Open(wt2), s.reposStatePath())
	return wt2
}

func fileLink(dir, path string, line int) string {
	return model.Link{Repo: model.LinkRepo{Abs: filepath.ToSlash(dir)}, Path: path, Line: line}.String()
}

func postLinkSwitch(t *testing.T, s *Server, link string, out any) int {
	t.Helper()
	return postJSON(t, serve(t, s), "/api/link-switch", `{"link":`+strconv.Quote(link)+`}`, "application/json", "", out)
}

// switchedStartAt re-roots s onto dir (what the page's doReroot does) and
// reads the start-at the page lands with.
func switchedStartAt(t *testing.T, s *Server, dir string) *steerWire {
	t.Helper()
	if err := s.adoptService(context.Background(), domain.Open(dir)); err != nil {
		t.Fatal(err)
	}
	var at startAtBody
	getJSON(t, serve(t, s), "/api/session/start-at", &at)
	return at.Steer
}

// The TUI's # confirm, enter: the link's place lands once the page shows the
// link's checkout (spec 2026-10-29-web-link-switch).
func TestLinkSwitchLandsAfterTheSwitch(t *testing.T) {
	s, top, _ := sessOvServer(t)
	wt2 := linkSwitchWT2(t, s, top)
	var b struct{ Checkout string }
	if code := postLinkSwitch(t, s, fileLink(wt2, "f.txt", 1), &b); code != http.StatusOK || !domain.SamePath(b.Checkout, wt2) {
		t.Fatalf("code=%d body=%+v", code, b)
	}
	at := switchedStartAt(t, s, wt2)
	if at == nil || at.Cmd != "navigate" || at.File != "f.txt" || at.Line != 1 {
		t.Fatalf("start-at = %+v", at)
	}
}

// A session overview link into the other worktree opens as its document.
func TestLinkSwitchSessionOverview(t *testing.T) {
	s, top, _ := sessOvServer(t)
	wt2 := linkSwitchWT2(t, s, top)
	cd, _ := s.service().GitCommonDir(context.Background())
	docs2 := linknav.SessionDocsDir(cd, wt2)
	if err := os.MkdirAll(docs2, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDoc(t, docs2, "ov1.md", sessOvDoc)
	if code := postLinkSwitch(t, s, sessOvLink(t, wt2, "ov1"), nil); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if at := switchedStartAt(t, s, wt2); at == nil || at.Cmd != "file_focus" || at.FileID != "ov1" {
		t.Fatalf("start-at = %+v", at)
	}
}

// A repository link only switches.
func TestLinkSwitchRepoLinkArmsNothing(t *testing.T) {
	s, top, _ := sessOvServer(t)
	wt2 := linkSwitchWT2(t, s, top)
	if code := postLinkSwitch(t, s, model.Link{Repo: model.LinkRepo{Abs: filepath.ToSlash(wt2)}}.String(), nil); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if at := switchedStartAt(t, s, wt2); at != nil {
		t.Fatalf("start-at = %+v, want none", at)
	}
}

// The page shows the link's checkout already (it switched meanwhile): no
// switch to make, nothing armed.
func TestLinkSwitchThisCheckoutRefused(t *testing.T) {
	s, top, _ := sessOvServer(t)
	if code := postLinkSwitch(t, s, fileLink(top, "f.txt", 1), nil); code != http.StatusConflict {
		t.Fatalf("code = %d, want 409", code)
	}
	s.steerAsk.mu.Lock()
	armed := s.steerAsk.replay != nil
	s.steerAsk.mu.Unlock()
	if armed {
		t.Fatal("a replay was armed")
	}
}

func TestLinkSwitchBadLink(t *testing.T) {
	s, _, _ := sessOvServer(t)
	if code := postLinkSwitch(t, s, "gg://nowhere-known/x.txt:1", nil); code != http.StatusUnprocessableEntity {
		t.Fatalf("code = %d, want 422", code)
	}
}
