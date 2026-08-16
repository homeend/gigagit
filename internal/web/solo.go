package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
)

// Solo mode narrows the commit list to one ref's history, the TUI's solo.
//
// It is SERVER state, not per-tab: there is a single CommitFeed behind
// /api/commits, so a second tab necessarily sees the same scope. Rather than
// pretend otherwise, every response that depends on the scope reports it, and
// the client renders an exit affordance from that — a mode you can enter and
// not see is a trap.
//
// The scope is stored as a ref and turned into a LogScope only when a feed is
// built (feedFor). That is what makes it survive the feed being dropped after a
// state-changing op: resetFeed nils the feed, the next request rebuilds it, and
// the rebuild re-reads this field. Nothing has to remember to re-apply anything.
//
// A scope is either a BRANCH (or tag) name or a COMMIT — "the history reachable
// from here", the TUI's "Solo from this commit". Both are just revs to git log,
// so the walk is identical; the kind matters for what may be entered (a branch
// is checked against the ref lists, a commit is resolved) and for how the client
// labels the chip (a 40-hex sha is unreadable at full length).

// soloCommitTag prefixes a commit-anchored scope in the single stored string.
// git forbids control characters in a refname, so no branch or tag can collide
// with it — and one stored value keeps ONE source of truth for the scope, which
// is the field the feed build already re-reads.
const soloCommitTag = "commit\x00"

// encodeSolo / decodeSolo convert between the stored string and the (kind, ref)
// pair the wire speaks. An empty ref is "no scope" in either direction.
func encodeSolo(kind, ref string) string {
	if ref == "" {
		return ""
	}
	if kind == "commit" {
		return soloCommitTag + ref
	}
	return ref
}

func decodeSolo(stored string) (kind, ref string) {
	if stored == "" {
		return "", ""
	}
	if r, ok := strings.CutPrefix(stored, soloCommitTag); ok {
		return "commit", r
	}
	return "branch", stored
}

// soloRef reports the scope the commit list is currently narrowed to as the
// (kind, ref) pair the wire speaks ("", "" = the whole repo).
func (s *Server) soloRef() (kind, ref string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return decodeSolo(s.solo)
}

// setSolo records the scope and drops the feed so the next /api/commits
// rebuilds under it.
func (s *Server) setSolo(stored string) {
	s.mu.Lock()
	s.solo = stored
	s.feed = nil
	s.mu.Unlock()
}

// soloScope maps the stored scope to the feed's refspec. Whether the ref is a
// branch name or a commit id, Branches alone is ref SELECTION and not a content
// filter, so the commit graph still renders.
func soloScope(stored string) domain.LogScope {
	_, ref := decodeSolo(stored)
	if ref == "" {
		return domain.LogScope{}
	}
	return domain.LogScope{Branches: []string{ref}}
}

// soloRequest is the wire shape. The historical form is a bare {branch}; the
// commit-anchored form names its kind: {"kind":"commit","ref":"<sha>"}. Both
// are accepted, and {branch} keeps behaving exactly as it did.
type soloRequest struct {
	Branch string `json:"branch"` // "" = show every branch again
	Kind   string `json:"kind"`   // "" / "branch" / "commit"
	Ref    string `json:"ref"`
}

// handleSolo sets or clears the commit-list scope.
//
// The ref is RESOLVED, not merely sanitized. That is not about argv safety
// (isGitArgSafe already covers that) but about never entering a scope that
// cannot render: a nonexistent ref would make every subsequent /api/commits
// fail, and the exit affordance the client draws from that response would go
// with it. A branch is resolved against the server's own branch AND tag lists
// (a tag is just a ref to git log — the same machinery the TUI's solo-this-tag
// uses); a commit is resolved by git and stored as its FULL hash, so the walk
// can never hit a short-sha ambiguity.
func (s *Server) handleSolo(w http.ResponseWriter, r *http.Request) {
	var req soloRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad request body: %w", err))
		return
	}
	kind, ref := req.Kind, req.Ref
	if ref == "" && kind == "" { // the historical {branch} form
		kind, ref = "branch", req.Branch
	}
	switch {
	case ref == "": // clear
		kind = ""
	case kind == "branch":
		if !isGitArgSafe(ref) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid ref"))
			return
		}
		known, err := s.knownRef(r, ref)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if !known {
			writeErr(w, http.StatusNotFound, errors.New("unknown branch or tag"))
			return
		}
	case kind == "commit":
		// Hex-only: a commit id is content-addressed, so unlike a rev
		// expression there is nothing to resolve or mis-resolve — and every
		// value the commits feed hands the browser is already full hex.
		if !isHexSha(ref) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid commit id"))
			return
		}
		full, found, err := s.service().ResolveRev(readCtx(r), ref)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if !found {
			writeErr(w, http.StatusNotFound, errors.New("unknown commit"))
			return
		}
		ref = full
	default:
		writeErr(w, http.StatusBadRequest, fmt.Errorf("unknown solo kind %q", kind))
		return
	}
	s.setSolo(encodeSolo(kind, ref))
	writeJSON(w, map[string]any{"solo": ref, "solo_kind": kind})
}

// knownRef reports whether name is one of this repo's local branches or tags.
func (s *Server) knownRef(r *http.Request, name string) (bool, error) {
	svc := s.service()
	branches, err := svc.Branches(r.Context())
	if err != nil {
		return false, err
	}
	for _, b := range branches {
		if b.Name == name {
			return true, nil
		}
	}
	tags, err := svc.Tags(r.Context())
	if err != nil {
		return false, err
	}
	for _, tg := range tags {
		if tg.Name == name {
			return true, nil
		}
	}
	return false, nil
}
