package web

// Console input (web attach, plan 1): POST /api/session-input types keys and
// paste into a session through the domain key table (the same emulator
// events the TUI sends); POST /api/session-size is the focused viewer
// pushing its cols×rows.

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/homeend/gigagit/internal/domain"
)

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("POST /api/session-input", writeGuard(s.handleSessionInput))
		mux.HandleFunc("POST /api/session-size", writeGuard(s.handleSessionSize))
	})
}

type sessionInputReq struct {
	ID    string              `json:"id"`
	Keys  []domain.ConsoleKey `json:"keys"`
	Paste string              `json:"paste"`
}

// sessionByID answers 404 for an unknown id.
func sessionByID(w http.ResponseWriter, id string) (*domain.AgentSession, bool) {
	s, ok := domain.Sessions().Get(domain.SessionID(id))
	if !ok {
		writeErr(w, http.StatusNotFound, errors.New("no such session"))
	}
	return s, ok
}

// handleSessionInput decodes every key BEFORE sending any, so a bad one in
// the batch refuses the whole batch rather than typing half of it.
func (s *Server) handleSessionInput(w http.ResponseWriter, r *http.Request) {
	var q sessionInputReq
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	sess, ok := sessionByID(w, q.ID)
	if !ok {
		return
	}
	ins := make([]domain.ConsoleInput, 0, len(q.Keys))
	for _, k := range q.Keys {
		in, err := domain.ConsoleKeyEvent(k)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		ins = append(ins, in)
	}
	if sess.Info().State != domain.SessionRunning {
		writeErr(w, http.StatusConflict, errors.New("the session has exited"))
		return
	}
	for _, in := range ins {
		if in.IsKey {
			sess.SendKey(in.Key)
		} else {
			sess.SendText(in.Text)
		}
	}
	if q.Paste != "" {
		sess.Paste(q.Paste)
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleSessionSize(w http.ResponseWriter, r *http.Request) {
	var q struct {
		ID   string `json:"id"`
		Cols int    `json:"cols"`
		Rows int    `json:"rows"`
	}
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	sess, ok := sessionByID(w, q.ID)
	if !ok {
		return
	}
	cols, rows := domain.ClampConsoleSize(q.Cols, q.Rows)
	if err := sess.Resize(cols, rows); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]int{"cols": cols, "rows": rows})
}
