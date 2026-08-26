package web

import (
	"errors"
	"net/http"
	"slices"

	"github.com/homeend/gigagit/internal/engine"
)

// Browse remote branches: list the branches that exist on a remote but have
// no local remote-tracking ref (a narrowed/single-branch monorepo fetch
// refspec hides them), and check one out via engine.CheckoutRemoteBranch.
// The existing "checkout-remote" op cannot serve this — it resolves against
// RemoteBranches, the set of already-tracked refs.

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/remote-heads", s.handleRemoteHeads)
	})
	RegisterOp("checkout-remote-head", buildCheckoutRemoteHead)
}

type remoteHeadRow struct {
	Name string `json:"name"` // short branch name on the remote
	Hash string `json:"hash"` // tip object id on the remote
}

// handleRemoteHeads serves the picker's two phases: without ?remote it lists
// the configured remote names (a local read); with ?remote=<name> it runs the
// ls-remote and answers the unfetched heads. The remote value is an
// identifier checked against a fresh RemoteNames read before any network
// call. This is a user-invoked NETWORK read, so errors are surfaced rather
// than degraded to an empty list, and the request context governs it (an
// abandoned picker cancels its ls-remote).
func (s *Server) handleRemoteHeads(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	names, err := svc.RemoteNames(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	remote := r.URL.Query().Get("remote")
	if remote == "" {
		writeJSON(w, map[string]any{"remotes": names})
		return
	}
	if !isGitArgSafe(remote) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid remote"))
		return
	}
	if !slices.Contains(names, remote) {
		writeErr(w, http.StatusNotFound, errors.New("unknown remote"))
		return
	}
	heads, err := svc.UnfetchedRemoteHeads(r.Context(), remote)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	rows := make([]remoteHeadRow, 0, len(heads))
	for _, h := range heads {
		rows = append(rows, remoteHeadRow{Name: h.Name, Hash: h.Hash})
	}
	writeJSON(w, map[string]any{"remote": remote, "heads": rows})
}

// buildCheckoutRemoteHead resolves {remote, branch} against fresh
// RemoteNames + UnfetchedRemoteHeads reads — the wire values are identifiers,
// membership-checked and replaced by the server's own copies before they
// reach a config write or git argv (the add-fetch-mappings pattern). The
// re-read also closes the gap where the picker's list went stale: a branch
// that got tracked meanwhile is refused, not double-mapped.
func buildCheckoutRemoteHead(s *Server, r *http.Request, req opStartRequest) (engine.Operation, func(), int, error) {
	if req.Remote == "" || !isGitArgSafe(req.Remote) {
		return nil, nil, http.StatusBadRequest, errors.New("invalid remote")
	}
	if req.Branch == "" || !isGitArgSafe(req.Branch) {
		return nil, nil, http.StatusBadRequest, errors.New("invalid branch")
	}
	svc := s.service()
	names, err := svc.RemoteNames(r.Context())
	if err != nil {
		return nil, nil, http.StatusInternalServerError, err
	}
	if !slices.Contains(names, req.Remote) {
		return nil, nil, http.StatusNotFound, errors.New("unknown remote")
	}
	heads, err := svc.UnfetchedRemoteHeads(r.Context(), req.Remote)
	if err != nil {
		return nil, nil, http.StatusBadGateway, err
	}
	for _, h := range heads {
		if h.Name == req.Branch {
			intent := engine.CheckoutStay
			if req.Switch {
				intent = engine.CheckoutSwitch
			}
			return engine.CheckoutRemoteBranch{Remote: req.Remote, Branch: h.Name, Intent: intent}, nil, 0, nil
		}
	}
	return nil, nil, http.StatusNotFound, errors.New("not an unfetched remote branch")
}
