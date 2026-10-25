package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
)

// Solo mode narrows the commit list to one ref's history, the TUI's solo;
// the scope LIST generalises it to the TUI's commit view of several refs
// (Add / Remove from commit view), solo being a list of one.
//
// It is SERVER state, not per-tab: there is a single CommitFeed behind
// /api/commits, so a second tab necessarily sees the same scope. Rather than
// pretend otherwise, every response that depends on the scope reports it, and
// the client renders an exit affordance from that — a mode you can enter and
// not see is a trap.
//
// The scope is stored as a list of refs and turned into a LogScope only when
// a feed is built (feedFor). That is what makes it survive the feed being dropped after a
// state-changing op: resetFeed nils the feed, the next request rebuilds it, and
// the rebuild re-reads this field. Nothing has to remember to re-apply anything.
//
// An entry is either a BRANCH (or tag) name or a COMMIT — "the history reachable
// from here", the TUI's "Solo from this commit". Both are just revs to git log,
// so the walk is identical; the kind matters for what may be entered (a branch
// is checked against the ref lists, a commit is resolved) and for how the client
// labels the chip (a 40-hex sha is unreadable at full length).

// soloCommitTag prefixes a commit-anchored entry in the stored list. git
// forbids control characters in a refname, so no branch or tag can collide
// with it — and one stored string per entry keeps ONE source of truth for the
// scope, which is the field the feed build already re-reads.
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

// scopeEntry is one member of the scope on the wire.
type scopeEntry struct {
	Kind string `json:"kind"` // "branch" (a branch or a tag) / "commit"
	Ref  string `json:"ref"`
}

// soloRef reports the scope as a SOLO: the (kind, ref) pair of a list of one,
// and "", "" for every branch AND for a list of several — the solo rows and
// the chip's "solo:" form speak only of a single ref.
func (s *Server) soloRef() (kind, ref string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.scope) != 1 {
		return "", ""
	}
	return decodeSolo(s.scope[0])
}

// scopeEntries is the whole list as the wire speaks it (never nil, so the
// JSON is [] rather than null).
func (s *Server) scopeEntries() []scopeEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]scopeEntry, 0, len(s.scope))
	for _, st := range s.scope {
		k, r := decodeSolo(st)
		out = append(out, scopeEntry{Kind: k, Ref: r})
	}
	return out
}

// storedScope is a copy of the list, for a feed build.
func (s *Server) storedScope() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.scope...)
}

// setScope records the list and drops the feed so the next /api/commits
// rebuilds under it. A fresh slice: callers build theirs from a copy.
func (s *Server) setScope(scope []string) {
	s.mu.Lock()
	s.scope = scope
	s.feed = nil
	s.mu.Unlock()
}

// editScope applies one add/remove to the list under the lock, so two tabs
// adding at once cannot lose a member. It reports whether the list changed.
func (s *Server) editScope(op, stored string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := slices.Index(s.scope, stored)
	switch {
	case op == "add" && i < 0:
		s.scope = append(slices.Clone(s.scope), stored)
	case op == "remove" && i >= 0:
		s.scope = slices.Delete(slices.Clone(s.scope), i, i+1)
	default:
		return false
	}
	s.feed = nil
	return true
}

// scopeBranches maps the stored list to the feed's ref selection. Whether an
// entry is a branch name or a commit id, Branches alone is ref SELECTION and
// not a content filter, so the commit graph still renders.
func scopeBranches(stored []string) []string {
	out := make([]string, 0, len(stored))
	for _, st := range stored {
		if _, ref := decodeSolo(st); ref != "" {
			out = append(out, ref)
		}
	}
	return out
}

// soloRequest is the wire shape. The historical form is a bare {branch}; the
// commit-anchored form names its kind: {"kind":"commit","ref":"<sha>"}. Both
// SET the scope to that one entry (or clear it), and {branch} keeps behaving
// exactly as it did. op "add" / "remove" edits the list instead — the TUI's
// Add / Remove from commit view.
type soloRequest struct {
	Branch string `json:"branch"` // "" = show every branch again
	Kind   string `json:"kind"`   // "" / "branch" / "commit"
	Ref    string `json:"ref"`
	Op     string `json:"op"` // "" = set / "add" / "remove"
}

// handleSolo sets, clears or edits the commit-list scope.
//
// The ref is RESOLVED, not merely sanitized. That is not about argv safety
// (isGitArgSafe already covers that) but about never entering a scope that
// cannot render: a nonexistent ref would make every subsequent /api/commits
// fail, and the exit affordance the client draws from that response would go
// with it. A branch is resolved against the server's own branch AND tag lists
// (a tag is just a ref to git log — the same machinery the TUI's solo-this-tag
// uses); a commit is resolved by git and stored as its FULL hash, so the walk
// can never hit a short-sha ambiguity.
//
// An add takes a local BRANCH only (the TUI's row is a Branches row). A
// remove looks nothing up: membership is the whole question, and a member
// branch deleted since it was added must still be removable.
func (s *Server) handleSolo(w http.ResponseWriter, r *http.Request) {
	var req soloRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad request body: %w", err))
		return
	}
	switch req.Op {
	case "":
	case "add":
		if req.Ref == "" || !isGitArgSafe(req.Ref) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid branch"))
			return
		}
		known, err := s.knownBranch(r, req.Ref)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if !known {
			writeErr(w, http.StatusNotFound, errors.New("unknown branch"))
			return
		}
		s.editScope("add", encodeSolo("branch", req.Ref))
		s.writeScope(w)
		return
	case "remove":
		kind := req.Kind
		if kind == "" {
			kind = "branch"
		}
		if req.Ref == "" || (kind != "branch" && kind != "commit") {
			writeErr(w, http.StatusBadRequest, errors.New("invalid scope entry"))
			return
		}
		s.editScope("remove", encodeSolo(kind, req.Ref))
		s.writeScope(w)
		return
	default:
		writeErr(w, http.StatusBadRequest, fmt.Errorf("unknown solo op %q", req.Op))
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
	if ref == "" {
		s.setScope(nil)
	} else {
		s.setScope([]string{encodeSolo(kind, ref)})
	}
	s.writeScope(w)
}

// writeScope answers with the scope now stored: the solo fields (a list of
// one) and the whole list.
func (s *Server) writeScope(w http.ResponseWriter) {
	kind, ref := s.soloRef()
	writeJSON(w, map[string]any{"solo": ref, "solo_kind": kind, "scope": s.scopeEntries()})
}

// knownBranch reports whether name is one of this repo's local branches.
func (s *Server) knownBranch(r *http.Request, name string) (bool, error) {
	branches, err := s.service().Branches(r.Context())
	if err != nil {
		return false, err
	}
	for _, b := range branches {
		if b.Name == name {
			return true, nil
		}
	}
	return false, nil
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
