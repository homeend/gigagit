package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// The steer switch ask — the TUI's steer_switch_ask.go. A worktree-bound
// command (a navigate to a file or a diff, a highlight) from a worktree the
// page is not showing is never applied to the page's own checkout, and the
// page does not switch on its own: ONE parked slot (the latest wins) behind a
// notice, and the agent hears at once that the user was asked.

// noticeSteerSwitch is the ask's notice id (the TUI's). PERMANENT.
const noticeSteerSwitch = "steer_switch"

type steerAskSlot struct {
	mu  sync.Mutex
	cmd *steer.Command
	// replay is a Switch's navigate, handed to the page as its start-at once
	// a re-root lands on target (and dropped by a re-root anywhere else).
	replay *steer.Command
	target string
	at     time.Time // when Switch was answered (the replay's age)
}

// steerReplayTTL is how long a Switch's navigate waits for its re-root: one
// that never happened (an op ran, the switch failed) must not replay on some
// later, unrelated switch to that worktree.
const steerReplayTTL = time.Minute

// steerWorktreeBound is the TUI's: a place in ONE checkout. A commit reveal,
// a note step, focus and reload read the same in every worktree.
func steerWorktreeBound(c steer.Command) bool {
	switch c.Cmd {
	case "navigate":
		return c.File != "" || c.Target != nil
	case "highlight":
		return true
	}
	return false
}

// shownWorktree is the checkout the page shows.
func (s *Server) shownWorktree() string {
	s.steerMu.Lock()
	wt := s.steerWorktree
	s.steerMu.Unlock()
	if wt == "" {
		wt = s.service().Root()
	}
	return wt
}

// maybeAskSteer parks c when it means a place in another worktree, and
// answers it. ok false = not an ask: apply c as always.
func (s *Server) maybeAskSteer(c steer.Command) (steer.Reply, bool) {
	shown := s.shownWorktree()
	if !steerWorktreeBound(c) || c.Worktree == "" || domain.SameCheckout(c.Worktree, shown) {
		return steer.Reply{}, false
	}
	s.steerAsk.mu.Lock()
	s.steerAsk.cmd = &c
	s.steerAsk.mu.Unlock()
	if h := s.liveHubRef(); h != nil {
		h.fanOut(liveMsg{Changed: []string{}, Reason: "notices"}) // the bell blinks now, not after the next op
	}
	return steerFail(c, fmt.Sprintf("gg web is showing worktree %s; asked the user to switch to %s", shown, c.Worktree)), true
}

// steerAskNotice renders the parked ask for the centre. Its actions keep the
// notice: the slot decides when it goes (the TUI's ask ignores session
// dismissals the same way).
func steerAskNotice(_ context.Context, s *Server, _ *domain.Service, _ model.RepoHealth, _ map[string]bool) []noticeWire {
	s.steerAsk.mu.Lock()
	c := s.steerAsk.cmd
	s.steerAsk.mu.Unlock()
	if c == nil {
		return nil
	}
	where := c.File
	if where == "" {
		where = c.Commit
	}
	if c.Line != nil {
		where += ":" + strconv.Itoa(c.Line.No)
	}
	base := filepath.Base(c.Worktree)
	return []noticeWire{{
		ID:     noticeSteerSwitch,
		Title:  fmt.Sprintf("an agent wants to show %s in %s", where, base),
		Detail: []string{"gg web is showing another worktree; nothing moves until you choose."},
		Actions: []noticeAction{
			{Label: "Switch to " + base + " and show", Steer: "switch", Keep: true},
			{Label: "Ignore", Steer: "ignore", Keep: true},
		},
	}}
}

// handleSteerAsk answers the ask (POST {act}): ignore drops it; switch takes
// it, keeps a navigate for replay once the page has re-rooted there, and
// answers the worktree the page re-roots to.
func (s *Server) handleSteerAsk(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Act string `json:"act"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil || (req.Act != "switch" && req.Act != "ignore") {
		writeErr(w, http.StatusBadRequest, errors.New(`act must be "switch" or "ignore"`))
		return
	}
	a := &s.steerAsk
	a.mu.Lock()
	defer a.mu.Unlock()
	c := a.cmd
	if c == nil {
		writeErr(w, http.StatusConflict, errors.New("nothing is asking to switch"))
		return
	}
	a.cmd = nil
	if req.Act == "ignore" {
		writeJSON(w, map[string]bool{"ok": true})
		return
	}
	a.replay, a.target, a.at = nil, c.Worktree, time.Now()
	if c.Cmd == "navigate" {
		replay := *c
		replay.Wait, replay.Worktree, replay.From = false, "", ""
		a.replay = &replay
	}
	writeJSON(w, map[string]string{"worktree": c.Worktree})
}

// steerAskRerooted is a re-root's word: the parked ask is moot, and a Switch's
// navigate replays only when the page landed where it asked.
func (s *Server) steerAskRerooted(root string) {
	a := &s.steerAsk
	a.mu.Lock()
	replay, target, fresh := a.replay, a.target, time.Since(a.at) < steerReplayTTL
	a.cmd, a.replay, a.target = nil, nil, ""
	a.mu.Unlock()
	if replay != nil && fresh && domain.SameCheckout(target, root) {
		_ = s.setStartAt(*replay) // validated when it was posted
	}
}
