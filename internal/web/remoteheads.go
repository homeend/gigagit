package web

import (
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

// Check out a remote branch by name: the picker lists EVERY branch on a
// remote — the ones a narrowed/single-branch monorepo fetch refspec hides
// AND the ones already tracked (flagged) — so any name the user pastes that
// exists on the remote matches. Checking one out picks the op server-side
// from fresh reads: a tracked branch goes through SmartCheckout on its
// remote-tracking ref (the Remotes menu's check-out, no network, no config
// write); an unfetched one through engine.CheckoutRemoteBranch, which writes
// the per-branch fetch mapping and fetches exactly that branch first.

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/remote-heads", s.handleRemoteHeads)
	})
	RegisterOp("checkout-remote-head", buildCheckoutRemoteHead)
}

type remoteHeadRow struct {
	Name    string `json:"name"`    // short branch name on the remote
	Hash    string `json:"hash"`    // tip object id (the remote's; the local ref's when ls-remote failed)
	Tracked bool   `json:"tracked"` // a local remote-tracking ref exists
}

// trackedOn returns remote's remote-tracking branches from rbs, keyed by the
// branch name on the remote.
func trackedOn(rbs []model.RemoteBranch, remote string) map[string]model.RemoteBranch {
	out := make(map[string]model.RemoteBranch)
	for _, rb := range rbs {
		if rb.Remote == remote && rb.Branch != "HEAD" {
			out[rb.Branch] = rb
		}
	}
	return out
}

// handleRemoteHeads serves the picker's two phases: without ?remote it lists
// the configured remote names (a local read); with ?remote=<name> it runs the
// ls-remote and answers every head, each flagged tracked or not. The remote
// value is an identifier checked against a fresh RemoteNames read before any
// network call. An ls-remote failure (offline, auth) still answers 200 with
// the locally tracked branches and the error beside them — those check out
// without the network — and is a 502 only when there is nothing to show. The
// request context governs the read (an abandoned picker cancels it).
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
	rbs, rerr := svc.RemoteBranches(r.Context())
	if rerr != nil {
		writeErr(w, http.StatusInternalServerError, rerr)
		return
	}
	tracked := trackedOn(rbs, remote)
	heads, lerr := svc.RemoteHeads(r.Context(), remote)
	if lerr != nil && len(tracked) == 0 {
		writeErr(w, http.StatusBadGateway, lerr)
		return
	}
	rows := make([]remoteHeadRow, 0, len(heads)+len(tracked))
	seen := make(map[string]bool, len(heads))
	for _, h := range heads {
		_, tr := tracked[h.Name]
		rows = append(rows, remoteHeadRow{Name: h.Name, Hash: h.Hash, Tracked: tr})
		seen[h.Name] = true
	}
	// Tracked refs the remote did not list: every one when ls-remote failed,
	// else a branch deleted upstream but not yet pruned — still checkable out
	// from its local ref, as the Remotes section shows it.
	var extra []remoteHeadRow
	for name, rb := range tracked {
		if !seen[name] {
			extra = append(extra, remoteHeadRow{Name: name, Hash: rb.Hash, Tracked: true})
		}
	}
	slices.SortFunc(extra, func(a, b remoteHeadRow) int { return strings.Compare(a.Name, b.Name) })
	rows = append(rows, extra...)
	body := map[string]any{"remote": remote, "heads": rows}
	if lerr != nil {
		body["error"] = lerr.Error()
	}
	writeJSON(w, body)
}

// buildCheckoutRemoteHead resolves {remote, branch} against fresh reads —
// the wire values are identifiers, membership-checked and replaced by the
// server's own copies before they reach a config write or git argv (the
// add-fetch-mappings pattern). Tracked is decided here, never by the client:
// a branch with a remote-tracking ref checks out from it (SmartCheckout, the
// Remotes menu's op — no redundant per-branch fetch line on a wildcard
// refspec); otherwise a fresh ls-remote must list it and it goes through
// CheckoutRemoteBranch. A branch that got tracked after the picker loaded
// therefore takes the tracked path, not a double mapping.
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
	intent := engine.CheckoutStay
	if req.Switch {
		intent = engine.CheckoutSwitch
	}
	rbs, err := svc.RemoteBranches(r.Context())
	if err != nil {
		return nil, nil, http.StatusInternalServerError, err
	}
	if rb, ok := trackedOn(rbs, req.Remote)[req.Branch]; ok {
		return engine.SmartCheckout{RemoteRef: rb.Name, Local: rb.Branch, Intent: intent}, nil, 0, nil
	}
	heads, err := svc.UnfetchedRemoteHeads(r.Context(), req.Remote)
	if err != nil {
		return nil, nil, http.StatusBadGateway, err
	}
	for _, h := range heads {
		if h.Name == req.Branch {
			return engine.CheckoutRemoteBranch{Remote: req.Remote, Branch: h.Name, Intent: intent}, nil, 0, nil
		}
	}
	return nil, nil, http.StatusNotFound, errors.New("no such branch on the remote")
}
