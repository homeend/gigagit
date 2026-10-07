package domain

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/sessionreg"
)

// SessionRegistryDir is where every TUI publishes its live sessions.
func SessionRegistryDir() string { return stateBaseDir("sessions") }

// SessionRef is one session as the worktree inventory reports it.
type SessionRef struct {
	ID    string `json:"id"`
	Agent string `json:"agent"`
	State string `json:"state"`
}

// PublishSessions keeps this process's registry current until ctx ends:
// rewritten on every session-list change, touched every second, removed on
// return. worktree reports the TUI's current worktree (it changes on reRoot);
// mcpURL is the TUI's agent channel ("" = none), published for discovery.
func PublishSessions(ctx context.Context, dir string, worktree func() string, mcpURL string) {
	if dir == "" {
		return
	}
	proc := agentsession.ProcTag()
	started := time.Now().UTC().Format(time.RFC3339)
	changed, stop := Sessions().Subscribe()
	defer stop()
	// dirty: the last write failed (on Windows a rename over a file a reader
	// holds open fails) — retried on the next tick so a new session is never
	// left unlisted until the NEXT change.
	dirty := false
	write := func() { dirty = sessionreg.Write(dir, proc, snapshotRegistry(started, worktree(), mcpURL)) != nil }
	write()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	defer sessionreg.Remove(dir, proc)
	lastWT := worktree()
	for {
		select {
		case <-ctx.Done():
			return
		case <-changed:
			write()
		case <-tick.C:
			if wt := worktree(); wt != lastWT || dirty {
				lastWT = wt
				write()
			} else if sessionreg.Touch(dir, proc) != nil {
				write() // swept by a reader while we stalled
			}
		}
	}
}

func snapshotRegistry(started, wt, mcpURL string) sessionreg.Registry {
	r := sessionreg.Registry{PID: os.Getpid(), Started: started, Worktree: wt, MCP: mcpURL}
	for _, in := range Sessions().List() {
		r.Sessions = append(r.Sessions, sessionreg.Entry{
			ID: agentsession.ProcTag() + "/" + string(in.ID), Dir: in.Dir, Agent: in.AgentID,
			Label: in.Label, Name: in.Name, State: sessionStateName(in.State), Started: in.Started.UTC().Format(time.RFC3339),
		})
	}
	return r
}

func sessionStateName(s agentsession.State) string {
	if s == agentsession.Running {
		return "running"
	}
	return "exited"
}

// liveView is one read of every live registry plus this process's own
// sessions.
type liveView struct {
	running map[string]bool   // full session id -> running
	agents  map[string]string // full session id -> agent tool id
	procs   map[string]bool   // procs with a LIVE registry (plus this process)
	byDir   map[string][]SessionRef
	tuis    []string // TUI worktrees
}

// sessionDead: its process's registry is live and does not list it running,
// or its process has no live registry and is gone. A registry that only went
// stale while its process lives (suspend, a frozen host) never kills.
func sessionDead(id string, lv liveView) bool {
	if lv.running[id] {
		return false
	}
	proc := sessionreg.ProcOf(id)
	if proc == "" || proc == id {
		return true // malformed
	}
	if lv.procs[proc] {
		return true
	}
	if start, ok := sessionreg.StartOf(id); ok {
		return !sessionreg.ProcAliveSince(sessionreg.PIDOf(id), start)
	}
	return !sessionreg.ProcAlive(sessionreg.PIDOf(id))
}

// readLive merges every live registry with THIS process's own sessions (so a
// TUI's marks are right before its first registry write, and a registry
// swept during a stall never hides our own sessions).
func readLive(dir string) liveView {
	lv := liveView{running: map[string]bool{}, agents: map[string]string{},
		procs: map[string]bool{agentsession.ProcTag(): true}, byDir: map[string][]SessionRef{}}
	add := func(e sessionreg.Entry) {
		if _, dup := lv.running[e.ID]; dup {
			return
		}
		lv.running[e.ID] = e.State == "running"
		// A terminal or custom command has no tool id: name it by its label.
		agent := e.Agent
		if agent == "" {
			agent = e.Label
		}
		lv.agents[e.ID] = agent
		d := filepath.Clean(e.Dir)
		lv.byDir[d] = append(lv.byDir[d], SessionRef{ID: e.ID, Agent: e.Agent, State: e.State})
	}
	for _, e := range snapshotRegistry("", "", "").Sessions {
		add(e)
	}
	if dir != "" {
		for _, r := range sessionreg.Live(dir) {
			lv.procs[r.Proc] = true
			if r.Worktree != "" {
				lv.tuis = append(lv.tuis, r.Worktree)
			}
			for _, e := range r.Sessions {
				add(e)
			}
		}
	}
	return lv
}

// AgentHostInfo is one live gg TUI as its registry file describes it.
type AgentHostInfo struct {
	PID      int                `json:"pid"`
	Worktree string             `json:"worktree"`
	MCP      string             `json:"mcp,omitempty"`
	Sessions []AgentHostSession `json:"sessions"`
}

// AgentHostSession is one session a live TUI hosts.
type AgentHostSession struct {
	ID    string `json:"id"`
	Agent string `json:"agent,omitempty"`
	Label string `json:"label,omitempty"`
	Name  string `json:"name,omitempty"`
	Dir   string `json:"dir"`
	State string `json:"state"`
}

// LiveAgentHosts lists every live gg TUI on this machine (read-only, no
// channel call): what `gg agent list` shows outside a gg console.
func (s *Service) LiveAgentHosts() []AgentHostInfo { return liveAgentHostsIn(s.registryDir()) }

func liveAgentHostsIn(dir string) []AgentHostInfo {
	if dir == "" {
		return nil
	}
	var out []AgentHostInfo
	for _, r := range sessionreg.Live(dir) {
		h := AgentHostInfo{PID: r.PID, Worktree: r.Worktree, MCP: r.MCP}
		for _, e := range r.Sessions {
			h.Sessions = append(h.Sessions, AgentHostSession{ID: e.ID, Agent: e.Agent, Label: e.Label, Name: e.Name, Dir: e.Dir, State: e.State})
		}
		out = append(out, h)
	}
	return out
}
