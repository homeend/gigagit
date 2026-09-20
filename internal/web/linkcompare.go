package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// A link comparison: two gg:// link TEXTS through domain.CompareLinks — the
// one door (spec W2). Nothing in this file evaluates a link itself: a parsed
// LOCAL-form link holds its checkout and its file undivided, and only the
// door's locate splits them (a file link evaluated without it silently reads
// as the whole tree).
//
// Link text is deliberately NOT run through isGitArgSafe: it holds `://`, `@`
// and `?` by grammar. The door parses it; nothing reaches argv unparsed.

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/compare-links", s.handleCompareLinks)
	})
}

// linkOpts is what every link call in this package passes: this checkout
// first, then the repo-switcher registry — which is what makes "another
// checkout" reportable as such rather than as "unknown".
func (s *Server) linkOpts(svc *domain.Service) domain.ResolveOpts {
	return domain.ResolveOpts{Cwd: svc, RegistryPath: s.reposStatePath()}
}

// linkSideSpec spells a RESOLVED byte source in /api/entry-diff's vocabulary.
// Per KIND with no default arm that answers (commitEntrySide's rule): a kind
// this wire cannot spell is a server bug, and must not reach the browser as a
// `commit:` with an empty hash.
func linkSideSpec(ep model.Endpoint) (string, error) {
	switch ep.Kind() {
	case model.EndpointWorkTree:
		return "worktree", nil
	case model.EndpointIndex:
		return "staged", nil
	case model.EndpointShelf:
		return "shelf:" + ep.ShelfID(), nil
	case model.EndpointCommit:
		return "commit:" + ep.Hash(), nil
	case model.EndpointPair:
		// A change-set's members are read as they are at b (Endpoint.FileRef).
		return "commit:" + ep.PairB(), nil
	case model.EndpointRef, model.EndpointInvalid:
		// %d, not Display(): Display panics on the zero endpoint.
		return "", fmt.Errorf("link side: endpoint kind %d has no diff spec", ep.Kind())
	default:
		return "", fmt.Errorf("link side: unknown endpoint kind %d", ep.Kind())
	}
}

// compareLinksStatus mirrors cli.compareLinkExit: grammar is the caller's
// mistake (400); any other failure that belongs to ONE side is a link this
// checkout cannot answer (422); a bare failure is the comparison's own (500).
// The message is the CAUSE's: the side rides its own field.
func compareLinksStatus(err error) (code int, side, msg string) {
	var se *domain.LinkSideError
	if !errors.As(err, &se) {
		return http.StatusInternalServerError, "", err.Error()
	}
	if errors.Is(se.Err, model.ErrLink) {
		return http.StatusBadRequest, se.Side.String(), se.Err.Error()
	}
	return http.StatusUnprocessableEntity, se.Side.String(), se.Err.Error()
}

// leftPathOf is the path a row's LEFT side is read at: its old path when the
// row is a rename, else the path itself.
func leftPathOf(oldPath, path string) string {
	if oldPath != "" {
		return oldPath
	}
	return path
}

type linkSideWire struct {
	Text string `json:"text"`
	Desc string `json:"desc"`
	Spec string `json:"spec"`
}

type linkFileWire struct {
	Path    string `json:"path"`
	Status  string `json:"status"`
	OldPath string `json:"old_path,omitempty"`
	// LeftSpec/RightSpec are set only where this member's bytes live somewhere
	// other than its side's own endpoint (a `-u` stash's untracked file).
	LeftSpec  string `json:"left_spec,omitempty"`
	RightSpec string `json:"right_spec,omitempty"`
}

// pairSides spells a commit pair as the two links the door compares: the
// point a against the change-set a..b — what the TUI's pair landing compares.
// Built as Links and rendered by String, never assembled from text.
func pairSides(repo model.LinkRepo, a, b string) (left, right string) {
	left = model.Link{Repo: repo, Target: model.LinkTarget{State: model.StateCommitted, Commit: a}}.String()
	right = model.Link{Repo: repo, Target: model.LinkTarget{State: model.StateCommitted, Pair: &model.LinkPair{A: a, B: b}}}.String()
	return left, right
}

// handleCompareLinks takes exactly ONE of three input forms, so a dialog
// submit, a saved row and a pair landing are one lane:
//
//	left + right   two link texts, as given
//	id             a saved comparison's texts, or a saved pair's two commits
//	a + b          two FULL commit ids (a `@a..b` navigate)
func (s *Server) handleCompareLinks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	left, right, id, a, b := q.Get("left"), q.Get("right"), q.Get("id"), q.Get("a"), q.Get("b")
	forms := 0
	for _, given := range []bool{left != "" || right != "", id != "", a != "" || b != ""} {
		if given {
			forms++
		}
	}
	if forms != 1 {
		writeErr(w, http.StatusBadRequest, errors.New("give exactly one of: left + right, id, a + b"))
		return
	}
	svc := s.service()
	label := ""
	switch {
	case id != "":
		kind, p, c, err := savedEntry(r, svc, id)
		if err != nil {
			writeErr(w, savedCompareErrStatus(err, http.StatusInternalServerError), err)
			return
		}
		label, left, right = c.Label, c.Left, c.Right
		if kind == "pair" {
			label, a, b = p.Label, p.A, p.B
		}
	case a != "" || b != "":
		if !isFullSha(a) || !isFullSha(b) {
			writeErr(w, http.StatusBadRequest, errors.New("a and b must be full commit ids"))
			return
		}
	default:
		if left == "" || right == "" {
			writeErr(w, http.StatusBadRequest, errors.New("a comparison needs both links"))
			return
		}
	}
	// Only a PAIR landing names its pair (the note scope the page arms): the
	// two-link form never does, even when its links spell one — a comparison
	// of two far-apart points must not pay the pair's rev-list.
	var pair *[2]string
	if a != "" {
		pair = &[2]string{a, b}
		repo, err := svc.LinkRepo(r.Context())
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		left, right = pairSides(repo, a, b)
	}
	s.writeLinkComparison(w, r, left, right, label, pair)
}

// writeLinkComparison runs the door and answers the comparison's wire shape.
// pair, when set, is the commit pair this comparison IS — two full ids.
func (s *Server) writeLinkComparison(w http.ResponseWriter, r *http.Request, left, right, label string, pair *[2]string) {
	svc := s.service()
	ctx := r.Context()
	c, err := svc.CompareLinks(ctx, left, right, s.linkOpts(svc))
	if err != nil {
		code, side, msg := compareLinksStatus(err)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": msg, "side": side})
		return
	}
	sideWire := func(text string, fs domain.FileSet) (linkSideWire, error) {
		spec, err := linkSideSpec(fs.Endpoint())
		desc := text
		if l, perr := model.ParseLink(text); perr == nil {
			desc = svc.DescribeLink(ctx, l)
		}
		return linkSideWire{Text: text, Desc: desc, Spec: spec}, err
	}
	lw, err := sideWire(c.LeftText, c.Left)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	rw, err := sideWire(c.RightText, c.Right)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	files := make([]linkFileWire, len(c.Files))
	for i, f := range c.Files {
		oldP := leftPathOf(f.OldPath, f.Path)
		// The web's compareSides: a member's bytes come from Source(path).
		ls, lerr := linkSideSpec(c.Left.Source(oldP))
		rs, rerr := linkSideSpec(c.Right.Source(f.Path))
		if err := errors.Join(lerr, rerr); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		row := linkFileWire{Path: f.Path, Status: f.Status, OldPath: f.OldPath}
		if ls != lw.Spec {
			row.LeftSpec = ls
		}
		if rs != rw.Spec {
			row.RightSpec = rs
		}
		files[i] = row
	}
	out := map[string]any{"left": lw, "right": rw, "files": files}
	if label != "" {
		out["label"] = label
	}
	if pair != nil {
		out["pair"] = map[string]string{"a": pair[0], "b": pair[1]}
	}
	writeJSON(w, out)
}
