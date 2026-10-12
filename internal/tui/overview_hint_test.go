package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

func TestSteerNavigateOpensAnOverview(t *testing.T) {
	t.Parallel()
	m, id := commitOverviewModel(t) // Task 3's helper
	c := steer.Command{Cmd: "navigate", HintKind: model.OverviewHintKind, HintID: id}
	// No counts hold it yet: an agent that just saved it links it at once.
	m, cmd := m.steerNavigate(c)
	m = drainCmds(t, m, cmd)
	if sv := m.splitLayer(); sv == nil || sv.left.doc() == nil || sv.left.doc().src.rev != id {
		t.Fatal("a ?overview= link opens that overview")
	}
}

// A link to a removed overview opens its commit and says so.
func TestSteerNavigateGoneOverview(t *testing.T) {
	t.Parallel()
	m, id := commitOverviewModel(t)
	if err := m.svc.RemoveOverview(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	c := steer.Command{Cmd: "navigate", HintKind: model.OverviewHintKind, HintID: id, Commit: m.commits[0].Hash}
	m, cmd := m.steerNavigate(c)
	m = drainCmds(t, m, cmd)
	if m.splitLayer() != nil || !strings.Contains(m.statusMsg, "no longer exists") {
		t.Fatalf("top %T status %q", m.topLayer(), m.statusMsg)
	}
}
