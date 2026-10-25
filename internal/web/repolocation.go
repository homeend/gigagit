package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"slices"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
)

// The TUI's Settings → Repo settings location: copy or move the whole
// per-repo config between the committed .gg.toml and the private user-dir
// file. The paths are always the server's own (domain.RepoConfigPaths); the
// wire names an action and whether the user confirmed an overwrite.

type repoLocationSlot struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
}

type repoLocationWire struct {
	Committed repoLocationSlot `json:"committed"`
	Private   repoLocationSlot `json:"private"`
	Actions   []string         `json:"actions"`
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func fileNonEmpty(path string) bool {
	if path == "" {
		return false
	}
	st, err := os.Stat(path)
	return err == nil && st.Size() > 0
}

// repoLocation reads the two places and the applicable actions now.
func repoLocation(ctx context.Context, svc *domain.Service) (committed, private string, acts []domain.RepoConfigAction, wire repoLocationWire, err error) {
	committed, private, err = svc.RepoConfigPaths(ctx)
	if err != nil {
		return "", "", nil, repoLocationWire{}, err
	}
	ce, pe := fileExists(committed), fileExists(private)
	acts = domain.RepoConfigActions(ce, pe, committed != "", private != "")
	wire = repoLocationWire{
		Committed: repoLocationSlot{Path: committed, Exists: ce},
		Private:   repoLocationSlot{Path: private, Exists: pe},
		Actions:   []string{},
	}
	for _, a := range acts {
		wire.Actions = append(wire.Actions, string(a))
	}
	return committed, private, acts, wire, nil
}

// handleRepoLocation runs one relocation: the TUI's repoConfigPopup.run.
// An action not applicable now (a file appeared or went away since the page
// read it) is refused; a destination holding content needs overwrite, else
// 409 carries the TUI's question.
func (s *Server) handleRepoLocation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action    string `json:"action"`
		Overwrite bool   `json:"overwrite"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("bad request body"))
		return
	}
	svc := s.service()
	committed, private, acts, _, err := repoLocation(r.Context(), svc)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	act := domain.RepoConfigAction(req.Action)
	if !slices.Contains(acts, act) {
		writeErr(w, http.StatusBadRequest, errors.New("nothing to do: that move does not apply to the files here now"))
		return
	}
	src, dst, isMove := domain.RepoConfigEndpoints(act, committed, private)
	if fileNonEmpty(dst) && !req.Overwrite {
		q := "Overwrite " + dst + " ?"
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": q, "confirm": q})
		return
	}
	if err := config.CopyRepoConfig(src, dst); err != nil {
		writeJSON(w, map[string]any{"msg": "repo settings: " + err.Error(), "err": true})
		return
	}
	msg := "repo settings: " + domain.RepoConfigActionLabel(act) + " done"
	failed := false
	if isMove {
		if err := config.RemoveRepoConfig(src); err != nil {
			msg, failed = "repo settings: copied but source not removed: "+err.Error(), true
		}
	}
	// The file layout changed: the feed re-reads commit_sort, the service
	// re-reads its policies from the now active file, block paths moved.
	s.resetFeed()
	applyUIPolicies(r.Context(), svc, s.activeRepoConfigPathOr(r.Context(), svc))
	s.forgetToolStatuses()
	s.notifyConfigChanged(true)
	writeJSON(w, map[string]any{"msg": msg, "err": failed})
}
