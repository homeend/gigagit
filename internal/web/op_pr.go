package web

import (
	"errors"
	"net/http"

	"github.com/homeend/gigagit/internal/engine"
)

// The two pull-request ops. Both are read-only as far as the forge goes:
// pr-fetch brings the PR's head into the gg-private refs/gg/pr/<n>, pr-forget
// drops that ref. The page names the PR by number; nothing else is accepted.
//
// The OpBuilder's cleanup slot is the post-run hook: a finished fetch makes
// the PR "known" (and flips its row's fetched flag), a forget removes a row,
// so both re-list and tell every open page.

func init() {
	RegisterOp("pr-fetch", buildPRFetch)
	RegisterOp("pr-forget", buildPRForget)
}

var errPRNumber = errors.New("number must be a positive pull-request number")

func buildPRFetch(s *Server, r *http.Request, req opStartRequest) (engine.Operation, func(), int, error) {
	if req.Number <= 0 {
		return nil, nil, http.StatusBadRequest, errPRNumber
	}
	svc := s.service()
	// PRFetchOp asks the forge for the head sha and the base repository — the
	// one forge call an op start makes. Whatever it refuses with (no such PR,
	// no usable forge) is the caller's answer.
	op, err := svc.PRFetchOp(r.Context(), req.Number)
	if err != nil {
		return nil, nil, http.StatusUnprocessableEntity, err
	}
	return op, func() { s.kickPRs(svc, true) }, 0, nil
}

func buildPRForget(s *Server, _ *http.Request, req opStartRequest) (engine.Operation, func(), int, error) {
	if req.Number <= 0 {
		return nil, nil, http.StatusBadRequest, errPRNumber
	}
	svc := s.service()
	n := req.Number
	return svc.PRForgetOp(n), func() {
		s.dropCachedPR(svc, n)
		s.kickPRs(svc, true)
	}, 0, nil
}
