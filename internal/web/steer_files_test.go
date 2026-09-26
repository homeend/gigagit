package web

import (
	"net/http"
	"testing"

	"github.com/homeend/gigagit/internal/steer"
)

// steerAsk posts one command and decodes the synchronous answer.
func steerAsk(t *testing.T, s *Server, body string) (int, steer.Reply) {
	t.Helper()
	var rep steer.Reply
	code := postJSON(t, serve(t, s), "/api/session/steer", body, "application/json", "", &rep)
	return code, rep
}

func TestSteerFilesAnswersFromTheList(t *testing.T) {
	t.Parallel()
	s := newSteerServer(t)
	code, rep := steerAsk(t, s, `{"id":"1-1","cmd":"files"}`)
	if code != http.StatusOK || !rep.OK || rep.ID != "1-1" || rep.Detail != "no open files" || len(rep.Files) != 0 {
		t.Fatalf("empty: code=%d rep=%+v", code, rep)
	}
	s.ofs.open(s.service().Root(), ofKey{Src: "worktree", Path: "f.txt"}, "", 3)
	code, rep = steerAsk(t, s, `{"id":"1-2","cmd":"files"}`)
	if code != http.StatusOK || !rep.OK || len(rep.Files) != 1 {
		t.Fatalf("code=%d rep=%+v", code, rep)
	}
	if f := rep.Files[0]; f.ID != "f1" || f.Path != "f.txt" || f.Source != "worktree" || f.Line != 3 || f.State != "background" {
		t.Fatalf("file = %+v", f)
	}
}

// files is read-only: an op in flight does not stop it (only a steer the hub
// must deliver is refused with 409).
func TestSteerFilesAnswersWhileAnOpIsInFlight(t *testing.T) {
	t.Parallel()
	s := newSteerServer(t)
	s.cur = &opRun{} // opInFlight() is true
	if code, rep := steerAsk(t, s, `{"id":"1-1","cmd":"files"}`); code != http.StatusOK || !rep.OK {
		t.Fatalf("code=%d rep=%+v", code, rep)
	}
}
