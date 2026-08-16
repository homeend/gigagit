package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

// The notification centre — the browser's half of the TUI's `!` dialog.
//
// The TUI notices repository problems it can fix and offers the fix behind one
// key; the browser noticed none of them. The first (and so far only) finding
// carried here is the narrowed fetch refspec: on a single-branch or shallow
// clone a push succeeds while the branch's remote-tracking ref never moves, so
// the ↓↑ markers and ahead/behind silently stay wrong and nobody is told why.
//
// Two rules this file lives under:
//
//   - **Notice ids are permanent.** A dismissal is remembered by id in
//     prompts.toml, so an id that ships is a promise to that file: renaming one
//     silently un-dismisses it for everybody. Choose them like config keys.
//   - **Suppression lives in promptstate, never in browser storage.** `gg web`
//     binds a RANDOM loopback port, so every run is a different origin and
//     localStorage starts empty — a dismissal kept there would come back on the
//     next run.

// noticeNarrowRefspec is the "branches aren't tracked by the fetch refspec"
// finding. PERMANENT id, and deliberately the TUI's own (internal/tui/notify.go):
// it is the same advice about the same repo, so dismissing it in either
// frontend silences both — the commit_graph_recommend precedent in health.go.
const noticeNarrowRefspec = "narrow_fetch_refspec"

// noticePushMapOffer suppresses the fork engine.Push already raises after a
// push whose remote-tracking ref did not move (FetchMappingDecisionID): on a
// single-branch clone that question comes back on EVERY push of every unmapped
// branch, and a modal you cannot turn off is one you learn to dismiss without
// reading. Dismissing this id answers it "skip" from then on.
//
// PERMANENT id, and separate from the notice above on purpose: silencing the
// per-push question must not also hide the finding, which stays in the centre
// with its repair — that is what makes turning the question off safe rather
// than a way to lose the problem.
//
// Scoped per repo (DismissNotice) rather than globally: a narrowed refspec is a
// property of one clone, so an answer here says nothing about another repo.
const noticePushMapOffer = "web_push_mapping_offer"

// dismissableNotices is the allowlist for POST /api/notifications/dismiss. A
// client bug can then never write junk keys into prompts.toml, which is a file
// nothing ever garbage-collects (the handleNoticeDismiss precedent).
var dismissableNotices = map[string]bool{
	noticeNarrowRefspec: true,
	noticePushMapOffer:  true,
}

// opAddFetchMappings is the wire name of the refspec repair. PERMANENT.
const opAddFetchMappings = "add-fetch-mappings"

// noticeAction is one offer attached to a notice. Op is the wire op name the
// client POSTs to /api/op; Branch narrows it to a single branch ("" = every
// affected one, the batch action). Dismiss, when set instead of Op, makes the
// row a suppression rather than an operation — the id it names is dismissed.
type noticeAction struct {
	Op      string `json:"op,omitempty"`
	Label   string `json:"label"`
	Branch  string `json:"branch,omitempty"`
	Dismiss string `json:"dismiss,omitempty"`
}

// noticeWire is one finding as the browser sees it.
type noticeWire struct {
	ID      string         `json:"id"`
	Title   string         `json:"title"`
	Detail  []string       `json:"detail"`
	Items   []string       `json:"items,omitempty"` // the affected things, one row each
	Actions []noticeAction `json:"actions"`
}

type noticesResp struct {
	Notices []noticeWire `json:"notices"`
	// Suppressed reports the dismissable ids the user already turned off —
	// including ones that produce no notice row (the post-push offer), which
	// the client needs before deciding whether to pop it up.
	Suppressed map[string]bool `json:"suppressed"`
}

// NoticeBuilder derives zero or more notices from one health snapshot. It is
// the notification centre's extension point: a feature adds a finding from its
// OWN file (RegisterNotice in an init) rather than editing this one, exactly as
// RegisterOp and registerRows keep parallel features out of each other's merges.
// Builders must not perform network calls — the centre is refreshed after every
// operation and has to stay cheap. dismissed carries this repo's persisted
// dismissals so a builder can leave out an offer the user already turned off
// without re-reading the store.
type NoticeBuilder func(ctx context.Context, s *Server, svc *domain.Service, h model.RepoHealth, dismissed map[string]bool) []noticeWire

var noticeRegistry []NoticeBuilder

// RegisterNotice adds a builder to the notification centre.
func RegisterNotice(b NoticeBuilder) {
	if b == nil {
		panic("web: RegisterNotice with a nil builder")
	}
	noticeRegistry = append(noticeRegistry, b)
}

func init() {
	RegisterNotice(narrowRefspecNotice)
	RegisterAutoAnswer(skipSuppressedFetchMapping)
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/notifications", s.handleNotifications)
		mux.HandleFunc("POST /api/notifications/dismiss", writeGuard(s.handleNotificationDismiss))
	})
	RegisterOp(opAddFetchMappings, buildAddFetchMappings)
}

// handleNotifications answers with every finding that is neither dismissed for
// this repo nor empty. Health failures degrade to an empty list rather than an
// error: the TUI's "health never surfaces errors in the UI" posture applies
// here too — a centre that 500s is worse than a centre with nothing in it.
func (s *Server) handleNotifications(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	resp := noticesResp{Notices: []noticeWire{}, Suppressed: map[string]bool{}}
	h, err := svc.RepoHealth(readCtx(r))
	if err != nil {
		writeJSON(w, resp)
		return
	}
	rememberRepoKey(svc, h.GitCommonDir) // for the auto-answer policies, which cannot ask git
	var dismissed map[string]bool
	if store := s.promptStore(); store != nil && h.GitCommonDir != "" {
		dismissed = store.DismissedNotices(h.GitCommonDir)
	}
	for id := range dismissableNotices {
		resp.Suppressed[id] = dismissed[id]
	}
	for _, build := range noticeRegistry {
		for _, n := range build(readCtx(r), s, svc, h, dismissed) {
			if dismissed[n.ID] {
				continue
			}
			resp.Notices = append(resp.Notices, n)
		}
	}
	writeJSON(w, resp)
}

// handleNotificationDismiss persists a "never for this repo" dismissal into the
// TUI-shared prompts store, keyed by git common dir. The id is allowlisted.
func (s *Server) handleNotificationDismiss(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("bad request body"))
		return
	}
	if !dismissableNotices[req.ID] {
		writeErr(w, http.StatusBadRequest, errors.New("unknown notice id"))
		return
	}
	svc := s.service()
	key, err := svc.GitCommonDir(r.Context())
	if err != nil || key == "" {
		writeErr(w, http.StatusInternalServerError, errors.New("cannot resolve repo key"))
		return
	}
	store := s.promptStore()
	if store == nil {
		writeErr(w, http.StatusInternalServerError, errors.New("no state dir for dismissals"))
		return
	}
	if err := store.DismissNotice(key, req.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	rememberRepoKey(svc, key)
	writeJSON(w, map[string]bool{"ok": true})
}

// narrowRefspecNotice mirrors the TUI notice of the same name: branches whose
// upstream is configured but which the clone's fetch refspec cannot resolve.
// Each affected branch is its own row with its own repair, plus one batch
// action for all of them — the TUI only offers the batch, and a per-branch fix
// matters here because the browser lists them.
func narrowRefspecNotice(_ context.Context, _ *Server, _ *domain.Service, h model.RepoHealth, dismissed map[string]bool) []noticeWire {
	if len(h.UnmappedBranches) == 0 {
		return nil
	}
	title := "1 branch isn't tracked by the fetch refspec"
	if len(h.UnmappedBranches) != 1 {
		title = fmt.Sprintf("%d branches aren't tracked by the fetch refspec", len(h.UnmappedBranches))
	}
	actions := []noticeAction{{
		Op:    opAddFetchMappings,
		Label: "add mappings + fetch",
	}}
	for _, b := range h.UnmappedBranches {
		actions = append(actions, noticeAction{Op: opAddFetchMappings, Label: "fix " + b, Branch: b})
	}
	// The escape hatch for the fork engine.Push raises after every push of an
	// unmapped branch. Offered only while it still fires, and never as the way
	// to make the problem disappear — the finding and its repair stay right
	// here afterwards.
	if !dismissed[noticePushMapOffer] {
		actions = append(actions, noticeAction{
			Label:   "stop asking after each push",
			Dismiss: noticePushMapOffer,
		})
	}
	return []noticeWire{{
		ID:    noticeNarrowRefspec,
		Title: title,
		Detail: []string{
			"This clone's fetch refspec doesn't map these branches, so a push never moves their remote-tracking ref — the ↓↑ tip markers and ahead/behind cannot follow them.",
			"gg can add a per-branch mapping and fetch just those branches (no mass download).",
		},
		Items:   h.UnmappedBranches,
		Actions: actions,
	}}
}

// The repo key an auto-answer policy needs, cached so the policy never asks git
// for it.
//
// svc.GitCommonDir takes a READ reservation on the repo gate. A decider runs
// inside an operation that already holds a write one, so asking there blocks
// until the operation finishes — and the operation is blocked waiting for the
// answer. That is not theoretical: it hung a push until the decide timeout, and
// the browser's decide POST raced ahead of the parked fork and 409'd.
//
// So the key is remembered by the handlers that resolve it anyway, none of
// which run under an operation, and the policy reads only the cache. One slot,
// keyed by the service pointer: a re-root swaps the service and the stale entry
// with it. An unknown key declines to claim the decision, so the modal opens
// exactly as it does today — fail-open.
var (
	repoKeyMu  sync.Mutex
	repoKeySvc *domain.Service
	repoKeyVal string
)

func rememberRepoKey(svc *domain.Service, key string) {
	if svc == nil || key == "" {
		return
	}
	repoKeyMu.Lock()
	repoKeySvc, repoKeyVal = svc, key
	repoKeyMu.Unlock()
}

func cachedRepoKey(svc *domain.Service) string {
	repoKeyMu.Lock()
	defer repoKeyMu.Unlock()
	if svc != repoKeySvc {
		return ""
	}
	return repoKeyVal
}

// skipSuppressedFetchMapping answers engine.Push's post-push "add a tracking
// mapping?" fork with "skip" once the user has turned that question off for
// this repo. Without it the fork is unanswerable-forever: it fires on every
// push of every unmapped branch, and the browser has nowhere of its own to
// remember an answer (a random port means a fresh localStorage every run).
//
// The only work here is a small TOML read, once per fork.
func skipSuppressedFetchMapping(s *Server, req engine.DecisionRequest) (string, bool) {
	if req.ID != engine.FetchMappingDecisionID {
		return "", false
	}
	key := cachedRepoKey(s.service())
	if key == "" {
		return "", false
	}
	store := s.promptStore()
	if store == nil {
		return "", false
	}
	if !store.DismissedNotices(key)[noticePushMapOffer] {
		return "", false
	}
	return "skip", true
}

// buildAddFetchMappings is the refspec repair. The branch names are resolved
// SERVER-SIDE against a fresh health read and never taken from the wire beyond
// a membership check: they flow into a config write and into `git fetch` argv,
// so a client-named branch must be one gg itself just reported as affected.
// An omitted branch is the batch action — every affected branch at once.
func buildAddFetchMappings(s *Server, r *http.Request, req opStartRequest) (engine.Operation, func(), int, error) {
	svc := s.service()
	h, err := svc.RepoHealth(r.Context())
	if err != nil {
		return nil, nil, http.StatusInternalServerError, err
	}
	if len(h.UnmappedBranches) == 0 {
		return nil, nil, http.StatusUnprocessableEntity, errors.New("no branches need a fetch mapping")
	}
	branches := h.UnmappedBranches
	if req.Branch != "" {
		found := false
		for _, b := range h.UnmappedBranches {
			if b == req.Branch {
				found = true
				break
			}
		}
		if !found {
			return nil, nil, http.StatusUnprocessableEntity, errors.New("branch does not need a fetch mapping")
		}
		branches = []string{req.Branch}
	}
	// Remote "origin" matches the detection: unmappedFromConfig only ever
	// reports branches configured to track origin, precisely so this repair
	// cannot forge a mapping the fetch could not satisfy.
	return engine.AddFetchMappings{Remote: "origin", Branches: branches}, nil, 0, nil
}
