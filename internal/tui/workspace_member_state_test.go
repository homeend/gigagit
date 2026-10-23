package tui

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// Probe round 2: repository-wide state that a member switch must not
// carry into the arriving member.

// a pull-request read in flight when the user switches member is
// dropped by its old generation without clearing prsInflight: no PR read
// can start again for the rest of the session (r, the lane, the probe).
func TestPRReadInFlightAcrossMemberSwitch(t *testing.T) {
	m := loadedModel(t)
	m, b := openTwo(t, m)
	a := m.homeRepo()
	m, cmd := m.readPRsCmd(context.Background(), false, false) // A's list read, in flight
	if cmd == nil || !m.prsInflight {
		t.Fatal("fixture: no read in flight")
	}
	stale := m.prsGen
	nm, ok := m.switchRepo(b, "", true)
	if !ok {
		t.Fatal(nm.statusMsg)
	}
	m = nm
	um, _ := m.Update(prsLoadedMsg{gen: stale}) // A's read lands after the switch
	m = um.(Model)
	nm, ok = m.switchRepo(a, "", true)
	if !ok {
		t.Fatal(nm.statusMsg)
	}
	m = nm
	if _, again := m.readPRsCmd(context.Background(), false, false); again == nil {
		t.Fatalf("after a member switch under an in-flight PR read, no PR read can start again (prsInflight=%v stuck)", m.prsInflight)
	}
}

// an interrupted GitHub send is remembered Model-wide by PR
// number; the next notice rebuild in member B (its first health probe)
// shows A's "A send to #7 was interrupted" notice in B, whose Finish /
// Discard act on B's service — B's PR #7.
func TestInterruptedSendStaysWithItsMember(t *testing.T) {
	m := loadedModel(t)
	m, b := openTwo(t, m)
	m.interrupted = map[int]interruptedSend{7: {review: "R1", keys: []string{"k1"}}}
	m = m.rebuildNotices()
	nm, ok := m.switchRepo(b, "", true)
	if !ok {
		t.Fatal(nm.statusMsg)
	}
	m = nm
	m, _ = m.applyRepoHealth(repoHealthMsg{gen: m.noticeGen, health: model.RepoHealth{GitCommonDir: filepath.Join(b.mainPath, ".git")}})
	for _, n := range m.notices {
		if n.id == "interrupted_send_7" {
			t.Fatalf("member B shows A's interrupted send notice %q (its actions send to B's PR #7)", n.title)
		}
	}
}
