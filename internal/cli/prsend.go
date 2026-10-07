package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

// multiFlag is a repeatable string flag (--note a --note b).
type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

// prNumber parses "7" or "#7".
func prNumber(s string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(s), "#"))
	return n, err == nil && n > 0
}

func prSend(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	usage := func(msg string) int {
		if msg != "" {
			fmt.Fprintln(stderr, "error:", msg)
		}
		fmt.Fprintln(stderr, prUsage)
		return 2
	}
	if len(args) == 0 {
		return usage("")
	}
	n, ok := prNumber(args[0])
	if !ok {
		return usage(fmt.Sprintf("%q is not a pull request number", args[0]))
	}
	fs := flag.NewFlagSet("pr send", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var notes multiFlag
	fs.Var(&notes, "note", "a local note, draft reply or review remark id (repeatable)")
	review := fs.String("review", "", "send a stored review: every unsent remark, its summary as the body")
	mine := fs.Bool("mine", false, "send every local note the PR shows as one review")
	verdict := fs.Bool("verdict", false, "a verdict with no comments")
	event := fs.String("event", "", "comment, approve or request-changes (with --yes)")
	body := fs.String("body", "", "the review body")
	yes := fs.Bool("yes", false, "answer the confirm (never inside a gg session)")
	finish := fs.Bool("finish", false, "submit the review an interrupted send left pending")
	discard := fs.Bool("discard", false, "delete the review an interrupted send left pending")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
		return usage("")
	}
	kinds := 0
	for _, on := range []bool{len(notes) > 0, *review != "", *mine, *verdict, *finish, *discard} {
		if on {
			kinds++
		}
	}
	if kinds != 1 {
		return usage("name exactly one of --note, --review, --mine, --verdict, --finish, --discard")
	}
	switch *event {
	case "", engine.OptComment, engine.OptApprove, engine.OptRequestChanges:
	default:
		return usage(fmt.Sprintf("--event %q: want comment, approve or request-changes", *event))
	}
	req := domain.PRSendRequest{PR: n, Review: *review, Mine: *mine, Notes: notes, Verdict: *verdict,
		Body: *body, Finish: *finish, Discard: *discard}
	return runPRSend(context.Background(), svc, req, *yes, defaultAnswer(req, *event), stdin, stdout, stderr)
}

// runPRSend sends (or, inside a gg session, queues for the user's
// approval) and prints the outcome.
func runPRSend(ctx context.Context, svc *domain.Service, req domain.PRSendRequest, yes bool, answer string,
	stdin io.Reader, stdout, stderr io.Writer) int {
	if inGGSession() {
		if yes {
			fmt.Fprintln(stderr, "--yes is ignored inside a gg session: the user approves sends in gg")
		}
		return queueAndWait(ctx, svc, req, stdout, stderr)
	}
	res, err := sendNow(ctx, svc, req, yes, answer, stdin, stderr)
	if res.Summary != "" {
		fmt.Fprintln(stdout, res.Summary)
	}
	if err != nil {
		if errors.Is(err, engine.ErrDecisionRequired) || strings.Contains(err.Error(), "needs a decision") {
			fmt.Fprintln(stderr, "(rerun with --yes to send without being asked)")
		}
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

// sendNow fetches a moved head, then runs the op. yes answers the confirm
// with answer; otherwise a terminal is asked and a pipe gets the decision
// error.
func sendNow(ctx context.Context, svc *domain.Service, req domain.PRSendRequest, yes bool, answer string,
	stdin io.Reader, stderr io.Writer) (engine.Result, error) {
	if st := svc.ForgeStatus(ctx); !st.Available() {
		return engine.Result{}, fmt.Errorf("gg pr: %w", st.Err)
	}
	if rv, err := svc.PRRevalidate(ctx, req.PR); err == nil && rv.Moved {
		fmt.Fprintf(stderr, "#%d has new commits: fetching them first\n", req.PR)
		fop, err := svc.PRFetchOp(ctx, req.PR)
		if err == nil {
			_, err = runOperation(ctx, svc, fop, cliDecider{}, stderr)
		}
		if err != nil {
			return engine.Result{}, err
		}
	}
	op, err := svc.PRSendOp(ctx, req)
	if err != nil {
		return engine.Result{}, err
	}
	// Asked only when the reader handed in IS the terminal (a test's or a
	// script's reader never is, whatever os.Stdin happens to be).
	dec := cliDecider{in: stdin, out: stderr, interactive: stdin == io.Reader(os.Stdin) && stdinIsTerminal()}
	if yes {
		dec.policy = map[string]string{engine.DecisionSendForge: answer}
	}
	return runOperation(ctx, svc, op, dec, stderr)
}

func prReply(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pr reply", flag.ContinueOnError)
	fs.SetOutput(stderr)
	send := fs.Bool("send", false, "send the reply at once")
	yes := fs.Bool("yes", false, "answer the confirm")
	source := fs.String("source", "user", "user or agent")
	pos, flags := splitPositionals(args, 3, "source")
	if err := fs.Parse(flags); err != nil || len(pos) != 3 {
		fmt.Fprintln(stderr, prUsage)
		return 2
	}
	n, ok := prNumber(pos[0])
	if !ok {
		fmt.Fprintln(stderr, prUsage)
		return 2
	}
	ctx := context.Background()
	root, thread, err := svc.PRThreadRoot(ctx, n, pos[1])
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	src, err := noteSourceValue(*source)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 2
	}
	summary, rationale, _ := strings.Cut(strings.TrimSpace(pos[2]), "\n")
	d, err := svc.NoteReply(ctx, "forge:"+root, noteFromText(src, summary, rationale))
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	fmt.Fprintf(stdout, "%s draft reply to %s\n", d.ID, thread)
	if !*send {
		return 0
	}
	return runPRSend(ctx, svc, domain.PRSendRequest{PR: n, Notes: []string{d.ID}}, *yes, engine.OptSend, stdin, stdout, stderr)
}

func prResolve(svc *domain.Service, resolve bool, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) != 2 {
		fmt.Fprintln(stderr, prUsage)
		return 2
	}
	n, ok := prNumber(args[0])
	if !ok {
		fmt.Fprintln(stderr, prUsage)
		return 2
	}
	req := domain.PRSendRequest{PR: n}
	if resolve {
		req.Resolve = []string{args[1]}
	} else {
		req.Unresolve = []string{args[1]}
	}
	return runPRSend(context.Background(), svc, req, false, engine.OptSend, stdin, stdout, stderr)
}

func prNotes(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pr notes", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "emit the wire notes as a JSON array")
	pos, flags := splitPositionals(args, 1)
	if err := fs.Parse(flags); err != nil || len(pos) != 1 {
		fmt.Fprintln(stderr, prUsage)
		return 2
	}
	n, ok := prNumber(pos[0])
	if !ok {
		fmt.Fprintln(stderr, prUsage)
		return 2
	}
	byPath, err := svc.PRNotes(context.Background(), n)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	var wires []domain.WireNote
	for _, p := range domain.PreviewNotePaths(byPath) {
		for _, r := range byPath[p] {
			if *asJSON {
				wires = append(wires, domain.ToWireNotePreview(r, true))
				continue
			}
			renderNoteLine(stdout, r, false, noteStatusWord(r, true))
			for _, rep := range r.Replies {
				renderNoteLine(stdout, rep, true, noteStatusWord(rep, true))
			}
		}
	}
	if *asJSON {
		if wires == nil {
			wires = []domain.WireNote{}
		}
		return jsonOut(stdout, stderr, wires)
	}
	return 0
}

// splitPositionals pulls the first k non-flag arguments (in order) out of
// args and returns the rest as flags, so a verb's operands may sit before
// or after its flags. A flag that takes a value keeps it (the next arg).
func splitPositionals(args []string, k int, valued ...string) (pos, flags []string) {
	takes := map[string]bool{}
	for _, v := range valued {
		takes[v] = true
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case strings.HasPrefix(a, "-") && len(a) > 1:
			flags = append(flags, a)
			name, _, hasVal := strings.Cut(strings.TrimLeft(a, "-"), "=")
			if takes[name] && !hasVal && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		case len(pos) < k:
			pos = append(pos, a)
		default:
			flags = append(flags, a)
		}
	}
	return pos, flags
}

// noteFromText is a typed draft reply: its first line the summary, the
// rest the rationale.
func noteFromText(src model.NoteSource, summary, rationale string) model.Note {
	return model.Note{Source: src, Author: noteAuthorDefault(""), Summary: strings.TrimSpace(summary),
		Rationale: strings.TrimSpace(rationale)}
}

// jsonOut prints v as indented JSON.
func jsonOut(stdout, stderr io.Writer, v any) int {
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}
