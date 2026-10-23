package tui

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
)

// a worker's tour (brief / report) in ANOTHER member is filed as a
// memory document — attachTourRoot attaches only the live member's
// worktrees (isRepoWorktree) — and the first look at that worktree
// attaches its docs directory, which drops every memory document of the
// root: Open brief / Open report on a worker in member B (viewed from A)
// switches there and shows nothing, and a report filed while B slept is
// gone (its seq is recorded as filed, so it is not filed again).
func TestTourOfAWorkerInAnotherMemberSurvivesTheLook(t *testing.T) {
	restore := domain.UseSessionManager(agentsession.NewManager())
	t.Cleanup(func() { domain.Sessions().KillAll(context.Background()); restore() })
	t.Cleanup(domain.UseSessionStates(domain.NewStaticStates(nil)))
	m := loadedModel(t)
	m.width, m.height = 160, 50
	m, b := openTwo(t, m)
	tc := config.ToolCommand{Category: "session", Name: "Sh", Mode: "session", Command: "sh -c 'sleep 600'"}
	s, _, err := domain.Open(b.mainPath).StartAgentSession(context.Background(), tc, b.mainPath, "", 80, 24, nil, "",
		domain.SpawnRecord{Parent: "x/ov", Brief: "# Task\nstart at [a](a.go:1)", Worktree: b.mainPath, Spawned: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	m.docs = agentdocs.New()
	t.Cleanup(m.docs.Close)
	docsRoot := t.TempDir()
	m.docsDirOf = func(w string) string { return filepath.Join(docsRoot, filepath.Base(w)) } // production: every worktree has one
	id := s.Info().ID
	key := "brief:" + domain.FullSessionID(id)
	m, _ = m.openTour(id, "brief")
	if m.liveRepo() != b {
		t.Fatalf("fixture: no look into B: %s", m.statusMsg)
	}
	if _, ok := m.docs.TourID(key); !ok {
		t.Fatal("the brief tour filed for the worker in member B was dropped when B's slot attached its docs directory — Open brief shows nothing")
	}
}
