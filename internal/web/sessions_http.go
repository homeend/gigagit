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

// sessionWire is one session as the page sees it. agent_state and the
// fields after it are what the agent is doing (domain.SessionStates): the
// protocol words working | idle | question, when that began, the spinner's
// own timer in seconds, the stall flag and a question's choices — absent for
// a session gg does not classify.
type sessionWire struct {
	ID         string                  `json:"id"`
	Label      string                  `json:"label"`
	Agent      string                  `json:"agent"`
	Repo       string                  `json:"repo"`
	Worktree   string                  `json:"worktree"`
	State      string                  `json:"state"`
	ExitCode   int                     `json:"exit_code"`
	Started    time.Time               `json:"started"`
	Task       string                  `json:"task,omitempty"`
	AgentState string                  `json:"agent_state,omitempty"`
	Since      time.Time               `json:"since,omitzero"`
	StepFor    int                     `json:"step_for,omitempty"`
	Stalled    bool                    `json:"stalled,omitempty"`
	Options    []domain.ActivityOption `json:"options,omitempty"`
	// An unanswered agent_report (domain.SessionReportOf): when it came,
	// whether the agent called it final, its first line.
	ReportAt    time.Time `json:"report_at,omitzero"`
	ReportFinal bool      `json:"report_final,omitempty"`
	ReportLine  string    `json:"report_line,omitempty"`
	// Which agent tours the session has (stage 4): a spawned worker's
	// brief, its latest report — the row menu offers to open them.
	HasBrief  bool `json:"has_brief,omitempty"`
	HasReport bool `json:"has_report,omitempty"`
}

// activityNoticeWire is one activity notice (a toast on every tab).
type activityNoticeWire struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"` // question | idle | stalled | report
	Label    string `json:"label"`
	Worktree string `json:"worktree"`
	QuietS   int    `json:"quiet_s,omitempty"` // stalled: seconds without output
	Text     string `json:"text,omitempty"`    // report: its first line
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
	out := sessionsWireWith(list, tasks, domain.SessionActivityOf, domain.SessionReportOf)
	for i := range out {
		out[i].HasBrief, out[i].HasReport = domain.AgentTourKinds(domain.FullSessionID(domain.SessionID(out[i].ID)))
	}
	return out
}

// sessionsWireWith is sessionsWire with the activity lookup injected.
func sessionsWireWith(list []domain.SessionInfo, tasks []domain.TaskInfo, activity func(domain.SessionID) (domain.SessionActivity, bool), report func(domain.SessionID) (domain.AgentReport, bool)) []sessionWire {
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
		if a, ok := activity(info.ID); ok {
			w.AgentState, w.Since, w.StepFor, w.Stalled, w.Options = a.Name(), a.Since, int(a.StepFor.Seconds()), a.Stalled, a.Options
		}
		if rep, ok := report(info.ID); ok {
			w.ReportAt, w.ReportFinal, w.ReportLine = rep.At, rep.Final, domain.ReportFirstLine(rep.Text)
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

// broadcastSessions tells every tab the list changed, with the activity
// notices since the last broadcast. fanOut, not emit: the op gate must not
// swallow a session's exit or a question.
func (s *Server) broadcastSessions(notices []activityNoticeWire) {
	if h := s.liveHubRef(); h != nil {
		h.fanOut(liveMsg{Changed: []string{}, Reason: "sessions", Sessions: s.sessionsNow(), Notices: notices})
	}
}

// watchSessionStates forwards activity changes and notices until stop
// closes. Its own subscription and its own notice cursor (from the
// watcher's current sequence: nothing older is replayed), so the terminal
// hosting this page reads every notice too.
func (s *Server) watchSessionStates(stop <-chan struct{}) {
	w := domain.SessionStates()
	ch, cancel := w.Subscribe()
	defer cancel()
	seq := w.NoticeSeq()
	for {
		select {
		case <-stop:
			return
		case <-ch:
			var out []activityNoticeWire
			for _, n := range w.Notices(seq) {
				seq = n.Seq
				out = append(out, activityNoticeWire{ID: string(n.ID), Kind: n.Kind, Label: n.Label, Worktree: n.Dir, QuietS: int(n.Quiet.Seconds()), Text: n.Text})
			}
			s.broadcastSessions(out)
		}
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
			s.broadcastSessions(nil)
		}
	}
}
