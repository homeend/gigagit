package web

// The working tree and the index as compare sides (the TUI's ◇ rows and its
// Compare against working tree / staged, Compare against working dir, Copy
// to working dir): GET /api/compare-sides lists the files between two sides
// named by spec — worktree, staged or commit:<hex> — and the page diffs each
// through /api/entry-diff (the same specs); the copy-to-working op writes a
// commit's or the index's version of one file into the working tree.

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/compare-sides", s.handleCompareSides)
	})
	RegisterOp("copy-to-working", buildCopyToWorking)
}

type sideWire struct {
	Label string `json:"label"`
	Spec  string `json:"spec"`
	Hash  string `json:"hash,omitempty"`
}

// sideEndpoint parses a compare side's spec: the working tree, the index or
// a commit by hex id — nothing else (a name would make the set move).
func sideEndpoint(spec string) (model.Endpoint, sideWire, error) {
	switch {
	case spec == "worktree":
		return model.WorkTreeEndpoint(), sideWire{Label: "working tree", Spec: spec}, nil
	case spec == "staged":
		return model.IndexEndpoint(), sideWire{Label: "staged", Spec: spec}, nil
	case strings.HasPrefix(spec, "commit:"):
		hash := strings.TrimPrefix(spec, "commit:")
		if !isHexSha(hash) {
			return model.Endpoint{}, sideWire{}, errors.New("commit: must be a hex commit id")
		}
		return mustCommitEndpoint(hash), sideWire{Label: shortSha(hash), Spec: spec, Hash: hash}, nil
	}
	return model.Endpoint{}, sideWire{}, fmt.Errorf("unknown compare side %q", spec)
}

func (s *Server) handleCompareSides(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	lep, lw, err := sideEndpoint(q.Get("left"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	rep, rw, err := sideEndpoint(q.Get("right"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	files, err := s.service().CompareFiles(r.Context(), lep, rep)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]compareFile, len(files))
	for i, f := range files {
		out[i] = compareFile{Path: f.Path, Status: f.Status, OldPath: f.OldPath}
	}
	// specs: the page diffs each file by SPEC (/api/entry-diff) — a live
	// side has no hash for the rev lane.
	writeJSON(w, map[string]any{"left": lw, "right": rw, "files": out, "specs": true})
}

// buildCopyToWorking is the TUI's Copy to working dir: the file's bytes at a
// commit (sha, hex only) or in the index (ref "staged"), written into the
// working tree at its own path; engine.WriteFile asks before replacing a
// differing working file and refuses a path that leaves the tree.
func buildCopyToWorking(s *Server, r *http.Request, req opStartRequest) (engine.Operation, func(), int, error) {
	if req.Path == "" || !isGitArgSafe(req.Path) || strings.Contains("/"+req.Path+"/", "/../") {
		return nil, nil, http.StatusBadRequest, errors.New("invalid path")
	}
	var ref model.FileRef
	switch {
	case req.Ref == "staged":
		ref = model.FileRef{Source: model.SourceStaged, Path: req.Path}
	case isHexSha(req.Sha):
		ref = model.FileRef{Source: model.SourceCommit, Locator: req.Sha, Path: req.Path}
	default:
		return nil, nil, http.StatusBadRequest, errors.New("copy-to-working needs a hex commit id or ref \"staged\"")
	}
	data, err := s.service().ResolveBytes(readCtx(r), ref)
	if err != nil {
		return nil, nil, http.StatusUnprocessableEntity, err
	}
	return engine.WriteFile{Path: req.Path, Data: data}, nil, 0, nil
}
