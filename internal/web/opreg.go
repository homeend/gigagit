package web

import (
	"fmt"
	"net/http"

	"github.com/homeend/gigagit/internal/engine"
)

// Operation registry.
//
// handleOpStart used to be the only way to add an operation: one more `case`
// in a switch that is already 43 arms long. That is fine for one author and
// hostile to several — every parallel branch edits the same lines of the same
// file, and the merges are where rows quietly go missing.
//
// A registered op lives in its own file instead:
//
//	func init() { RegisterOp("apply-patch", buildApplyPatch) }
//
// The existing switch is untouched and still authoritative for everything
// already in it. Migrating those 43 arms would be a large diff in exactly the
// file this registry exists to keep out of merges, and would buy nothing.

// OpBuilder turns a request into the operation to run. It mirrors the shape
// the in-switch builders already use:
//
//   - cleanup, when non-nil, runs after the operation finishes — the shelved
//     commit's patch lane writes a temp file that has to outlive the build but
//     not the run.
//   - a non-nil error is a refusal: code is the HTTP status to answer with, so
//     a builder can say 404 / 422 / 400 rather than everything being a 500.
type OpBuilder func(s *Server, r *http.Request, req opStartRequest) (op engine.Operation, cleanup func(), code int, err error)

var opRegistry = map[string]OpBuilder{}

// RegisterOp adds a builder under name. It panics on a duplicate: two features
// answering to one wire name is a bug that must fail at startup, not at 3am
// when whichever init() ran last silently wins.
func RegisterOp(name string, b OpBuilder) {
	if name == "" {
		panic("web: RegisterOp with an empty name")
	}
	if b == nil {
		panic("web: RegisterOp(" + name + ") with a nil builder")
	}
	if _, dup := opRegistry[name]; dup {
		panic(fmt.Sprintf("web: RegisterOp(%q) called twice", name))
	}
	opRegistry[name] = b
}

// lookupOp returns the builder registered for name.
func lookupOp(name string) (OpBuilder, bool) {
	b, ok := opRegistry[name]
	return b, ok
}
