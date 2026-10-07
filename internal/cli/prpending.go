package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/clock"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

var pendingWaitTimeout = domain.DefaultPendingWait

func inGGSession() bool { return sessionGetenv("GG_INBOX") != "" }

// queueAndWait is an agent's send: a pending entry the user approves in gg,
// and a long-poll on its outcome.
func queueAndWait(ctx context.Context, svc *domain.Service, req domain.PRSendRequest, stdout, stderr io.Writer) int {
	who := strings.TrimSpace(os.Getenv("GG_AGENT"))
	if who == "" {
		who = "agent"
	}
	req.Agent = who
	e, err := svc.PendingSendAdd(ctx, req, who)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	fmt.Fprintf(stdout, "queued %s: waiting for the user's approval (gg pr pending approve %s)\n", e.ID, e.ID)
	return waitOutcome(ctx, svc, e.ID, stdout, stderr)
}

func waitOutcome(ctx context.Context, svc *domain.Service, id string, stdout, stderr io.Writer) int {
	e, err := svc.PendingSendWait(ctx, id, pendingWaitTimeout)
	switch {
	case errors.Is(err, domain.ErrPendingStillWaiting):
		fmt.Fprintf(stdout, "still pending: %s (gg pr pending wait %s)\n", id, id)
		return 3
	case err != nil:
		fmt.Fprintln(stderr, "error:", err)
		return 1
	case e.State == domain.PendingSent:
		fmt.Fprintln(stdout, e.Outcome)
		return 0
	}
	fmt.Fprintf(stderr, "%s: %s\n", e.State, e.Outcome)
	return 1
}

func prPending(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	ctx := context.Background()
	verb := "list"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		verb, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("pr pending", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "JSON rows (list)")
	yes := fs.Bool("yes", false, "answer the confirm (approve)")
	pos, flags := splitPositionals(args, 1)
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	needID := verb != "list"
	if needID != (len(pos) == 1) {
		fmt.Fprintln(stderr, prUsage)
		return 2
	}
	switch verb {
	case "list":
		all, err := svc.PendingSends(ctx)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		if *asJSON {
			if all == nil {
				all = []domain.PendingSend{}
			}
			return jsonOut(stdout, stderr, all)
		}
		if len(all) == 0 {
			fmt.Fprintln(stdout, "(no pending sends)")
		}
		for _, e := range all {
			fmt.Fprintf(stdout, "%s  #%d  %-9s %s  %s  %s ago\n", e.ID, e.Request.PR, e.State, e.Requester,
				describeRequest(e.Request), clock.Now().Sub(e.Created).Round(time.Second))
		}
		return 0
	case "wait":
		return waitOutcome(ctx, svc, pos[0], stdout, stderr)
	case "cancel":
		if _, err := svc.PendingSendFinish(ctx, pos[0], domain.PendingCancelled, "cancelled by the agent"); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		return 0
	case "approve", "reject":
		if inGGSession() {
			fmt.Fprintln(stderr, "error: approve pending sends in your own terminal or in gg")
			return 1
		}
		e, err := svc.PendingSendGet(ctx, pos[0])
		if err == nil && e.State != domain.PendingWaiting {
			err = domain.ErrPendingSendClosed
		}
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		if verb == "reject" {
			_, err := svc.PendingSendFinish(ctx, e.ID, domain.PendingRejected, "rejected by the user")
			if err != nil {
				fmt.Fprintln(stderr, "error:", err)
				return 1
			}
			return 0
		}
		return approvePending(ctx, svc, e, *yes, stdin, stdout, stderr)
	}
	fmt.Fprintln(stderr, prUsage)
	return 2
}

// approvePending runs a queued request with the user's own confirm and
// records its outcome for the agent waiting on it.
func approvePending(ctx context.Context, svc *domain.Service, e domain.PendingSend, yes bool,
	stdin io.Reader, stdout, stderr io.Writer) int {
	if ev := e.Request.Event; ev != "" && !yes {
		fmt.Fprintf(stderr, "%s asks: %s\n", e.Requester, ev)
	}
	res, err := sendNow(ctx, svc, e.Request, yes, defaultAnswer(e.Request), stdin, stderr)
	state, outcome, waiting := domain.PendingOutcome(res, err)
	if waiting || errors.Is(err, errJoinNeedsConfirm) {
		// The approver could not answer here: nothing reached GitHub, and the
		// agent's request is still good.
		fmt.Fprintln(stderr, "error:", err)
		fmt.Fprintf(stderr, "still pending: %s (approve it in a terminal, or in gg)\n", e.ID)
		return 1
	}
	if _, ferr := svc.PendingSendFinish(ctx, e.ID, state, outcome); ferr != nil {
		fmt.Fprintln(stderr, "error:", ferr)
	}
	if res.Summary != "" {
		fmt.Fprintln(stdout, res.Summary)
	}
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

// describeRequest is a queued request in a few words (the pending list).
func describeRequest(r domain.PRSendRequest) string {
	if r.Event != "" {
		return describeWhat(r) + " (asks: " + r.Event + ")"
	}
	return describeWhat(r)
}

func describeWhat(r domain.PRSendRequest) string {
	switch {
	case r.Review != "":
		return "review " + r.Review
	case r.Mine:
		return "my draft review"
	case r.Finish:
		return "finish the pending review"
	case r.Discard:
		return "discard the pending review"
	case len(r.Resolve) > 0:
		return "resolve " + strings.Join(r.Resolve, ", ")
	case len(r.Unresolve) > 0:
		return "unresolve " + strings.Join(r.Unresolve, ", ")
	case len(r.Notes) == 1:
		return "1 note"
	case len(r.Notes) > 1:
		return fmt.Sprintf("%d notes", len(r.Notes))
	}
	return "verdict"
}

// defaultAnswer is what --yes answers the confirm with: the asked event for
// a review with a verdict (comment when none), discard for --discard, send
// otherwise.
func defaultAnswer(req domain.PRSendRequest) string {
	switch {
	case req.Review != "" || req.Mine || req.Verdict:
		if req.Event != "" {
			return req.Event
		}
		return engine.OptComment
	case req.Discard:
		return engine.OptDiscard
	}
	return engine.OptSend
}
