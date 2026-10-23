package web

import (
	"errors"
	"net/http"
	"strings"

	"github.com/homeend/gigagit/internal/engine"
)

// The commit box's two missing lanes: have an agent write the message, and
// amend the last commit.
//
// The generate lane is review.go's third sibling (review, conflict-complete,
// and now commit_message) and keeps both of that file's load-bearing
// properties: the command text NEVER comes off the wire — the client names a
// TOOL and the server resolves the command from the effective config — and an
// unapproved command is refused until the user has seen it in full and said
// yes, remembered per repo against a hash of the config TEMPLATE text in the
// same store the TUI's lanes use.
//
// Nothing here commits. A generated message lands in the message box for the
// human to read, edit, and send — the TUI's ctrl+g does exactly that, and an
// agent that could commit on its own would be a different (and much larger)
// promise than "help me write this message".
func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/commit-message/head", s.handleHeadCommitMessage)
	})
	RegisterOp("commit-amend", buildCommitAmend)
}

// handleHeadCommitMessage answers HEAD's full message, for the amend prefill.
// /api/commit-message takes a hex sha and the browser has no honest way to
// name HEAD as one before the feed has loaded — and asking the server for "the
// last commit" is the question the TUI's amend asks too (LastCommitMessage).
func (s *Server) handleHeadCommitMessage(w http.ResponseWriter, r *http.Request) {
	msg, err := s.service().LastCommitMessage(readCtx(r))
	if err != nil {
		// No commit yet: there is nothing to amend, and that is the honest
		// answer rather than a 500.
		writeErr(w, http.StatusConflict, errors.New("no commit to amend"))
		return
	}
	writeJSON(w, map[string]any{"message": msg})
}

// buildCommitAmend rewrites the last commit with a new message — the TUI's C.
//
// Nothing here checks that there IS a commit to amend: git says
// "You have nothing to amend" better than a re-implementation of the rule
// would, and the op surfaces git's own words. The browser gates the button on
// a non-empty feed (the TUI's canAmend) so the refusal is rare rather than
// routine.
func buildCommitAmend(s *Server, r *http.Request, req opStartRequest) (engine.Operation, func(), int, error) {
	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		return nil, nil, http.StatusBadRequest, errors.New("an amended commit still needs a message")
	}
	return engine.Commit{Message: req.Message, Amend: true}, nil, 0, nil
}
