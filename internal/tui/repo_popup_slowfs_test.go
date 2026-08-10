package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/repos"
)

func TestRepoPopupSlowFSRowMarkerAndWarning(t *testing.T) {
	m := Model{width: 100, height: 30}
	fast := "/home/fake/fastrepo"
	slow := "/mnt/fake/slowrepo"
	m = m.pushLayer(&repoPopup{
		entries: []repos.Entry{
			{Path: fast, LastOpened: time.Now()},
			{Path: slow, LastOpened: time.Now()},
		},
		now: time.Now(),
	})

	u, _ := m.Update(repoFSMsg{foreign: map[string]bool{slow: true}})
	m = u.(Model)
	p := layerOf[*repoPopup](m)
	if p == nil {
		t.Fatal("popup gone after repoFSMsg")
	}

	out := p.box(m)
	if strings.Count(out, "(slow fs)") != 1 {
		t.Fatalf("want exactly one (slow fs) marker (on the slow row):\n%s", out)
	}
	if strings.Contains(out, "switching may be very slow") {
		t.Fatalf("warning line shown while a local repo is selected:\n%s", out)
	}

	p.moveSel(1) // select the slow row
	out = p.box(m)
	if !strings.Contains(out, "switching may be very slow") {
		t.Fatalf("selected slow row must show the warning line:\n%s", out)
	}
}

// TestRepoPopupSlowFSWidth pins that the marker suffix never pushes a row past
// the terminal width (the cutoff/window math must absorb it).
func TestRepoPopupSlowFSWidth(t *testing.T) {
	m := Model{width: 80, height: 24}
	long := "/mnt/very/deeply/nested/path/that/is/way/longer/than/the/popup/box/myrepo"
	m = m.pushLayer(&repoPopup{
		entries: []repos.Entry{{Path: long, LastOpened: time.Now()}},
		now:     time.Now(),
	})
	u, _ := m.Update(repoFSMsg{foreign: map[string]bool{long: true}})
	m = u.(Model)
	p := layerOf[*repoPopup](m)
	out := p.box(m)
	for _, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w > m.width {
			t.Errorf("popup line exceeds width (%d): %q", w, line)
		}
	}
}

// TestRepoPopupFSMsgAfterCloseIsNoop pins the late-delivery race: the probe
// result landing after the popup closed must be dropped, not panic.
func TestRepoPopupFSMsgAfterCloseIsNoop(t *testing.T) {
	m := Model{width: 100, height: 30}
	u, cmd := m.Update(repoFSMsg{foreign: map[string]bool{"/x": true}})
	if cmd != nil {
		t.Fatal("late repoFSMsg must be a no-op")
	}
	_ = u
}

// TestOpenRepoPopupFiresProbe pins that opening the switcher starts the async
// probe and that the probe covers every listed entry.
func TestOpenRepoPopupFiresProbe(t *testing.T) {
	m, _, otherDir := seededModel(t)
	mm, cmd, ok := m.openRepoPopup()
	if !ok {
		t.Fatal("openRepoPopup refused with a seeded registry")
	}
	if layerOf[*repoPopup](mm) == nil {
		t.Fatal("popup layer missing")
	}
	if cmd == nil {
		t.Fatal("openRepoPopup must return the probe cmd")
	}
	msg := cmd()
	fs, isFS := msg.(repoFSMsg)
	if !isFS {
		t.Fatalf("probe cmd returned %T, want repoFSMsg", msg)
	}
	if _, covered := fs.foreign[otherDir]; !covered {
		t.Fatalf("probe result missing entry %s: %+v", otherDir, fs.foreign)
	}
}
