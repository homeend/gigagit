package web

import "net/http"

// Route registry — the read/write endpoints' half of opreg.go.
//
// A feature that needs its own endpoint registers it from its own file rather
// than adding a line to the one route table in server.go:
//
//	func init() {
//		RegisterRoutes(func(mux *http.ServeMux, s *Server) {
//			mux.HandleFunc("GET /api/mine", s.handleMine)
//			mux.HandleFunc("POST /api/mine", writeGuard(s.handleMineSet))
//		})
//	}
//
// Registered routes are added AFTER the built-in ones, so a feature cannot
// shadow a core endpoint by accident: net/http's mux rejects a duplicate
// pattern by panicking at startup, which is the moment to find out.
//
// Anything that mutates must still be wrapped in writeGuard, exactly as the
// built-in table does — the registry moves where routes are declared, not the
// rules they live under.

type RouteFunc func(mux *http.ServeMux, s *Server)

var routeRegistry []RouteFunc

// RegisterRoutes adds fn to the set applied to every Server's mux.
func RegisterRoutes(fn RouteFunc) {
	if fn == nil {
		panic("web: RegisterRoutes with a nil func")
	}
	routeRegistry = append(routeRegistry, fn)
}

// applyRegisteredRoutes is called by Handler once its own table is in place.
func (s *Server) applyRegisteredRoutes(mux *http.ServeMux) {
	for _, fn := range routeRegistry {
		fn(mux, s)
	}
}
