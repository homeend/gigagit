package web

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/homeend/gigagit/internal/domain"
)

// The migration consent surface: what the TUI's pre-launch gate asks at a
// terminal prompt, this exposes over HTTP for the browser to ask in a panel
// before the app renders. Unlike the TUI (which runs once before the UI
// starts and can block on stdin), a browser tab reloads the page on every
// re-root and refresh, so there is no separate "already asked" state to
// track here either — GET /api/preflight is simply asked again each time,
// which is the same "no suppression" rule the TUI comment states.
//
// gatedFeatures is the one-entry table (today) of feature ids whose
// disablement the SPA cares about hiding UI for. Kept next to the handler
// rather than scattered, same convention as MCP's tool→feature table.
var gatedFeatures = []string{domain.FeatureVersions}

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/preflight", s.handlePreflight)
		mux.HandleFunc("POST /api/preflight/migrate", writeGuard(s.handlePreflightMigrate))
	})
}

// preflightMigrationRow is one pending migration on the wire — the same
// facts domain.PendingMigration carries, at a stable JSON shape.
type preflightMigrationRow struct {
	Feature     string   `json:"feature"`
	Store       string   `json:"store"`
	From        int      `json:"from"`
	To          int      `json:"to"`
	Consequence string   `json:"consequence"`
	Refs        []string `json:"refs"`
}

func (s *Server) handlePreflight(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	ctx := readCtx(r)

	pending, err := svc.PendingMigrations(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	rows := make([]preflightMigrationRow, 0, len(pending))
	for _, m := range pending {
		rows = append(rows, preflightMigrationRow{
			Feature: m.Feature, Store: m.Store, From: m.From, To: m.To,
			Consequence: m.Consequence, Refs: m.Refs,
		})
	}

	disabled := make([]string, 0, len(gatedFeatures))
	for _, id := range gatedFeatures {
		if !svc.FeatureEnabled(ctx, id) {
			disabled = append(disabled, id)
		}
	}

	writeJSON(w, map[string]any{"pending": rows, "disabled": disabled})
}

// handlePreflightMigrate applies one pending migration. The wire never
// carries a Store/From/To/Refs the way domain.PendingMigration does — those
// name exactly what RunMigration deletes, and a browser page that could
// supply them directly could be made to delete arbitrary refs. Instead the
// request names only the feature id, and the migration to run is resolved
// server-side against PendingMigrations, exactly as gitconfig resolves a
// key against its curated catalog before ever touching git.
func (s *Server) handlePreflightMigrate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Feature string `json:"feature"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if body.Feature == "" {
		writeErr(w, http.StatusBadRequest, errors.New("feature is required"))
		return
	}

	svc := s.service()
	ctx := r.Context()

	pending, err := svc.PendingMigrations(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	var found *domain.PendingMigration
	for i := range pending {
		if pending[i].Feature == body.Feature {
			found = &pending[i]
			break
		}
	}
	if found == nil {
		writeErr(w, http.StatusNotFound, errors.New("no pending migration for feature "+body.Feature))
		return
	}

	if err := svc.RunMigration(ctx, *found); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}
