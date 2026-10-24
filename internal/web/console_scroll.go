package web

import (
	"encoding/json"
	"errors"
	"net/http"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/homeend/gigagit/internal/domain"
)

// The console's scrollback and mouse (the TUI's console_scroll.go and
// console_mouse.go): a frozen history to read back through, and the mouse
// passed to a program that asks for it.

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/session-history", s.handleSessionHistory)
		mux.HandleFunc("POST /api/session-mouse", writeGuard(s.handleSessionMouse))
	})
}

// handleSessionHistory is scroll mode's frozen view: every scrollback line,
// then the screen, as styled runs; screen_top is where the screen begins.
func (s *Server) handleSessionHistory(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionByID(w, r.URL.Query().Get("id"))
	if !ok {
		return
	}
	h := sess.History()
	lines := make([][]domain.ScreenRun, h.Len())
	for i := range lines {
		lines[i] = h.Runs(i)
	}
	rows := sess.Input().Rows
	writeJSON(w, map[string]any{"lines": lines, "cols": h.Width(), "screen_top": max(0, h.Len()-rows)})
}

var mouseButtons = map[string]uv.MouseButton{
	"none": uv.MouseNone, "left": uv.MouseLeft, "middle": uv.MouseMiddle, "right": uv.MouseRight,
	"wheelup": uv.MouseWheelUp, "wheeldown": uv.MouseWheelDown,
}

// handleSessionMouse sends one mouse event: to the program when it enabled
// a mouse mode; a wheel on an alternate screen without one as ↑/↓ (xterm's
// alternate scroll); otherwise nothing — the page scrolls back instead.
func (s *Server) handleSessionMouse(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	var q struct {
		ID     string `json:"id"`
		Kind   string `json:"kind"`
		Button string `json:"button"`
		X      int    `json:"x"`
		Y      int    `json:"y"`
		Mod    int    `json:"mod"` // 1 shift, 2 ctrl, 4 alt (console.js modOf)
	}
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	b, ok := mouseButtons[q.Button]
	if !ok || q.X < 0 || q.Y < 0 || q.X > 1000 || q.Y > 1000 {
		writeErr(w, http.StatusBadRequest, errors.New("bad mouse event"))
		return
	}
	var mod uv.KeyMod
	if q.Mod&1 != 0 {
		mod |= uv.ModShift
	}
	if q.Mod&2 != 0 {
		mod |= uv.ModCtrl
	}
	if q.Mod&4 != 0 {
		mod |= uv.ModAlt
	}
	mm := uv.Mouse{X: q.X, Y: q.Y, Button: b, Mod: mod}
	var ev domain.SessionMouse
	switch q.Kind {
	case "press":
		ev = uv.MouseClickEvent(mm)
	case "release":
		ev = uv.MouseReleaseEvent(mm)
	case "motion":
		ev = uv.MouseMotionEvent(mm)
	case "wheel":
		if b != uv.MouseWheelUp && b != uv.MouseWheelDown {
			writeErr(w, http.StatusBadRequest, errors.New("a wheel event needs wheelup or wheeldown"))
			return
		}
		ev = uv.MouseWheelEvent(mm)
	default:
		writeErr(w, http.StatusBadRequest, errors.New("bad mouse event"))
		return
	}
	sess, ok := sessionByID(w, q.ID)
	if !ok {
		return
	}
	in := sess.Input()
	// Clamped into the program's grid (the TUI's): a drag off the edge stays on it.
	mm.X, mm.Y = clampCell(mm.X, in.Cols), clampCell(mm.Y, in.Rows)
	ev = rebuildMouse(q.Kind, mm)
	switch {
	case in.Mouse:
		sess.SendMouse(ev)
		writeJSON(w, map[string]string{"sent": "mouse"})
	case q.Kind == "wheel" && in.AltScreen:
		code := uv.KeyUp
		if b == uv.MouseWheelDown {
			code = uv.KeyDown
		}
		sess.ScrollKey(uv.KeyPressEvent{Code: code})
		writeJSON(w, map[string]string{"sent": "keys"})
	default:
		writeJSON(w, map[string]string{"sent": "none"})
	}
}

// clampCell keeps a 0-based cell coordinate inside n cells.
func clampCell(v, n int) int {
	if n > 0 && v > n-1 {
		return n - 1
	}
	return v
}

func rebuildMouse(kind string, mm uv.Mouse) domain.SessionMouse {
	switch kind {
	case "release":
		return uv.MouseReleaseEvent(mm)
	case "motion":
		return uv.MouseMotionEvent(mm)
	case "wheel":
		return uv.MouseWheelEvent(mm)
	}
	return uv.MouseClickEvent(mm)
}
