package domain

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/config"
)

// useSessions installs a fresh manager and registry for one serial test.
func useSessions(t *testing.T) {
	t.Helper()
	restoreMgr := UseSessionManager(agentsession.NewManager())
	restoreReg := useSpawnRegistry()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		Sessions().KillAll(ctx)
		restoreReg()
		restoreMgr()
	})
}

func sleeper() config.ToolCommand {
	return config.ToolCommand{Category: "session", Name: "Sleeper", Mode: "session", Command: "sh -c 'sleep 600' <prompt>"}
}

func TestStartAgentSessionMintsAndBindsAToken(t *testing.T) {
	useSessions(t)
	main, svc := newRealRepo(t)
	s, tok, err := svc.StartAgentSession(context.Background(), sleeper(), main, "", 80, 24, nil, "http://127.0.0.1:1/mcp", SpawnRecord{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) != 64 {
		t.Fatalf("token %q", tok)
	}
	id, ok := VerifyAgentToken(tok)
	if !ok || id != FullSessionID(s.Info().ID) {
		t.Fatalf("verify = %q %v", id, ok)
	}
	if _, ok := VerifyAgentToken(strings.Repeat("0", 64)); ok {
		t.Fatal("an unknown token verified")
	}
	Sessions().Kill(s.Info().ID)
	<-s.Done()
	if _, ok := VerifyAgentToken(tok); ok {
		t.Fatal("an exited session's token must stop working")
	}
}

func TestStartAgentSessionWithoutURLHasNoToken(t *testing.T) {
	useSessions(t)
	main, svc := newRealRepo(t)
	_, tok, err := svc.StartAgentSession(context.Background(), sleeper(), main, "", 80, 24, nil, "", SpawnRecord{}, "")
	if err != nil || tok != "" {
		t.Fatalf("tok %q err %v", tok, err)
	}
}

func TestAgentDescends(t *testing.T) {
	useSessions(t)
	bindRecord("p/s2", SpawnRecord{Parent: "p/s1", Spawned: true})
	bindRecord("p/s3", SpawnRecord{Parent: "p/s2", Spawned: true})
	if !AgentDescends("p/s2", "p/s1") || !AgentDescends("p/s3", "p/s1") {
		t.Fatal("children and grandchildren descend")
	}
	if AgentDescends("p/s1", "p/s2") || AgentDescends("p/s4", "p/s1") || AgentDescends("p/s1", "p/s1") {
		t.Fatal("parents, strangers and self do not")
	}
}

func TestSpawnSlotsAreRaceFree(t *testing.T) {
	useSessions(t)
	var mu sync.Mutex
	got := 0
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, ok := reserveSpawnSlot(3); ok {
				mu.Lock()
				got++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if got != 3 {
		t.Fatalf("%d of 8 racing reservations won 3 slots", got)
	}
}

// The kickoff points the worker at its protocol, so briefs need not repeat it.
func TestKickoffNamesTheDelegateSkill(t *testing.T) {
	if !strings.Contains(AgentKickoff, "Worker protocol section of the gg-delegate skill") || !strings.Contains(AgentKickoff, "gg agent task") ||
		!strings.Contains(AgentKickoff, "gg skill path gg-delegate") {
		t.Fatalf("kickoff = %q", AgentKickoff)
	}
}

func TestKickoffIsShellSafe(t *testing.T) {
	if strings.ContainsAny(AgentKickoff, "\"'%!^&|<>$`\\") {
		t.Fatalf("the kick-off must be safe in sh and cmd.exe quoting: %q", AgentKickoff)
	}
}
