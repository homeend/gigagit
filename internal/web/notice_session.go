package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"sync"
)

// The notification centre's SESSION — the TUI's noticeSessionDismissed and
// noticesUnread (internal/tui/notify.go, notice_popup.go) for a server that
// many tabs share. It lives in memory, per process and per repository (the
// git common dir, the prompt store's key), never in browser storage: gg web
// binds a random port, so a tab's storage starts empty every run, and a page
// reload is the same session.
//
//   - Every action on a notice — a fix, "Not now", "Never" — hides it for the
//     session, so a re-read cannot resurrect it; only a copy-like action
//     (keep) leaves it standing. "Never" is ALSO persisted (prompts.toml).
//   - Unread follows armBlinkForNew: a read of the centre that carries an id
//     the previous read did not marks it unread, until the centre is opened.

// noticeStaleLock is the TUI's stale-lock notice id. PERMANENT.
const noticeStaleLock = "stale_git_lock"

// Labels of the two dismissal rows, the TUI's words.
const (
	labelNotNow = "Not now (ask again next load)"
	labelNever  = "Never for this repo"
)

// notNowNever is the pair of dismissal rows every persisted-dismissable
// notice ends with.
func notNowNever(id string) []noticeAction {
	return []noticeAction{{Label: labelNotNow, Session: true}, {Label: labelNever, Dismiss: id}}
}

// maxSessionIDs bounds one repo's session set (a client bug cannot grow it
// without limit; the centre never holds more than a handful).
const maxSessionIDs = 256

// sessionNoticeID is what a session id may look like: the TUI's snake_case
// ids, with their numeric / hashed suffixes.
var sessionNoticeID = regexp.MustCompile(`^[a-z0-9_]{1,128}$`)

type noticeSession struct {
	mu        sync.Mutex
	dismissed map[string]map[string]bool // repo key → notice id
	prev      map[string]map[string]bool // repo key → the ids the last read carried
	unread    map[string]bool
}

func (n *noticeSession) dismiss(key, id string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.dismissed == nil {
		n.dismissed = map[string]map[string]bool{}
	}
	set := n.dismissed[key]
	if set == nil {
		set = map[string]bool{}
		n.dismissed[key] = set
	}
	if len(set) < maxSessionIDs {
		set[id] = true
	}
}

// undismiss brings a notice back for the session (a lock failure is new
// information, not the same advice resurfacing).
func (n *noticeSession) undismiss(key, id string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.dismissed[key], id)
}

func (n *noticeSession) isDismissed(key, id string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.dismissed[key][id]
}

// observe records the ids a read is answering with and reports whether the
// centre is unread: true from the first read carrying a new id until read.
func (n *noticeSession) observe(key string, ids []string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.prev == nil {
		n.prev, n.unread = map[string]map[string]bool{}, map[string]bool{}
	}
	now := make(map[string]bool, len(ids))
	for _, id := range ids {
		now[id] = true
		if !n.prev[key][id] {
			n.unread[key] = true
		}
	}
	n.prev[key] = now
	if len(ids) == 0 {
		n.unread[key] = false // nothing left to read
	}
	return n.unread[key]
}

func (n *noticeSession) read(key string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.unread != nil {
		n.unread[key] = false
	}
}

// repoKey is the session key of the served repository.
func (s *Server) repoKey(r *http.Request) (string, error) {
	key, err := s.service().GitCommonDir(readCtx(r))
	if err != nil || key == "" {
		return "", errors.New("cannot resolve repo key")
	}
	rememberRepoKey(s.service(), key)
	return key, nil
}

// handleNoticeSession hides one notice for this session (POST {id}).
func (s *Server) handleNoticeSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil || !sessionNoticeID.MatchString(req.ID) {
		writeErr(w, http.StatusBadRequest, errors.New("bad notice id"))
		return
	}
	key, err := s.repoKey(r)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.notices.dismiss(key, req.ID)
	writeJSON(w, map[string]bool{"ok": true})
}

// handleNoticeRead marks the centre read: it was opened.
func (s *Server) handleNoticeRead(w http.ResponseWriter, r *http.Request) {
	key, err := s.repoKey(r)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.notices.read(key)
	writeJSON(w, map[string]bool{"ok": true})
}
