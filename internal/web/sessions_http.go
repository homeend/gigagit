package web

// Agent sessions on the web (web attach, plan 1): the list every tab shows
// (GET /api/sessions), the AI-task list (GET /api/tasks, read-only) and the
// "sessions" live event that keeps them fresh without polling.

import (
	"net/http"
	"sort"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/sessions", s.handleSessions)
		mux.HandleFunc("GET /api/tasks", s.handleTasks)
	})
}

// sessionWire is one session as the page sees it. agent_state/since are
// plan 3's activity detection and stay empty until then.
type sessionWire struct {
	ID         string    `json:"id"`
	Label      string    `json:"label"`
	Agent      string    `json:"agent"`
	Repo       string    `json:"repo"`
	Worktree   string    `json:"worktree"`
	State      string    `json:"state"`
	ExitCode   int       `json:"exit_code"`
	Started    time.Time `json:"started"`
	Task       string    `json:"task,omitempty"`
	AgentState string    `json:"agent_state,omitempty"`
	Since      time.Time `json:"since,omitempty"`
}

type taskWire struct {
	ID        string    `json:"id"`
	Key       string    `json:"key"`
	Kind      string    `json:"kind"`
	Agent     string    `json:"agent"`
	Mode      string    `json:"mode"`
	State     string    `json:"state"`
	Session   string    `json:"session,omitempty"`
	Submitted time.Time `json:"submitted"`
	Started   time.Time `json:"started"`
	Ended     time.Time `json:"ended"`
}

// sessionsWire joins the session list with the tasks that own sessions: a
// task-backed session is labelled "<agent> · <task key>" like the TUI's
// sub-row. Order: start time.
func sessionsWire(list []domain.SessionInfo, tasks []domain.TaskInfo) []sessionWire {
	owner := map[domain.SessionID]domain.TaskInfo{}
	for _, tk := range tasks {
		if tk.Session != "" {
			owner[tk.Session] = tk
		}
	}
	out := make([]sessionWire, 0, len(list))
	for _, info := range list {
		w := sessionWire{ID: string(info.ID), Label: info.Label, Agent: info.AgentID, Repo: info.Repo, Worktree: info.Dir,
			State: "running", ExitCode: info.ExitCode, Started: info.Started}
		if info.State == domain.SessionExited {
			w.State = "exited"
		}
		if tk, ok := owner[info.ID]; ok {
			w.Task, w.Label = string(tk.ID), info.Label+" · "+tk.Key
		}
		out = append(out, w)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Started.Before(out[j].Started) })
	return out
}

func tasksWire(list []domain.TaskInfo) []taskWire {
	out := make([]taskWire, 0, len(list))
	for _, tk := range list {
		out = append(out, taskWire{ID: string(tk.ID), Key: tk.Key, Kind: string(tk.Kind), Agent: tk.Agent, Mode: string(tk.Mode),
			State: string(tk.State), Session: string(tk.Session), Submitted: tk.Submitted, Started: tk.Started, Ended: tk.Ended})
	}
	return out
}

func (s *Server) sessionsNow() []sessionWire {
	return sessionsWire(domain.Sessions().List(), domain.Tasks().List())
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"sessions": s.sessionsNow()})
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"tasks": tasksWire(domain.Tasks().List())})
}

// broadcastSessions tells every tab the list changed. fanOut, not emit: the
// op gate must not swallow a session's exit.
func (s *Server) broadcastSessions() {
	if h := s.liveHubRef(); h != nil {
		h.fanOut(liveMsg{Changed: []string{}, Reason: "sessions", Sessions: s.sessionsNow()})
	}
}

// watchSessions forwards the manager's change signal to the tabs until stop
// closes. Started by New, stopped by Close. Its own subscription: the TUI in
// the same process has one too, and neither steals the other's wakeups.
func (s *Server) watchSessions(stop <-chan struct{}) {
	ch, cancel := domain.Sessions().Subscribe()
	defer cancel()
	for {
		select {
		case <-stop:
			return
		case <-ch:
			s.broadcastSessions()
		}
	}
}
