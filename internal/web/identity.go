package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/homeend/gigagit/internal/model"
)

// identityWireRow mirrors model.Identity with raw set-flags: the client
// renders the "(not set — inherits global)" note itself, so the wire carries
// facts, not display strings.
type identityRow struct {
	GlobalName     string `json:"global_name"`
	GlobalEmail    string `json:"global_email"`
	GlobalSet      bool   `json:"global_set"`
	LocalName      string `json:"local_name"`
	LocalEmail     string `json:"local_email"`
	LocalSet       bool   `json:"local_set"`
	EffectiveName  string `json:"effective_name"`
	EffectiveEmail string `json:"effective_email"`
}

type profileRow struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	GitName  string `json:"git_name"`
	GitEmail string `json:"git_email"`
	Scope    string `json:"scope"` // "global" | "repo" (model.ProfileScope.String)
}

// handleIdentity returns the current git identity plus the named profiles in
// one payload — the same pair the TUI identity view loads on open.
func (s *Server) handleIdentity(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	id, err := svc.Identity(readCtx(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	ps, err := svc.Profiles(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	rows := make([]profileRow, 0, len(ps))
	for _, p := range ps {
		rows = append(rows, profileRow{ID: p.ID, Name: p.Name, GitName: p.GitName, GitEmail: p.GitEmail, Scope: p.Scope.String()})
	}
	writeJSON(w, map[string]any{
		"identity": identityRow{
			GlobalName: id.GlobalName, GlobalEmail: id.GlobalEmail, GlobalSet: id.GlobalSet,
			LocalName: id.LocalName, LocalEmail: id.LocalEmail, LocalSet: id.LocalSet,
			EffectiveName: id.EffectiveName, EffectiveEmail: id.EffectiveEmail,
		},
		"profiles": rows,
	})
}

// parseProfileScope maps the wire scope to the enum; the bool reports whether
// the value was one of the two allowed words.
func parseProfileScope(s string) (model.ProfileScope, bool) {
	switch s {
	case "global":
		return model.ProfileScopeGlobal, true
	case "repo":
		return model.ProfileScopeRepo, true
	}
	return 0, false
}

// handleProfileAdd creates or renames a profile. Rename mirrors the TUI's
// addProfileCmd exactly: add the new row FIRST, then remove the renamed-from
// original only when it survived under a different id — a failed add never
// loses the original.
func (s *Server) handleProfileAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		GitName     string `json:"git_name"`
		GitEmail    string `json:"git_email"`
		Scope       string `json:"scope"`
		RenameFrom  string `json:"rename_from"`
		RenameScope string `json:"rename_scope"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	// Validate everything before any write (the settings-handler rule): the
	// TUI form enforces non-empty fields, so the domain store would happily
	// persist empties — this layer must refuse them instead.
	name := strings.TrimSpace(req.Name)
	gitName := strings.TrimSpace(req.GitName)
	gitEmail := strings.TrimSpace(req.GitEmail)
	if name == "" || gitName == "" || gitEmail == "" {
		writeErr(w, http.StatusBadRequest, errors.New("profile name, git name and email are required"))
		return
	}
	scope, ok := parseProfileScope(req.Scope)
	if !ok {
		writeErr(w, http.StatusBadRequest, errors.New("scope must be global or repo"))
		return
	}
	var renameScope model.ProfileScope
	if req.RenameFrom != "" {
		if renameScope, ok = parseProfileScope(req.RenameScope); !ok {
			writeErr(w, http.StatusBadRequest, errors.New("rename_scope must be global or repo"))
			return
		}
	}
	svc := s.service()
	added, err := svc.AddProfile(r.Context(), model.Profile{Name: name, GitName: gitName, GitEmail: gitEmail, Scope: scope})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if req.RenameFrom != "" && req.RenameFrom != added.ID {
		_ = svc.RemoveProfile(r.Context(), renameScope, req.RenameFrom)
	}
	writeJSON(w, map[string]string{"id": added.ID})
}

// handleProfileRemove deletes one profile. The (scope,id) pair resolves
// against a fresh list read — the web convention for identifiers — so an id
// the store no longer holds is a clean 404, not a store-internal error.
func (s *Server) handleProfileRemove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Scope string `json:"scope"`
		ID    string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	scope, ok := parseProfileScope(req.Scope)
	if !ok {
		writeErr(w, http.StatusBadRequest, errors.New("scope must be global or repo"))
		return
	}
	if req.ID == "" {
		writeErr(w, http.StatusBadRequest, errors.New("id required"))
		return
	}
	svc := s.service()
	ps, err := svc.Profiles(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	found := false
	for _, p := range ps {
		if p.ID == req.ID && p.Scope == scope {
			found = true
			break
		}
	}
	if !found {
		writeErr(w, http.StatusNotFound, errors.New("unknown profile"))
		return
	}
	if err := svc.RemoveProfile(r.Context(), scope, req.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
