package web

// The per-console screen stream (web attach, plan 1): GET /api/session-screen
// is one SSE stream per open console. One producer per session snapshots
// its styled runs on every change (coalesced) and hands them to every
// attached stream; each stream writes only the rows that changed since the
// frame IT last wrote, and a whole frame after a size change, a fresh
// attach or a skipped delivery — so a slow tab never lags or shows garbage.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/homeend/gigagit/internal/domain"
)

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/session-screen", s.handleSessionScreen)
	})
}

// feedCoalesce bounds how often a session's screen is snapshotted.
const feedCoalesce = 40 * time.Millisecond

// feedGonePoll is how often a producer checks that its session is still
// listed. (The manager's Changed() is one coalesced channel with one web
// receiver, watchSessions; a second receiver would steal its signals.)
const feedGonePoll = time.Second

// --- wire ---------------------------------------------------------------------

type frameLine struct {
	Y    int                `json:"y"`
	Runs []domain.ScreenRun `json:"runs"`
}

// frameWire is one screen update. Full carries every row; otherwise only
// the rows that changed since the previous frame THIS stream wrote. A frame
// with no rows still carries the cursor, which moves without a cell changing.
type frameWire struct {
	Full   bool        `json:"full"`
	Cols   int         `json:"cols"`
	Rows   int         `json:"rows"`
	CX     int         `json:"cx"`
	CY     int         `json:"cy"`
	Cursor bool        `json:"cursor"`
	Alt    bool        `json:"alt"`
	Lines  []frameLine `json:"lines"`
}

// diffFrame builds the frame that turns prev into next: full when there is
// no prev or the size changed, else the changed rows only.
func diffFrame(prev *domain.ScreenRuns, next domain.ScreenRuns) frameWire {
	f := frameWire{Cols: next.Cols, Rows: next.Rows, CX: next.CursorX, CY: next.CursorY, Cursor: next.CursorVisible, Alt: next.AltScreen, Lines: []frameLine{}}
	f.Full = prev == nil || prev.Cols != next.Cols || prev.Rows != next.Rows || len(prev.Lines) != len(next.Lines)
	for y, row := range next.Lines {
		if !f.Full && slices.Equal(prev.Lines[y].Runs, row.Runs) {
			continue
		}
		f.Lines = append(f.Lines, frameLine{Y: y, Runs: nonNilRuns(row.Runs)})
	}
	return f
}

func nonNilRuns(r []domain.ScreenRun) []domain.ScreenRun {
	if r == nil {
		return []domain.ScreenRun{}
	}
	return r
}

// palette16 is the console's basic-colour table, sent once in hello so the
// page can theme the "#rrggbb" values it recognises.
func palette16() []string {
	out := make([]string, 16)
	for i := range out {
		r, g, b, _ := ansi.BasicColor(i).RGBA()
		out[i] = fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
	}
	return out
}

// --- feeds --------------------------------------------------------------------

// feedMsg is one delivery to a stream: a screen (Full when the stream missed
// one), an exit, or the session's removal.
type feedMsg struct {
	Screen *domain.ScreenRuns
	Full   bool
	Exited *int
	Gone   bool
}

type feedSub struct {
	ch     chan feedMsg
	missed bool // a publish found the buffer full: the next screen is marked Full
}

// screenFeeds fans one session's snapshots out to its attached streams. One
// producer goroutine per session runs while it has subscribers.
type screenFeeds struct {
	mu   sync.Mutex
	subs map[domain.SessionID]map[*feedSub]struct{}
	run  map[domain.SessionID]chan struct{} // producer stop channels
}

func newScreenFeeds() *screenFeeds {
	return &screenFeeds{subs: map[domain.SessionID]map[*feedSub]struct{}{}, run: map[domain.SessionID]chan struct{}{}}
}

// subscribe registers a stream for id (no producer: tests drive publish).
// The last cancel for a session stops its producer.
func (f *screenFeeds) subscribe(id domain.SessionID) (<-chan feedMsg, func()) {
	sub := &feedSub{ch: make(chan feedMsg, 1)}
	f.mu.Lock()
	if f.subs[id] == nil {
		f.subs[id] = map[*feedSub]struct{}{}
	}
	f.subs[id][sub] = struct{}{}
	f.mu.Unlock()
	return sub.ch, func() {
		f.mu.Lock()
		delete(f.subs[id], sub)
		if len(f.subs[id]) == 0 {
			delete(f.subs, id)
			if stop := f.run[id]; stop != nil {
				close(stop)
				delete(f.run, id)
			}
		}
		f.mu.Unlock()
	}
}

// publish hands sr to every subscriber of id; one that still holds an
// unread screen is skipped and gets its next screen marked Full.
func (f *screenFeeds) publish(id domain.SessionID, sr domain.ScreenRuns) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for sub := range f.subs[id] {
		select {
		case sub.ch <- feedMsg{Screen: &sr, Full: sub.missed}:
			sub.missed = false
		default:
			sub.missed = true
		}
	}
}

// send delivers a control message (exit, gone), waiting briefly so it is
// never lost behind an unread screen.
func (f *screenFeeds) send(id domain.SessionID, msg feedMsg) {
	f.mu.Lock()
	subs := make([]*feedSub, 0, len(f.subs[id]))
	for sub := range f.subs[id] {
		subs = append(subs, sub)
	}
	f.mu.Unlock()
	for _, sub := range subs {
		select {
		case sub.ch <- msg:
		case <-time.After(time.Second):
		}
	}
}

// attach subscribes and makes sure a producer runs for the session.
func (f *screenFeeds) attach(sess *domain.AgentSession) (<-chan feedMsg, func()) {
	id := sess.Info().ID
	ch, cancel := f.subscribe(id)
	f.mu.Lock()
	if f.run[id] == nil {
		stop := make(chan struct{})
		f.run[id] = stop
		go f.produce(sess, stop)
	}
	f.mu.Unlock()
	return ch, cancel
}

// produce snapshots the session on every change, coalesced, until stop; it
// reports the exit once and the removal (the manager no longer lists it).
func (f *screenFeeds) produce(sess *domain.AgentSession, stop <-chan struct{}) {
	id := sess.Info().ID
	var timer *time.Timer
	var fire <-chan time.Time
	exited := false
	gone := time.NewTicker(feedGonePoll)
	defer gone.Stop()
	for {
		select {
		case <-stop:
			return
		case <-sess.Changed():
			if timer == nil {
				timer = time.NewTimer(feedCoalesce)
				fire = timer.C
			}
		case <-fire:
			timer, fire = nil, nil
			f.publish(id, sess.ScreenRuns())
			if info := sess.Info(); info.State == domain.SessionExited && !exited {
				exited = true
				code := info.ExitCode
				f.send(id, feedMsg{Exited: &code})
			}
		case <-gone.C:
			if _, ok := domain.Sessions().Get(id); !ok {
				f.send(id, feedMsg{Gone: true})
				return
			}
		}
	}
}

// --- GET /api/session-screen --------------------------------------------------

func (s *Server) handleSessionScreen(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, errors.New("streaming unsupported"))
		return
	}
	sess, ok := domain.Sessions().Get(domain.SessionID(r.URL.Query().Get("id")))
	if !ok {
		writeErr(w, http.StatusNotFound, errors.New("no such session"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ch, cancel := s.feeds.attach(sess)
	defer cancel()
	last := sess.ScreenRuns()
	writeSSEEvent(w, "hello", map[string]any{"frame": diffFrame(nil, last), "palette": palette16()})
	if info := sess.Info(); info.State == domain.SessionExited {
		writeSSEEvent(w, "exited", map[string]int{"code": info.ExitCode})
	}
	fl.Flush()
	ping := time.NewTicker(liveKeepalive)
	defer ping.Stop()
	for {
		select {
		case m := <-ch:
			switch {
			case m.Gone:
				writeSSEEvent(w, "gone", map[string]any{})
				fl.Flush()
				return
			case m.Exited != nil:
				writeSSEEvent(w, "exited", map[string]int{"code": *m.Exited})
			case m.Screen != nil:
				prev := &last
				if m.Full {
					prev = nil
				}
				writeSSEEvent(w, "frame", diffFrame(prev, *m.Screen))
				last = *m.Screen
			}
			fl.Flush()
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		case <-s.closing:
			return
		case <-r.Context().Done():
			return
		}
	}
}

func writeSSEEvent(w http.ResponseWriter, name string, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, b)
}
