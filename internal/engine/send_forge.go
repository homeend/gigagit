package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/forge"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/repogate"
)

// SendKind is what one SendItem does on the forge.
type SendKind int

const (
	SendThread    SendKind = iota // a new thread (line or file level)
	SendReply                     // a reply on an existing thread
	SendResolve                   // resolve an existing thread
	SendUnresolve                 // unresolve an existing thread
)

// SendReplyBody is one local reply posted into a thread the send creates.
type SendReplyBody struct{ Key, Body string }

// SendItem is one thing a SendToForge posts.
type SendItem struct {
	Key      string // the local item ("" = none, e.g. a resolve)
	Label    string // one confirm line: "a.go:12 rename this"
	Kind     SendKind
	Thread   forge.Thread    // SendThread
	Replies  []SendReplyBody // SendThread: local replies, posted into the new thread in order
	Resolve  bool            // SendThread: resolve once submitted
	ThreadID string          // SendReply / SendResolve / SendUnresolve
	Body     string          // SendReply
	Summary  string          // the note's summary, cut to 60 runes ("" for a resolve)
}

// SendSkip is an item the plan could not send, and why. Path/Line/Summary
// name it for a frontend that renders its own text (the TUI); Label is the
// CLI's English line; Reason is a code (the domain's Skip* constants).
type SendSkip struct {
	Label, Reason string
	Path          string
	Line          int
	Summary       string
}

// BodyText is the review body as the user reads it: the send marker dropped.
func (p SendPlan) BodyText() string { return strings.TrimSpace(forge.StripSendMarker(p.Body)) }

// SendMode is what a SendToForge does with its plan.
type SendMode int

const (
	SendReview  SendMode = iota // one review: threads + a verdict, all or nothing
	SendActions                 // replies / resolves, one by one
	SendFinish                  // submit the pending review an interrupted send left
	SendDiscard                 // delete the pending review an interrupted send left
)

// SendPlan is everything one send posts, resolved by domain before the op
// runs (outside the repo gate).
type SendPlan struct {
	Target  string // "owner/repo #7"
	PR      int
	PRID    string // the forge's node id
	Head    string // the head the threads anchor on
	Mode    SendMode
	Verdict bool   // SendReview: the user picks comment/approve/request-changes (false: send/abort, COMMENT)
	Key     string // SendReview of a whole stored review: the review note (stamped too)
	Body    string // the review body (SendReview, SendFinish)
	OwnPR   bool   // the viewer opened the PR: no approve / request-changes
	Pending string // the viewer's pending review on the forge ("" = none)
	Items   []SendItem
	Skipped []SendSkip
}

// SendLedger is domain's record of a send: stamps on the local items while
// they travel, failures, and the settle pass that deletes what the forge
// now has. Settle must not take the repo gate (the op holds a reservation).
type SendLedger interface {
	Stamp(ctx context.Context, key string, s model.NoteSend) error // ◌: ids known so far
	// Fail marks keys failed (○!). review is the pending review that may
	// still hold them on the forge (a delete that failed too), "" when none.
	Fail(ctx context.Context, keys []string, review string, err error)
	Settle(ctx context.Context) error // re-read the PR, settle every stamp
}

// SendToForge is the one op that writes to a forge (spec 2026-10-07 §3.3):
// one confirm, then the writes in a fixed order. Plan is a value computed
// by domain right before the op runs (every domain read reserves the gate,
// which is not re-entrant).
type SendToForge struct {
	Plan   SendPlan
	Writer forge.Writer
	Ledger SendLedger
	Now    func() time.Time // stamp clock; nil = time.Now (domain passes its clock)
}

const DecisionSendForge = "forge.send"

const (
	OptComment           = "comment"
	OptApprove           = "approve"
	OptRequestChanges    = "request-changes"
	OptSend              = "send"
	OptSubmitWithPending = "submit-with-pending"
	OptDiscard           = "discard"
)

// ErrNothingToSend: every item was skipped (or none was given).
var ErrNothingToSend = errors.New("nothing to send: every item was skipped")

var _ Operation = SendToForge{}

// LockMode: the op touches no git state; the note store has its own lock.
func (op SendToForge) LockMode() repogate.Mode { return repogate.Read }

func (op SendToForge) now() time.Time {
	if op.Now != nil {
		return op.Now()
	}
	return time.Now()
}

func (op SendToForge) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Writer == nil || op.Ledger == nil {
		return Result{}, fmt.Errorf("send to forge: Writer and Ledger are required")
	}
	p := op.Plan
	switch p.Mode {
	case SendFinish:
		return op.finish(ctx, deps, p)
	case SendDiscard:
		return op.discard(ctx, deps, p)
	case SendActions:
		return op.actions(ctx, deps, p)
	}
	return op.review(ctx, deps, p)
}

// confirm asks once and checks the answer is one of the options.
func confirm(ctx context.Context, deps OpDeps, p SendPlan, options []string) (string, error) {
	req := PromptReq(DecisionSendForge, "Send to %s:\n%s", options, p.Target, describePlan(p))
	resp, err := deps.decide(ctx, req)
	if err != nil {
		return "", err
	}
	if !slices.Contains(options, resp.Option) {
		return "", fmt.Errorf("%s: %q is not one of %s", DecisionSendForge, resp.Option, strings.Join(options, ", "))
	}
	return resp.Option, nil
}

// describePlan is the confirm's body: the review body, every item and every
// skipped item with its reason — exactly what will be posted.
func describePlan(p SendPlan) string {
	var b strings.Builder
	if strings.TrimSpace(p.Body) != "" {
		fmt.Fprintf(&b, "review body:\n%s\n", indent(forge.StripSendMarker(p.Body)))
	}
	for _, it := range p.Items {
		fmt.Fprintf(&b, "  + %s\n", it.Label)
		for range it.Replies {
			fmt.Fprintf(&b, "      + reply\n")
		}
	}
	for _, s := range p.Skipped {
		fmt.Fprintf(&b, "  - %s (skipped: %s)\n", s.Label, s.Reason)
	}
	return strings.TrimRight(b.String(), "\n")
}

func indent(s string) string { return "    " + strings.ReplaceAll(s, "\n", "\n    ") }

func (op SendToForge) review(ctx context.Context, deps OpDeps, p SendPlan) (Result, error) {
	if len(p.Items) == 0 && !p.Verdict {
		return Result{}, ErrNothingToSend
	}
	options := []string{OptSend, "abort"}
	if p.Verdict {
		options = []string{OptComment, OptApprove, OptRequestChanges, "abort"}
		if p.OwnPR {
			options = []string{OptComment, "abort"}
		}
	}
	if p.Pending != "" {
		options = []string{OptSubmitWithPending, "abort"}
	}
	choice, err := confirm(ctx, deps, p, options)
	if err != nil {
		return Result{}, err
	}
	if choice == "abort" {
		return Result{}.WithSummary("aborted: sending to %s", p.Target), nil
	}
	ev := forge.EventComment
	switch choice {
	case OptApprove:
		ev = forge.EventApprove
	case OptRequestChanges:
		ev = forge.EventRequestChanges
	}
	joined := p.Pending != ""
	review := p.Pending
	if !joined {
		if review, err = op.Writer.StartReview(ctx, p.PRID, p.Head); err != nil {
			op.Ledger.Fail(ctx, planKeys(p), "", err)
			return Result{}, err
		}
	}
	stamp := func(key string, s model.NoteSend) {
		if key != "" {
			s.PR, s.Review, s.At, s.Joined = p.PR, review, op.now(), joined
			_ = op.Ledger.Stamp(ctx, key, s)
		}
	}
	stamp(p.Key, model.NoteSend{})
	for _, it := range p.Items {
		stamp(it.Key, model.NoteSend{})
		for _, r := range it.Replies {
			stamp(r.Key, model.NoteSend{})
		}
	}
	fail := func(err error) (Result, error) {
		if joined {
			// The user's own pending review: never deleted. The stamps stay, so
			// `--finish` (or the browser) can submit what gg added.
			return Result{}, fmt.Errorf("%w (your pending review on GitHub now holds gg's comments: finish or discard it)", err)
		}
		left := "" // the pending review is gone: the items are simply local again
		if derr := op.Writer.DeletePendingReview(ctx, review); derr != nil {
			err = errors.Join(err, fmt.Errorf("deleting the pending review: %w", derr))
			// It may still hold gg's threads: keep its id on the failed
			// items so --finish / --discard can reach it, never a re-send.
			left = review
		}
		op.Ledger.Fail(ctx, planKeys(p), left, err)
		return Result{}, err
	}
	threads := map[string]string{} // item key → thread id
	for _, it := range p.Items {
		deps.emit(ctx, Progress{Step: "sending", Detail: it.Label})
		ref, err := op.Writer.AddThread(ctx, review, it.Thread)
		if err != nil {
			return fail(err)
		}
		threads[it.Key] = ref.ID
		stamp(it.Key, model.NoteSend{Thread: ref.ID, Comment: ref.CommentID, URL: ref.URL})
		for _, r := range it.Replies {
			c, err := op.Writer.Reply(ctx, review, ref.ID, r.Body)
			if err != nil {
				return fail(err)
			}
			stamp(r.Key, model.NoteSend{Thread: ref.ID, Comment: c.ID, URL: c.URL})
		}
	}
	if err := op.submit(ctx, review, ev, p.Body, len(p.Items)); err != nil {
		return fail(err)
	}
	res := Result{Changed: true}.WithSummary("sent %d comments to %s", len(p.Items), p.Target)
	if err := op.Ledger.Settle(ctx); err != nil {
		res = res.AppendSummary("; local copies are removed at the next refresh (%v)", err)
	}
	var unresolved int
	for _, it := range p.Items {
		if it.Resolve {
			if err := op.Writer.Resolve(ctx, threads[it.Key]); err != nil {
				unresolved++
			}
		}
	}
	if unresolved > 0 {
		res = res.AppendSummary("; %d threads could not be resolved", unresolved)
	}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

// submit submits, retrying ONCE with a count body when the forge refuses a
// blank one (whether GraphQL does is unverified — spec §3.4).
func (op SendToForge) submit(ctx context.Context, review string, ev forge.Event, body string, n int) error {
	err := op.Writer.SubmitReview(ctx, review, ev, body)
	if err == nil || strings.TrimSpace(body) != "" || !forge.IsBlankBodyError(err) {
		return err
	}
	fallback := "1 comment"
	if n != 1 {
		fallback = fmt.Sprintf("%d comments", n)
	}
	return op.Writer.SubmitReview(ctx, review, ev, fallback)
}

func planKeys(p SendPlan) []string {
	var keys []string
	if p.Key != "" {
		keys = append(keys, p.Key)
	}
	for _, it := range p.Items {
		if it.Key != "" {
			keys = append(keys, it.Key)
		}
		for _, r := range it.Replies {
			keys = append(keys, r.Key)
		}
	}
	return keys
}

func (op SendToForge) actions(ctx context.Context, deps OpDeps, p SendPlan) (Result, error) {
	if len(p.Items) == 0 {
		return Result{}, ErrNothingToSend
	}
	needsConfirm := slices.ContainsFunc(p.Items, func(it SendItem) bool { return it.Kind == SendReply })
	if needsConfirm { // resolve / unresolve alone are immediate (spec §3.5)
		choice, err := confirm(ctx, deps, p, []string{OptSend, "abort"})
		if err != nil {
			return Result{}, err
		}
		if choice == "abort" {
			return Result{}.WithSummary("aborted: sending to %s", p.Target), nil
		}
	}
	var failed int
	var firstErr error
	for _, it := range p.Items {
		var err error
		switch it.Kind {
		case SendReply:
			if it.Key != "" {
				_ = op.Ledger.Stamp(ctx, it.Key, model.NoteSend{PR: p.PR, Thread: it.ThreadID, At: op.now()})
			}
			var c forge.CommentRef
			if c, err = op.Writer.Reply(ctx, "", it.ThreadID, it.Body); err == nil && it.Key != "" {
				_ = op.Ledger.Stamp(ctx, it.Key, model.NoteSend{PR: p.PR, Thread: it.ThreadID, Comment: c.ID, URL: c.URL, At: op.now()})
			}
		case SendResolve:
			err = op.Writer.Resolve(ctx, it.ThreadID)
		case SendUnresolve:
			err = op.Writer.Unresolve(ctx, it.ThreadID)
		}
		if err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
			if it.Key != "" {
				op.Ledger.Fail(ctx, []string{it.Key}, "", err)
			}
		}
	}
	settleErr := op.Ledger.Settle(ctx)
	if failed > 0 {
		return Result{Changed: failed < len(p.Items)}, fmt.Errorf("%d of %d failed: %w", failed, len(p.Items), firstErr)
	}
	res := Result{Changed: true}.WithSummary("sent %d actions to %s", len(p.Items), p.Target)
	if settleErr != nil {
		res = res.AppendSummary("; local copies are removed at the next refresh (%v)", settleErr)
	}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

func (op SendToForge) finish(ctx context.Context, deps OpDeps, p SendPlan) (Result, error) {
	choice, err := confirm(ctx, deps, p, []string{OptSend, "abort"})
	if err != nil {
		return Result{}, err
	}
	if choice == "abort" {
		return Result{}.WithSummary("aborted: sending to %s", p.Target), nil
	}
	if err := op.submit(ctx, p.Pending, forge.EventComment, p.Body, len(p.Items)); err != nil {
		return Result{}, err
	}
	res := Result{Changed: true}.WithSummary("submitted the pending review on %s", p.Target)
	if err := op.Ledger.Settle(ctx); err != nil {
		res = res.AppendSummary("; local copies are removed at the next refresh (%v)", err)
	}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

func (op SendToForge) discard(ctx context.Context, deps OpDeps, p SendPlan) (Result, error) {
	choice, err := confirm(ctx, deps, p, []string{OptDiscard, "abort"})
	if err != nil {
		return Result{}, err
	}
	if choice == "abort" {
		return Result{}.WithSummary("aborted: sending to %s", p.Target), nil
	}
	if err := op.Writer.DeletePendingReview(ctx, p.Pending); err != nil {
		return Result{}, err
	}
	res := Result{Changed: true}.WithSummary("discarded the pending review on %s", p.Target)
	_ = op.Ledger.Settle(ctx) // the stamps naming it are cleared: the items are local again
	deps.emit(ctx, Done{Result: res})
	return res, nil
}
