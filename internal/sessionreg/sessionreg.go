// Package sessionreg is the machine-wide registry of live agent sessions: each
// gg TUI publishes <proc>.json listing the sessions it hosts, so another gg
// process (the CLI) can tell which sessions are running and which worktree a
// TUI is rooted in. Liveness is the file's mtime (refreshed every second by
// the owner), the steer presence precedent. DAG leaf: stdlib + x/sys.
package sessionreg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// LiveWindow is how stale a registry may get before it counts as belonging
// to a crashed (or stalled) process — steer.LiveWindow's value.
const LiveWindow = 5 * time.Second

// Entry is one session a TUI hosts.
type Entry struct {
	ID      string `json:"id"` // <proc>/<session id> — the child's GG_SESSION_ID
	Dir     string `json:"dir"`
	Agent   string `json:"agent,omitempty"`
	Label   string `json:"label,omitempty"`
	Name    string `json:"name,omitempty"` // the user's name for the session
	State   string `json:"state"`          // "running" | "exited"
	Started string `json:"started,omitempty"`
}

// Registry is one gg process's published sessions.
type Registry struct {
	Proc     string  `json:"-"` // from the file name (Live fills it)
	PID      int     `json:"pid"`
	Started  string  `json:"started,omitempty"`
	Worktree string  `json:"worktree"`      // the TUI's own worktree
	MCP      string  `json:"mcp,omitempty"` // the TUI's agent channel URL ("" = none); never a token
	Sessions []Entry `json:"sessions"`
}

func file(dir, proc string) string { return filepath.Join(dir, proc+".json") }

// Write replaces proc's registry atomically (temp + rename).
func Write(dir, proc string, r Registry) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+proc+".*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), file(dir, proc)); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// Touch refreshes proc's liveness.
func Touch(dir, proc string) error {
	now := time.Now()
	return os.Chtimes(file(dir, proc), now, now)
}

// Remove deletes proc's registry (clean shutdown).
func Remove(dir, proc string) { os.Remove(file(dir, proc)) }

// Live returns every registry fresher than LiveWindow, removing stale and
// unparsable ones so the next reader does not even stat them.
func Live(dir string) []Registry {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Registry
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") || strings.HasPrefix(name, ".") {
			continue
		}
		p := filepath.Join(dir, name)
		st, err := e.Info()
		if err != nil {
			continue
		}
		if time.Since(st.ModTime()) > LiveWindow {
			os.Remove(p)
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var r Registry
		if json.Unmarshal(data, &r) != nil {
			os.Remove(p)
			continue
		}
		r.Proc = strings.TrimSuffix(name, ".json")
		out = append(out, r)
	}
	return out
}

// ProcOf is the "<pid>-<start>" part of a session id — its registry's name.
func ProcOf(sessionID string) string {
	proc, _, _ := strings.Cut(sessionID, "/")
	return proc
}

// PIDOf is the pid inside a session id; 0 when the id is malformed.
func PIDOf(sessionID string) int {
	pid, _, _ := strings.Cut(ProcOf(sessionID), "-")
	n, err := strconv.Atoi(pid)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// procStartSlack absorbs /proc's whole-second boot time and tick rounding.
const procStartSlack = 2 * time.Second

// ProcAliveSince reports whether pid is alive AND is the process that was
// already running at notAfter (a session id's <start>): a live pid that
// started later is a reused pid, and the tag's owner is gone. Where the
// start time is unknown, liveness is the pid alone.
func ProcAliveSince(pid int, notAfter time.Time) bool {
	if !ProcAlive(pid) {
		return false
	}
	start, ok := procStart(pid)
	if !ok {
		return true
	}
	return !start.After(notAfter.Add(procStartSlack))
}

// StartOf is the <start> inside a session id; ok is false when malformed.
func StartOf(sessionID string) (time.Time, bool) {
	_, start, ok := strings.Cut(ProcOf(sessionID), "-")
	if !ok {
		return time.Time{}, false
	}
	n, err := strconv.ParseInt(start, 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(0, n), true
}
