package web

// The AI tasks tab (the ctrl+\\ switcher's middle tab) and the Worktrees ◆
// rows: the TUI's Headless tab (internal/tui/task_tab.go) over the process's
// task manager. Rows are its live tasks, then the history records it does
// not hold; enter shows a result, a running agent's console or a failure's
// output; k k cancels, x removes an ended record, s retries a review save.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/exttool"
)

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/tasks/result", s.handleTaskResult)
		mux.HandleFunc("POST /api/tasks/cancel", writeGuard(s.handleTaskCancel))
		mux.HandleFunc("POST /api/tasks/remove", writeGuard(s.handleTaskRemove))
		mux.HandleFunc("POST /api/tasks/retry-save", writeGuard(s.handleTaskRetrySave))
	})
}

// removedTasks are the ids x removed. A task this process ran stays in
// List() after its record is deleted (the TUI's taskTrack.removed answers
// the same), so the rows leave it out here.
var removedTasks sync.Map

// taskRowsNow is the tab's rows as they stand.
func taskRowsNow() []taskWire {
	m := domain.Tasks()
	rows := taskRowsWire(m.List(), m.History())
	out := rows[:0]
	for _, r := range rows {
		if _, gone := removedTasks.Load(r.ID); !gone {
			out = append(out, r)
		}
	}
	return out
}

// taskRowsWire orders the tab like the TUI's taskRows: List() (live tasks,
// then this process's ended ones), then the history records List() does not
// hold.
func taskRowsWire(list []domain.TaskInfo, hist []domain.TaskRecord) []taskWire {
	out := make([]taskWire, 0, len(list)+len(hist))
	seen := map[string]bool{}
	for _, tk := range list {
		seen[string(tk.ID)] = true
		out = append(out, taskWire{ID: string(tk.ID), Key: tk.Key, Kind: string(tk.Kind), Agent: tk.Agent, Mode: string(tk.Mode),
			State: string(tk.State), Session: string(tk.Session), Submitted: tk.Submitted, Started: tk.Started, Ended: tk.Ended,
			Worktree: tk.Worktree, Live: tk.State.Live(), NoteID: tk.NoteID, SaveErr: tk.SaveErr,
			HasResult: tk.NoteID != "" || (tk.SaveErr == "" && tk.Results > 0)})
	}
	for _, r := range hist {
		if seen[r.ID] {
			continue
		}
		out = append(out, taskWire{ID: r.ID, Key: r.Key, Kind: r.Kind, Agent: r.Agent, Mode: r.Mode, State: r.State,
			Started: r.Started, Ended: r.Ended, Worktree: r.Worktree, NoteID: r.NoteID,
			HasResult: r.NoteID != "" || r.ResultFile != ""})
	}
	return out
}

// findTask resolves a wire id against the live list and the history — the
// only ids the endpoints act on (a history id names a file in the store).
func findTask(id string) (info *domain.TaskInfo, rec *domain.TaskRecord) {
	if _, gone := removedTasks.Load(id); gone {
		return nil, nil
	}
	m := domain.Tasks()
	for _, tk := range m.List() {
		if string(tk.ID) == id {
			return &tk, nil
		}
	}
	for _, r := range m.History() {
		if r.ID == id {
			return nil, &r
		}
	}
	return nil, nil
}

// taskResultWire is what enter shows in the result overlay. NoteID set =
// open that review instead; Apply = a commit message for the served worktree.
type taskResultWire struct {
	Title  string `json:"title"`
	Text   string `json:"text"`
	Ext    string `json:"ext"`
	NoteID string `json:"note_id,omitempty"`
	Apply  bool   `json:"apply"`
}

// handleTaskResult is enter on a task (the TUI's openTask minus the
// console, which the page opens from the row's session itself): a review's
// stored note, else the result text, else a failure's output.
func (s *Server) handleTaskResult(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	info, rec := findTask(id)
	if info == nil && rec == nil {
		writeErr(w, http.StatusNotFound, errors.New("no such task"))
		return
	}
	m := domain.Tasks()
	var key, agentKind, worktree, state, noteID, saveErr string
	if info != nil {
		key, agentKind, worktree, state, noteID, saveErr = info.Key, string(info.Kind), info.Worktree, string(info.State), info.NoteID, info.SaveErr
	} else {
		key, agentKind, worktree, state, noteID = rec.Key, rec.Kind, rec.Worktree, rec.State, rec.NoteID
	}
	live := domain.TaskState(state).Live()
	if agentKind == string(exttool.CatReview) {
		if noteID != "" {
			writeJSON(w, taskResultWire{Title: key, NoteID: noteID})
			return
		}
		if saveErr != "" {
			writeErr(w, http.StatusConflict, fmt.Errorf("%s — s retry save", saveErr))
			return
		}
	}
	result := ""
	if info != nil && info.Results > 0 {
		result = info.Result
	} else if !live {
		result, _ = m.HistoryResult(id)
	}
	if strings.TrimSpace(result) != "" {
		out := taskResultWire{Title: key, Text: result, Ext: ".md"}
		if agentKind == string(exttool.CatCommitMessage) {
			out.Ext = ".txt"
			if top, err := s.service().TopLevel(readCtx(r)); err == nil && domain.SameCheckout(worktree, top) {
				out.Apply = true
			}
		}
		writeJSON(w, out)
		return
	}
	if state == string(domain.TaskFailed) {
		tail := ""
		if info != nil {
			tail = info.Tail
		}
		if tail == "" {
			tail, _ = m.HistoryTail(id)
		}
		if strings.TrimSpace(tail) == "" && info != nil {
			tail = info.Err
		}
		if strings.TrimSpace(tail) == "" && rec != nil {
			tail = rec.Err
		}
		writeJSON(w, taskResultWire{Title: key + " — output", Text: tail, Ext: ".log"})
		return
	}
	writeErr(w, http.StatusNotFound, errors.New("no result yet"))
}

type taskIDRequest struct {
	ID string `json:"id"`
}

// taskAction decodes {id}, resolves it, and runs act on the resolved task.
func taskAction(w http.ResponseWriter, r *http.Request, act func(info *domain.TaskInfo, rec *domain.TaskRecord) (int, error)) {
	var req taskIDRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad request body: %w", err))
		return
	}
	info, rec := findTask(req.ID)
	if info == nil && rec == nil {
		writeErr(w, http.StatusNotFound, errors.New("no such task"))
		return
	}
	if code, err := act(info, rec); err != nil {
		writeErr(w, code, err)
		return
	}
	writeJSON(w, map[string]any{})
}

// handleTaskCancel is k k: cancel a queued or running task.
func (s *Server) handleTaskCancel(w http.ResponseWriter, r *http.Request) {
	taskAction(w, r, func(info *domain.TaskInfo, _ *domain.TaskRecord) (int, error) {
		if info == nil || !info.State.Live() {
			return http.StatusConflict, errors.New("the task has already ended")
		}
		if err := domain.Tasks().Cancel(info.ID); err != nil {
			return http.StatusConflict, err
		}
		return 0, nil
	})
}

// handleTaskRemove is x: delete an ended record; a live one must be
// cancelled first.
func (s *Server) handleTaskRemove(w http.ResponseWriter, r *http.Request) {
	taskAction(w, r, func(info *domain.TaskInfo, rec *domain.TaskRecord) (int, error) {
		if info != nil && info.State.Live() {
			return http.StatusConflict, errors.New("cancel it first (k k)")
		}
		id := ""
		if info != nil {
			id = string(info.ID)
		} else {
			id = rec.ID
		}
		if err := domain.Tasks().RemoveHistory(id); err != nil {
			return http.StatusInternalServerError, err
		}
		removedTasks.Store(id, true)
		// A history edit wakes no task subscriber: tell every tab here.
		s.broadcastTasks()
		return 0, nil
	})
}

// handleTaskRetrySave is s: store a review result whose save failed.
func (s *Server) handleTaskRetrySave(w http.ResponseWriter, r *http.Request) {
	taskAction(w, r, func(info *domain.TaskInfo, _ *domain.TaskRecord) (int, error) {
		if info == nil || info.SaveErr == "" {
			return http.StatusConflict, errors.New("nothing to save again")
		}
		if err := domain.Tasks().RetrySave(info.ID); err != nil {
			return http.StatusInternalServerError, err
		}
		return 0, nil
	})
}

// watchTasks sends the rows to every tab on each task change until stop
// closes. fanOut, not emit: the op gate must not swallow a task ending.
func (s *Server) watchTasks(stop <-chan struct{}) {
	ch, cancel := domain.Tasks().Subscribe()
	defer cancel()
	for {
		select {
		case <-stop:
			return
		case <-ch:
			s.broadcastTasks()
		}
	}
}

// broadcastTasks sends the rows as they stand to every tab.
func (s *Server) broadcastTasks() {
	if h := s.liveHubRef(); h != nil {
		h.fanOut(liveMsg{Changed: []string{}, Reason: "tasks", Tasks: taskRowsNow()})
	}
}
