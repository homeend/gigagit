package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

// Sending to GitHub from the page (spec 2026-10-07 §4.2). The page names
// WHAT to send with values this server handed it — the PR number, note and
// remark ids, GitHub thread ids, a group id — and every one is checked
// against the PR's cached notes before anything is planned. The plan is
// built here, in the handler, outside the repo gate (R12); the op parks on
// its one confirm in the page's modal, which draws the plan the answer
// carries. Agents never send (user ruling 2026-10-08): no gg tool or skill
// reaches this route.

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("POST /api/pr/send", writeGuard(s.handlePRSend))
		mux.HandleFunc("GET /api/pr/send/groups", s.handlePRSendGroups)
	})
}

// prSendBudget bounds the forge reads a send's plan makes (the PR, its
// comments): a forge that hangs answers 504, never a hung request. A var so
// tests can shorten it.
var prSendBudget = prRevalidateBudget

// errBadSend marks a request the page got wrong: 400. Every other refusal is
// a lookup the request cannot fix (prSendLookupStatus) or a planning refusal
// (sendErrStatus).
var errBadSend = errors.New("bad send request")

// badSend is a request fault whose words stay the page's message.
type badSend struct{ error }

func (b badSend) Unwrap() error { return errBadSend }

const (
	maxSendBody = 64 << 10 // a review body the user typed
	maxSendIDs  = 500
)

// prSendWire is the page's request: what to send, never how.
type prSendWire struct {
	Kind    string   `json:"kind"` // notes | group | verdict | resolve | unresolve | finish | discard
	IDs     []string `json:"ids"`
	Group   string   `json:"group"`
	Body    string   `json:"body"`
	BodySet bool     `json:"body_set"`
}

type sendItemWire struct {
	Label   string `json:"label"`
	Replies int    `json:"replies,omitempty"`
	Resolve bool   `json:"resolve,omitempty"`
}

type sendSkipWire struct {
	Label  string `json:"label"`
	Reason string `json:"reason"`
}

// sendPlanWire is what the page's confirm draws: never a sha, a node id or a
// local key — the page only ever posts back one of the decision's options.
type sendPlanWire struct {
	Target     string         `json:"target"`
	Mode       string         `json:"mode"`
	Verdict    bool           `json:"verdict"`
	OwnPR      bool           `json:"own_pr"`
	HasPending bool           `json:"has_pending"`
	Body       string         `json:"body"`
	Items      []sendItemWire `json:"items"`
	Skipped    []sendSkipWire `json:"skipped"`
}

var sendModeWire = map[engine.SendMode]string{engine.SendReview: "review", engine.SendActions: "actions",
	engine.SendFinish: "finish", engine.SendDiscard: "discard"}

func planWire(p engine.SendPlan) sendPlanWire {
	w := sendPlanWire{Target: p.Target, Mode: sendModeWire[p.Mode], Verdict: p.Verdict, OwnPR: p.OwnPR,
		HasPending: p.Pending != "", Body: p.BodyText(), Items: []sendItemWire{}, Skipped: []sendSkipWire{}}
	for _, it := range p.Items {
		w.Items = append(w.Items, sendItemWire{Label: it.Label, Replies: len(it.Replies), Resolve: it.Resolve})
	}
	for _, sk := range p.Skipped {
		w.Skipped = append(w.Skipped, sendSkipWire{Label: sk.Label, Reason: sk.Reason})
	}
	return w
}

// prSendIDs are the ids PR n's view holds: local roots, review remarks and
// draft replies (notes), and GitHub thread roots (threads).
func prSendIDs(ctx context.Context, svc *domain.Service, n int) (notes, threads map[string]bool, err error) {
	byPath, err := svc.PRNotes(ctx, n)
	if err != nil {
		return nil, nil, err
	}
	notes, threads = map[string]bool{}, map[string]bool{}
	for _, rs := range byPath {
		for _, r := range rs {
			if r.Note.Source == model.NoteSourceForge {
				threads[r.Note.ID] = true
			} else {
				notes[r.Note.ID] = true
			}
			for _, rep := range r.Replies {
				if rep.Note.Source != model.NoteSourceForge {
					notes[rep.Note.ID] = true
				}
			}
		}
	}
	return notes, threads, nil
}

// prSendRequest turns the page's request into the domain's, refusing any
// value the PR's cached notes do not hold.
func prSendRequest(ctx context.Context, svc *domain.Service, n int, in prSendWire) (domain.PRSendRequest, error) {
	req := domain.PRSendRequest{PR: n}
	if len(in.Body) > maxSendBody {
		return req, badSend{fmt.Errorf("the review body is over %d KiB", maxSendBody>>10)}
	}
	if len(in.IDs) > maxSendIDs {
		return req, badSend{errors.New("too many ids")}
	}
	pick := func(allowed map[string]bool) ([]string, error) {
		if len(in.IDs) == 0 {
			return nil, badSend{errors.New("ids required")}
		}
		for _, id := range in.IDs {
			if !allowed[id] {
				return nil, badSend{fmt.Errorf("%q is not a note of pull request #%d", id, n)}
			}
		}
		return in.IDs, nil
	}
	var err error
	switch in.Kind {
	case "notes", "resolve", "unresolve":
		notes, threads, lerr := prSendIDs(ctx, svc, n)
		if lerr != nil {
			return req, lerr
		}
		switch in.Kind {
		case "notes":
			req.Notes, err = pick(notes)
		case "resolve":
			req.Resolve, err = pick(threads)
		default:
			req.Unresolve, err = pick(threads)
		}
	case "group":
		groups, gerr := svc.PRSendGroups(ctx, n)
		if gerr != nil {
			return req, gerr
		}
		listed := false
		for _, g := range groups {
			listed = listed || g.ID == in.Group
		}
		switch {
		case !listed:
			err = badSend{fmt.Errorf("%q is not a group of pull request #%d with notes to send", in.Group, n)}
		case in.Group == domain.GroupMine:
			req.Mine = true
		default:
			req.Review = strings.TrimPrefix(in.Group, "review:")
		}
		req.Body, req.BodySet = in.Body, in.BodySet
	case "verdict":
		req.Verdict, req.Body = true, in.Body
	case "finish":
		req.Finish = true
	case "discard":
		req.Discard = true
	default:
		err = badSend{fmt.Errorf("unknown kind %q", in.Kind)}
	}
	return req, err
}

func (s *Server) handlePRSend(w http.ResponseWriter, r *http.Request) {
	svc, pr, ok := s.knownPR(w, r)
	if !ok {
		return
	}
	var in prSendWire
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSendBody+(16<<10))).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad request body: %w", err))
		return
	}
	if s.opInFlight() { // before any planning: a plan reads under the gate a parked op may hold
		writeErr(w, http.StatusConflict, errOpBusy)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), prSendBudget) // the planning's forge reads; the op runs on its own
	defer cancel()
	req, err := prSendRequest(ctx, svc, pr.Number, in)
	if err != nil {
		status := http.StatusBadRequest
		if !errors.Is(err, errBadSend) {
			status = prSendLookupStatus(err)
		}
		writeErr(w, status, err)
		return
	}
	op, err := svc.PRSendOp(ctx, req) // outside the gate (R12)
	if errors.Is(err, domain.ErrPRHeadMoved) {
		// A machine code beside the words: the page fetches the new head and
		// re-opens the diff (the send is planned again on what is then shown).
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error(), "code": "head_moved"})
		return
	}
	if err != nil {
		writeErr(w, sendErrStatus(err), err)
		return
	}
	n := pr.Number
	run, err := s.startRun("op", func(ctx context.Context, svc *domain.Service, events chan<- engine.Event, dec engine.Decider) (engine.Result, map[string]any, error) {
		res, err := svc.Execute(ctx, op, events, dec)
		return res, map[string]any{"notes_changed": true, "pr": n}, err
	})
	if err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"op_id": run.id, "plan": planWire(op.Plan)})
}

// prSendLookupStatus maps a failure to read the PR's notes or groups — never
// the request's fault — to its HTTP status.
func prSendLookupStatus(err error) int {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout
	case errors.Is(err, domain.ErrForgeUnavailable):
		return http.StatusBadGateway
	}
	return http.StatusUnprocessableEntity
}

// sendErrStatus maps a planning refusal to its HTTP status.
func sendErrStatus(err error) int {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout
	case errors.Is(err, domain.ErrPRHeadMoved), errors.Is(err, domain.ErrInterruptedPending):
		return http.StatusConflict
	case errors.Is(err, domain.ErrSendRequest), errors.Is(err, domain.ErrMixedSend):
		return http.StatusBadRequest
	}
	return http.StatusUnprocessableEntity
}

// handlePRSendGroups is Send review…'s group list: each group with its
// colour slot and, for an AI review, its summary as the body prefill.
func (s *Server) handlePRSendGroups(w http.ResponseWriter, r *http.Request) {
	svc, pr, ok := s.knownPR(w, r)
	if !ok {
		return
	}
	ctx := readCtx(r)
	groups, err := svc.PRSendGroups(ctx, pr.Number)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}
	out := []map[string]any{}
	for _, g := range groups {
		body := ""
		if id, ok := strings.CutPrefix(g.ID, "review:"); ok {
			body, _ = svc.ReviewBodyText(ctx, id)
		}
		out = append(out, map[string]any{"id": g.ID, "agent": g.Agent, "summary": g.Summary, "count": g.Count,
			"slot": domain.GroupSlot(g.ID), "body": body})
	}
	own := false
	if p, _, ok := svc.PRDetailsCached(pr.Number); ok {
		own = p.ViewerDidAuthor
	}
	writeJSON(w, map[string]any{"groups": out, "own_pr": own})
}
