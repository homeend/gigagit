package web

import "net/http"

// handleUIConfig is the big-repo banner's original accept path for
// "[ui] show_graph / commit_sort". The settings panel writer accepts a
// superset of the same request shape with identical validation, per-key
// file routing, and the commit_sort feed reset, so this endpoint simply
// delegates — one writer, two routes (both behind writeGuard).
func (s *Server) handleUIConfig(w http.ResponseWriter, r *http.Request) {
	s.handleSettingsSet(w, r)
}
