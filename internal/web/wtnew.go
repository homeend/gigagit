package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

// The TUI's create-worktree popup (internal/tui/worktree_popup.go) behind
// every web "new worktree" entrance: /api/worktree/preview is its recompute,
// create-worktree its startCreateFromPopup.

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("POST /api/worktree/preview", writeGuard(s.handleWorktreePreview))
	})
}

// wtPreviewFroms are the dialog's entrances; "branch" is the TUI's existing
// mode (the selection is checked out, the branch template does not apply).
var wtPreviewFroms = map[string]bool{"branch": true, "commit": true, "tag": true, "remote": true}

func (s *Server) handleWorktreePreview(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var req struct {
		From   string            `json:"from"`
		Start  string            `json:"start"`
		Branch string            `json:"branch"`
		Path   string            `json:"path"`
		Inputs map[string]string `json:"inputs"`
		Seed   string            `json:"seed"`
		Now    string            `json:"now"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if !wtPreviewFroms[req.From] {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("unknown entrance %q", req.From))
		return
	}
	// <random> and <date> are fixed for the dialog's life: the first call
	// picks them, the page echoes them back (a uint64 does not fit a JS
	// number, so the seed travels as a decimal string).
	seed, err := strconv.ParseUint(req.Seed, 10, 64)
	if req.Seed == "" || err != nil {
		seed = rand.Uint64()
	}
	now, err := time.Parse(time.RFC3339Nano, req.Now)
	if req.Now == "" || err != nil {
		now = time.Now()
	}
	svc := s.service()
	cfg, err := s.effectiveConfig(r.Context(), svc)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	p := svc.PreviewNewWorktree(r.Context(), domain.NewWorktreeSpec{
		Start:          req.Start,
		Existing:       req.From == "branch",
		BranchTemplate: cfg.Worktree.DefaultBranchTemplate,
		PathTemplate:   cfg.Worktree.PathTemplate,
		FixedBranch:    req.Branch,
		FixedPath:      req.Path,
		Inputs:         req.Inputs,
		Seed:           seed,
		Now:            now,
	})
	out := map[string]any{
		"labels":   nonNilStrings(p.Labels),
		"branch":   p.Branch,
		"path":     p.Path,
		"seed":     strconv.FormatUint(seed, 10),
		"now":      now.Format(time.RFC3339Nano),
		"hook":     cfg.Worktree.PostCreateHook != "",
		"existing": req.From == "branch",
	}
	if p.Err != nil {
		out["error"] = p.Err.Error()
	}
	writeJSON(w, out)
}

func nonNilStrings(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// startCreateWorktree answers create-worktree. The start is a COMMIT (hex
// only), a TAG or a REMOTE branch (each checked against a fresh read), or a
// local BRANCH: the same name checks it out, another name cuts a NEW branch
// from it (the TUI popup's e). Counters bump only after a successful create
// and are derived here — the wire says only THAT the path template was used
// and WHICH prefix was picked.
func (s *Server) startCreateWorktree(w http.ResponseWriter, r *http.Request, req opStartRequest) {
	op, code, err := s.buildCreateWorktree(r, req)
	if err != nil {
		writeErr(w, code, err)
		return
	}
	var seqNames []string
	if req.PathSeq {
		if cfg, cerr := s.effectiveConfig(r.Context(), s.service()); cerr == nil {
			seqNames = domain.WorktreeSeqNames(cfg.Worktree.PathTemplate)
		}
	}
	if req.PrefixID != "" {
		scope, ok := parseProfileScope(req.PrefixScope)
		if !ok {
			writeErr(w, http.StatusBadRequest, errors.New("prefix_scope must be global or repo"))
			return
		}
		p, ok := s.prefixByID(r, req.PrefixID, scope)
		if !ok {
			writeErr(w, http.StatusNotFound, errors.New("unknown prefix"))
			return
		}
		seqNames = append(seqNames, domain.PrefixSeqNames(p.Value)...)
	}
	run, err := s.startRun("op", func(ctx context.Context, svc *domain.Service, events chan<- engine.Event, dec engine.Decider) (engine.Result, map[string]any, error) {
		res, err := svc.Execute(ctx, op, events, dec)
		if err == nil && res.Changed && len(seqNames) > 0 {
			_ = svc.BumpPrefixSeqs(ctx, seqNames)
		}
		return res, nil, err
	})
	if err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{"op_id": run.id})
}

func (s *Server) buildCreateWorktree(r *http.Request, req opStartRequest) (engine.Operation, int, error) {
	if req.Path == "" || !isGitArgSafe(req.Path) {
		return nil, http.StatusBadRequest, errors.New("invalid path")
	}
	hook := ""
	if !req.NoHook {
		// Honoured rather than silently skipped, as in the TUI; the engine's
		// approval decision shows the script and parks in the browser modal.
		hook = s.postCreateHook(r)
	}
	keep := engine.KeepNone
	if req.Mode != "" {
		k, ok := worktreeKeepModes[req.Mode]
		if !ok || req.Sha == "" {
			return nil, http.StatusBadRequest, fmt.Errorf("unknown keep mode %q", req.Mode)
		}
		keep = k
	}
	var start string
	switch {
	case req.Sha != "":
		if !isHexSha(req.Sha) {
			return nil, http.StatusBadRequest, errors.New("invalid commit")
		}
		start = req.Sha
	case req.Tag != "":
		tags, err := s.service().Tags(r.Context())
		if err != nil {
			return nil, http.StatusInternalServerError, err
		}
		found := false
		for _, tg := range tags {
			if tg.Name == req.Tag {
				found = true
				break
			}
		}
		if !found {
			return nil, http.StatusBadRequest, errors.New("no such tag")
		}
		start = "refs/tags/" + req.Tag
	case req.Ref != "":
		rbs, err := s.service().RemoteBranches(r.Context())
		if err != nil {
			return nil, http.StatusInternalServerError, err
		}
		found := false
		for _, rb := range rbs {
			if rb.Name == req.Ref {
				found = true
				break
			}
		}
		if !found {
			return nil, http.StatusBadRequest, errors.New("no such remote branch")
		}
		start = "refs/remotes/" + req.Ref
	default:
		// A local branch: the engine refuses one that is not local, and git
		// one already checked out elsewhere.
		if req.Branch == "" || !isGitArgSafe(req.Branch) {
			return nil, http.StatusBadRequest, errors.New("invalid branch")
		}
		if req.Name == "" || req.Name == req.Branch {
			return engine.CreateWorktreeForBranch{Branch: req.Branch, Path: req.Path, PostCreateHook: hook}, 0, nil
		}
		start = req.Branch
	}
	if req.Name == "" || !isGitArgSafe(req.Name) {
		return nil, http.StatusBadRequest, errors.New("invalid branch name")
	}
	return engine.CreateWorktree{StartPoint: start, Branch: req.Name, Path: req.Path, PostCreateHook: hook, Keep: keep}, 0, nil
}
