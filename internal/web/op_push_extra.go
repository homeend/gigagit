package web

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

// Tags on the branch tip, at push time.
//
// The TUI's P asks one question before pushing: the tip commit carries tags the
// remote does not have — push those too? The browser never asked, so a tag
// created in the web UI stayed local forever with nothing on screen saying so.
//
// The check is a NETWORK call (`git ls-remote --tags`), which is why the TUI
// bounds it to five seconds and skips the question entirely when the budget
// runs out: a push must never hang behind an unreachable remote. GET
// /api/push-check mirrors that exactly — a timeout answers `checked:false` with
// an empty list, and the client then pushes the ordinary way.

// pushCheckBudget bounds the pre-push remote-tag lookup. Package-level rather
// than a Server field so a test can shorten it (the Server struct belongs to
// server.go, which parallel work must not touch).
var pushCheckBudget = 5 * time.Second

// opPushTipTags is the wire name of the tip-tag push. PERMANENT.
const opPushTipTags = "push-tip-tags"

func init() {
	RegisterOp(opPushTipTags, buildPushTipTags)
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/push-check", s.handlePushCheck)
	})
}

type pushCheckResp struct {
	Branch string `json:"branch"`
	// TipTags are the local tags on the branch tip; Unpushed is the subset the
	// remote does not have. Checked reports whether the remote lookup actually
	// finished — false means the budget ran out (or the remote errored) and the
	// client must NOT offer anything, exactly as the TUI skips its prompt.
	TipTags  []string `json:"tip_tags"`
	Unpushed []string `json:"unpushed"`
	Checked  bool     `json:"checked"`
}

// handlePushCheck reports whether a push of branch (default: the current one)
// should offer to carry tip tags along. It is deliberately a read endpoint the
// client calls BEFORE starting the push, not a fork inside the push: the web
// Decider can only resolve decisions the engine raises, and engine.Push raises
// none for this.
func (s *Server) handlePushCheck(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	branch := r.URL.Query().Get("branch")
	if branch == "" {
		cur, err := svc.CurrentBranch(r.Context())
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		branch = cur
	} else if !isGitArgSafe(branch) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid branch"))
		return
	}
	resp := pushCheckResp{Branch: branch, TipTags: []string{}, Unpushed: []string{}}
	if branch == "" { // detached HEAD: the push itself refuses; nothing to offer
		writeJSON(w, resp)
		return
	}
	names, err := tipTagNames(r.Context(), svc, branch)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	resp.TipTags = names
	if len(names) == 0 {
		// No tags on the tip: answer without touching the network at all, the
		// TUI's "push directly, no network call" fast path.
		resp.Checked = true
		writeJSON(w, resp)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), pushCheckBudget)
	defer cancel()
	remote, err := svc.RemoteTagsFresh(ctx)
	if err != nil {
		writeJSON(w, resp) // Checked stays false → the client pushes without asking
		return
	}
	resp.Checked = true
	for _, n := range names {
		if !remote[n] {
			resp.Unpushed = append(resp.Unpushed, n)
		}
	}
	writeJSON(w, resp)
}

// buildPushTipTags pushes the tags sitting on a branch's tip commit. The names
// are resolved server-side from the branch, never taken from the wire: they
// flow into `git push` argv, and the whole point of the prompt is "these
// specific tags", not an arbitrary list. Tags further back in history are
// deliberately none of this op's business — that is the TUI's rule too.
//
// Re-pushing a tag the remote already has is a no-op for git, so this does not
// repeat the ls-remote check the client already ran; it pushes the tip set.
func buildPushTipTags(s *Server, r *http.Request, req opStartRequest) (engine.Operation, func(), int, error) {
	svc := s.service()
	branch := req.Branch
	if branch == "" {
		cur, err := svc.CurrentBranch(r.Context())
		if err != nil {
			return nil, nil, http.StatusInternalServerError, err
		}
		branch = cur
	} else if !isGitArgSafe(branch) {
		return nil, nil, http.StatusBadRequest, errors.New("invalid branch")
	}
	if branch == "" {
		return nil, nil, http.StatusConflict, errors.New("push tags: no current branch (detached HEAD?)")
	}
	names, err := tipTagNames(r.Context(), svc, branch)
	if err != nil {
		return nil, nil, http.StatusInternalServerError, err
	}
	if len(names) == 0 {
		return nil, nil, http.StatusUnprocessableEntity, errors.New("no tags on the branch tip")
	}
	return engine.PushTags{Remote: "origin", Names: names}, nil, 0, nil
}

// tipTagNames returns the local tags whose target is branch's tip commit.
// model.Tag.Target and model.Branch.Hash are the same abbreviation of the same
// repo's object names, so comparing them directly is correct (the TUI's
// tagsAtCommit). An unknown branch has no tip and therefore no tip tags.
func tipTagNames(ctx context.Context, svc *domain.Service, branch string) ([]string, error) {
	branches, err := svc.Branches(ctx)
	if err != nil {
		return nil, err
	}
	tip := ""
	for _, b := range branches {
		if b.Name == branch {
			tip = b.Hash
			break
		}
	}
	if tip == "" {
		return nil, nil
	}
	tags, err := svc.Tags(ctx)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, t := range tags {
		if t.Target == tip {
			out = append(out, t.Name)
		}
	}
	return out, nil
}
