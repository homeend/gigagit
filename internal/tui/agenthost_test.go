package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
)

func TestAgentSpawnRequestRoundTrip(t *testing.T) {
	st := newAgentHostState()
	st.url = "http://127.0.0.1:1/mcp"
	m := sizedModel(t, 120, 40)
	m.agentHost = st
	focus := m.focus
	var spawned domain.SpawnSpec
	agentSpawn = func(_ context.Context, sp domain.SpawnSpec) (domain.AgentStartResult, *domain.AgentSession, error) {
		spawned = sp
		return domain.AgentStartResult{ID: "p/s7", Worktree: "/w/job", Tool: "Claude"}, nil, nil
	}
	t.Cleanup(func() { agentSpawn = domain.SpawnAgent })
	starter := starterFor(st)
	done := make(chan domain.AgentStartResult, 1)
	go func() {
		res, err := starter(context.Background(), domain.AgentStartRequest{Caller: "p/s1", Worktree: "/w/job", Tool: "Claude", Prompt: "x"})
		if err != nil {
			t.Error(err)
		}
		done <- res
	}()
	req := (waitAgentSpawnCmd(st)()).(agentSpawnRequestMsg)
	nm, cmd := m.onAgentSpawnRequest(req)
	msg := cmd().(tea.BatchMsg)[0]().(agentSpawnedMsg) // the batch's first cmd is the spawn
	nm2, _ := nm.onAgentSpawned(msg)
	select {
	case res := <-done:
		if res.ID != "p/s7" {
			t.Fatalf("reply %+v", res)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the starter never got its reply")
	}
	if spawned.MCPURL != st.url || spawned.Cols < 20 || spawned.Approved == nil {
		t.Fatalf("spec %+v", spawned)
	}
	if !strings.Contains(nm2.statusMsg, "Claude") || nm2.focus != focus {
		t.Fatalf("status %q, focus %v→%v: a spawned worker never takes focus", nm2.statusMsg, focus, nm2.focus)
	}
}

func TestStarterRefusesWhenClosing(t *testing.T) {
	st := newAgentHostState()
	Model{agentHost: st}.closeAgentHost()
	Model{agentHost: st}.closeAgentHost() // idempotent: no double close
	if _, err := starterFor(st)(context.Background(), domain.AgentStartRequest{}); err == nil {
		t.Fatal("a closing TUI must refuse")
	}
}
