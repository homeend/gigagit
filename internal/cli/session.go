package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// steerReplyWaitForTest bounds the wait for a TUI's answer. A variable rather
// than a const so a test can shorten it; production never changes it.
var steerReplyWaitForTest = 2 * time.Second

// steerHTTPTimeout bounds the POST to an open gg web page. The endpoint answers
// 202 immediately, so anything slower is a page that is gone.
const steerHTTPTimeout = 2 * time.Second

// steerErrorBodyMax caps how much of an error response is echoed back. The
// endpoint's refusals are one short English line; anything longer is noise.
const steerErrorBodyMax = 4 << 10

// cmdSession is `gg session`: post a steering command to whatever gg session is
// showing this worktree.
func cmdSession(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	dir, err := sessionInboxDir(svc)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return runSession(dir, svc, args, stdout, stderr)
}

// sessionInboxDir resolves this worktree's inbox. "" (no state home) is not an
// error: it simply means nothing can be live, and the verbs then report so.
func sessionInboxDir(svc *domain.Service) (string, error) {
	ctx := context.Background()
	cd, err := svc.GitCommonDir(ctx)
	if err != nil {
		return "", err
	}
	top, err := svc.TopLevel(ctx)
	if err != nil {
		return "", err
	}
	return config.SessionSteerDir(cd, top), nil
}

// runSession dispatches one session subcommand against an explicit inbox dir.
// The dir is a parameter so tests can point it at t.TempDir() and stay parallel.
func runSession(dir string, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: gg session <status|navigate|reload|focus|highlight> [flags]")
		return 2
	}
	switch args[0] {
	case "status":
		return sessionStatus(dir, svc, args[1:], stdout, stderr)
	case "navigate":
		return sessionNavigate(dir, svc, args[1:], stdout, stderr)
	case "reload":
		return sessionReload(dir, args[1:], stdout, stderr)
	case "focus":
		return sessionFocus(dir, args[1:], stdout, stderr)
	case "highlight":
		return sessionHighlight(dir, svc, args[1:], stdout, stderr)
	}
	fmt.Fprintf(stderr, "session: unknown subcommand %q\n", args[0])
	return 2
}

// sessionRoute is who is live for this worktree right now.
type sessionRoute struct {
	tui, web     steer.Presence
	tuiOK, webOK bool
}

func routeFor(dir string) sessionRoute {
	if dir == "" {
		return sessionRoute{}
	}
	var r sessionRoute
	r.tui, r.tuiOK = steer.Live(dir, steer.TUIPresence)
	r.web, r.webOK = steer.Live(dir, steer.WebPresence)
	return r
}

// sendSteer applies the routing table: no live session is exit 1, a live web
// page gets a POST, a live TUI gets the inbox file, and when both are live the
// TUI's reply decides the exit code.
func sendSteer(dir string, c steer.Command, noWait bool, stdout, stderr io.Writer) int {
	r := routeFor(dir)
	if !r.tuiOK && !r.webOK {
		fmt.Fprintln(stderr, "no gg session for this worktree")
		return 1
	}
	// One id for BOTH deliveries: Post honours a preset id, so a command that
	// goes to the page and the TUI alike is one command with one name, not two
	// that no reply could ever be correlated against.
	if c.ID == "" {
		c.ID = steer.NewID()
	}
	webFailed := false
	if r.webOK {
		if err := postWebSteer(r.web.URL, c); err != nil {
			fmt.Fprintln(stderr, "web:", err)
			webFailed = true
		} else {
			fmt.Fprintln(stdout, "web: sent")
		}
	}
	if !r.tuiOK {
		if webFailed {
			return 1
		}
		return 0
	}
	c.Wait = !noWait
	id, err := steer.Post(dir, c)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if noWait {
		fmt.Fprintln(stdout, id)
		return 0
	}
	rep, ok := steer.AwaitReply(dir, id, steerReplyWaitForTest)
	if !ok {
		// The command is still in the inbox and may yet be applied — a slow
		// answer is not a failure, and reporting one would make an agent retry
		// and move the window twice.
		fmt.Fprintln(stdout, "queued: no answer from the TUI within 2s")
		return 0
	}
	if rep.OK {
		if rep.Detail != "" {
			fmt.Fprintln(stdout, rep.Detail)
		}
		return 0
	}
	fmt.Fprintln(stderr, rep.Error)
	return 1
}

// postWebSteer hands the command to an open gg web page. Content-Type is JSON
// and no Origin header is sent, which the server's writeGuard accepts from a
// non-browser client. A 409 is the endpoint's "an operation is in flight"
// refusal and its own English line is echoed verbatim.
func postWebSteer(base string, c steer.Command) error {
	if base == "" {
		return errors.New("the gg web presence carries no URL")
	}
	body, err := json.Marshal(c)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(base, "/")+"/api/session/steer", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: steerHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusConflict {
		if msg := readSteerErrorBody(resp.Body); msg != "" {
			return errors.New(msg)
		}
		return errors.New("operation in flight")
	}
	if resp.StatusCode/100 != 2 {
		if msg := readSteerErrorBody(resp.Body); msg != "" {
			return fmt.Errorf("gg web answered %s: %s", resp.Status, msg)
		}
		return fmt.Errorf("gg web answered %s", resp.Status)
	}
	return nil
}

// readSteerErrorBody returns the endpoint's refusal line, capped and trimmed.
func readSteerErrorBody(r io.Reader) string {
	data, err := io.ReadAll(io.LimitReader(r, steerErrorBodyMax))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// targetOf maps a resolved note address onto the wire target. It is the CLI's
// half of the same allowlist the web's noteState enforces.
func targetOf(a model.FileAddress) *steer.Target {
	t := &steer.Target{State: "unstaged"}
	switch a.State {
	case model.StateStaged:
		t.State = "staged"
	case model.StateUntracked:
		t.State = "untracked"
	case model.StateCommitted:
		t.State, t.Commit = "commit", a.Commit
	}
	return t
}

// resolveHunkLine turns --hunk N into the {side,no} the consumer lands on: the
// FIRST line of the hunk's new span, or its old span for a pure deletion. (A
// NOTE anchors at a range END; a landing wants the top of the region.) Resolved
// HERE and never in the TUI, so one landing path serves --hunk, --new-line and
// --old-line alike.
func resolveHunkLine(ctx context.Context, svc *domain.Service, cached bool, rev, path string, n int) (steer.Line, error) {
	spec, err := svc.HunkDiffSpec(ctx, cached, rev, []string{path})
	if err != nil {
		return steer.Line{}, err
	}
	files, err := svc.DiffHunks(ctx, spec)
	if err != nil {
		return steer.Line{}, err
	}
	for _, f := range files {
		if f.Path != path && f.OldPath != path {
			continue
		}
		for _, h := range f.Hunks {
			if h.N != n {
				continue
			}
			if h.New[0] > 0 {
				return steer.Line{Side: "new", No: h.New[0]}, nil
			}
			if h.Old[0] > 0 {
				return steer.Line{Side: "old", No: h.Old[0]}, nil
			}
			return steer.Line{}, fmt.Errorf("hunk %d of %s has no lines", n, path)
		}
		return steer.Line{}, fmt.Errorf("%s has %d hunks", path, len(f.Hunks))
	}
	return steer.Line{}, fmt.Errorf("%s has no hunks in this diff", path)
}

// sessionStatus prints the routing for this worktree.
func sessionStatus(dir string, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print the routing as JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	r := routeFor(dir)
	view := sessionOpenView(svc)
	if *asJSON {
		out := map[string]any{"worktree": r.worktree(), "view": view}
		if r.tuiOK {
			out["tui"] = map[string]any{"pid": r.tui.PID, "started": r.tui.Started}
		} else {
			out["tui"] = nil
		}
		if r.webOK {
			out["web"] = map[string]any{"pid": r.web.PID, "url": r.web.URL}
		} else {
			out["web"] = nil
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		if !r.tuiOK && !r.webOK {
			return 1
		}
		return 0
	}
	if !r.tuiOK && !r.webOK {
		fmt.Fprintln(stderr, "no gg session for this worktree")
		return 1
	}
	fmt.Fprintln(stdout, "worktree:", r.worktree())
	if r.tuiOK {
		fmt.Fprintf(stdout, "tui: pid %d (since %s)\n", r.tui.PID, r.tui.Started)
	}
	if r.webOK {
		fmt.Fprintf(stdout, "web: pid %d at %s\n", r.web.PID, r.web.URL)
	}
	if view != "" {
		fmt.Fprintln(stdout, "view:", view)
	}
	return 0
}

// worktree is whichever live presence knows it (both record the same path).
func (r sessionRoute) worktree() string {
	if r.tuiOK && r.tui.Worktree != "" {
		return r.tui.Worktree
	}
	return r.web.Worktree
}

// sessionOpenView reads the phase-0 session snapshot for a one-line "what is on
// screen" summary. Best-effort: no snapshot, or an unreadable one, is "".
func sessionOpenView(svc *domain.Service) string {
	cd, err := svc.GitCommonDir(context.Background())
	if err != nil {
		return ""
	}
	return sessionOpenViewAt(config.SessionSnapshotPath(cd))
}

// sessionOpenViewAt is sessionOpenView against an explicit snapshot path, which
// is the test seam (the real path depends on the state home). The shape mirrors
// the TUI's snapWriter: cursor.commit is an OBJECT carrying the hash, so
// decoding it as a bare string would fail and blank every view line.
func sessionOpenViewAt(path string) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var snap struct {
		Focus struct {
			Panel string `json:"panel"`
		} `json:"focus"`
		Cursor struct {
			File   string `json:"file"`
			Commit struct {
				Hash string `json:"hash"`
			} `json:"commit"`
		} `json:"cursor"`
	}
	if json.Unmarshal(data, &snap) != nil {
		return ""
	}
	parts := make([]string, 0, 2)
	if snap.Focus.Panel != "" {
		parts = append(parts, snap.Focus.Panel)
	}
	switch {
	case snap.Cursor.File != "":
		parts = append(parts, snap.Cursor.File)
	case snap.Cursor.Commit.Hash != "":
		parts = append(parts, snap.Cursor.Commit.Hash)
	}
	return strings.Join(parts, " / ")
}

// sessionNavigate is the navigate verb: a file + a line, a commit to reveal, or
// a note step.
func sessionNavigate(dir string, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session navigate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tf := addTargetFlags(fs)
	hunk := fs.Int("hunk", 0, "land on the Nth @@ hunk of --file (`gg diff --hunks` numbers them)")
	newLine := fs.Int("new-line", 0, "land on this line of the new side")
	oldLine := fs.Int("old-line", 0, "land on this line of the old side")
	next := fs.Bool("next-comment", false, "step the open diff to the next note")
	prev := fs.Bool("prev-comment", false, "step the open diff to the previous note")
	noWait := fs.Bool("no-wait", false, "post the command and exit without waiting for an answer")
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 0 {
		fmt.Fprintf(stderr, "session navigate: unexpected argument %q (navigate takes only flags)\n", pos[0])
		return 2
	}

	if *next || *prev {
		if *next && *prev {
			fmt.Fprintln(stderr, "session navigate: --next-comment and --prev-comment are mutually exclusive")
			return 2
		}
		step := "next_note"
		if *prev {
			step = "prev_note"
		}
		return sendSteer(dir, steer.Command{Cmd: "navigate", Step: step}, *noWait, stdout, stderr)
	}

	// A bare --rev with no --file reveals the commit. NoteTarget cannot be used
	// here — it refuses a target with no path (ErrNoteTargetUsage, "a note needs
	// a file path") — and the raw rev must NOT be posted either: the consumer
	// compares hashes (commitIsHash), so "HEAD" would match nothing and every
	// such command would come back "commit not loaded in the feed". Resolve it
	// to a full sha up front, and refuse anything that does not resolve.
	if *tf.file == "" {
		if *tf.rev == "" {
			fmt.Fprintln(stderr, "session navigate: pass --file, --rev, or --next-comment/--prev-comment")
			return 2
		}
		full, found, err := svc.ResolveRev(context.Background(), *tf.rev) // peels annotated tags to the commit
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		if !found {
			fmt.Fprintf(stderr, "session navigate: %q did not resolve to a commit\n", *tf.rev)
			return 1
		}
		full = strings.TrimSpace(full)
		if len(full) != 40 {
			fmt.Fprintf(stderr, "session navigate: %q did not resolve to a commit\n", *tf.rev)
			return 1
		}
		return sendSteer(dir, steer.Command{Cmd: "navigate", Commit: full}, *noWait, stdout, stderr)
	}

	set := 0
	for _, n := range []int{*hunk, *newLine, *oldLine} {
		if n != 0 {
			set++
		}
	}
	if set != 1 {
		fmt.Fprintln(stderr, "session navigate: pass exactly one of --hunk, --new-line, --old-line")
		return 2
	}

	ctx := context.Background()
	addr, err := svc.NoteTarget(ctx, *tf.file, *tf.cached, *tf.rev)
	if err != nil {
		if errors.Is(err, domain.ErrNoteTargetUsage) {
			fmt.Fprintln(stderr, "session navigate:", err)
			return 2
		}
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}

	var line steer.Line
	switch {
	case *hunk != 0:
		if addr.State == model.StateUntracked {
			fmt.Fprintln(stderr, "session navigate: untracked files have no hunks; use --new-line")
			return 1
		}
		line, err = resolveHunkLine(ctx, svc, *tf.cached, *tf.rev, addr.Path, *hunk)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
	case *newLine != 0:
		line = steer.Line{Side: "new", No: *newLine}
	default:
		line = steer.Line{Side: "old", No: *oldLine}
	}

	return sendSteer(dir, steer.Command{
		Cmd:    "navigate",
		File:   addr.Path,
		Target: targetOf(addr),
		Line:   &line,
	}, *noWait, stdout, stderr)
}

// parseSteerFlags parses fs allowing flags to follow positional arguments. Go's
// flag package stops at the first non-flag token, so `gg session focus commits
// --no-wait` would otherwise leave --no-wait unparsed and silently wait two
// seconds for an answer the caller said it did not want.
func parseSteerFlags(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return pos, nil
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

// sessionReload re-reads one or more of the TUI's sources.
func sessionReload(dir string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session reload", flag.ContinueOnError)
	fs.SetOutput(stderr)
	noWait := fs.Bool("no-wait", false, "post the command and exit without waiting for an answer")
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	src := "notes"
	switch len(pos) {
	case 0:
	case 1:
		src = pos[0]
	default:
		fmt.Fprintln(stderr, "usage: gg session reload [notes|status|all] [--no-wait]")
		return 2
	}
	switch src {
	case "notes", "status", "all":
	default:
		fmt.Fprintf(stderr, "session reload: unknown source %q (notes, status or all)\n", src)
		return 2
	}
	return sendSteer(dir, steer.Command{Cmd: "reload", Sources: []string{src}}, *noWait, stdout, stderr)
}

// sessionFocus switches the TUI (or the web page) to a named panel.
func sessionFocus(dir string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session focus", flag.ContinueOnError)
	fs.SetOutput(stderr)
	noWait := fs.Bool("no-wait", false, "post the command and exit without waiting for an answer")
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 1 {
		fmt.Fprintln(stderr, "usage: gg session focus <files|staged|commits|branches|worktrees|remotes|tags|reflog|previews> [--no-wait]")
		return 2
	}
	return sendSteer(dir, steer.Command{Cmd: "focus", Panel: pos[0]}, *noWait, stdout, stderr)
}

// sessionHighlight adds or clears the attention bands an agent paints.
func sessionHighlight(dir string, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: gg session highlight <add|clear> [flags]")
		return 2
	}
	switch args[0] {
	case "add":
		return sessionHighlightAdd(dir, svc, args[1:], stdout, stderr)
	case "clear":
		return sessionHighlightClear(dir, svc, args[1:], stdout, stderr)
	}
	fmt.Fprintf(stderr, "session highlight: unknown subcommand %q (add or clear)\n", args[0])
	return 2
}

func sessionHighlightAdd(dir string, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session highlight add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tf := addTargetFlags(fs)
	start := fs.Int("start", 0, "first line of the band (1-based)")
	end := fs.Int("end", 0, "last line of the band; defaults to --start")
	side := fs.String("side", "new", "which side of the diff to paint: new or old")
	tone := fs.String("tone", "info", "band colour: info, warn or error")
	noWait := fs.Bool("no-wait", false, "post the command and exit without waiting for an answer")
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 0 {
		fmt.Fprintf(stderr, "session highlight add: unexpected argument %q\n", pos[0])
		return 2
	}
	if *tf.file == "" || *start < 1 {
		fmt.Fprintln(stderr, "session highlight add: --file and a 1-based --start are required")
		return 2
	}
	switch *side {
	case "new", "old":
	default:
		fmt.Fprintf(stderr, "session highlight add: unknown side %q (new or old)\n", *side)
		return 2
	}
	switch *tone {
	case "info", "warn", "error":
	default:
		fmt.Fprintf(stderr, "session highlight add: unknown tone %q (info, warn or error)\n", *tone)
		return 2
	}
	if *end != 0 && *end < *start {
		fmt.Fprintln(stderr, "session highlight add: --end is before --start")
		return 2
	}
	addr, err := svc.NoteTarget(context.Background(), *tf.file, *tf.cached, *tf.rev)
	if err != nil {
		if errors.Is(err, domain.ErrNoteTargetUsage) {
			fmt.Fprintln(stderr, "session highlight add:", err)
			return 2
		}
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return sendSteer(dir, steer.Command{
		Cmd: "highlight", File: addr.Path, Target: targetOf(addr),
		Side: *side, Start: *start, End: *end, Tone: *tone,
	}, *noWait, stdout, stderr)
}

func sessionHighlightClear(dir string, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session highlight clear", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tf := addTargetFlags(fs)
	noWait := fs.Bool("no-wait", false, "post the command and exit without waiting for an answer")
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 0 {
		fmt.Fprintf(stderr, "session highlight clear: unexpected argument %q\n", pos[0])
		return 2
	}
	c := steer.Command{Cmd: "highlight_clear"}
	if *tf.file != "" {
		addr, err := svc.NoteTarget(context.Background(), *tf.file, *tf.cached, *tf.rev)
		if err != nil {
			if errors.Is(err, domain.ErrNoteTargetUsage) {
				fmt.Fprintln(stderr, "session highlight clear:", err)
				return 2
			}
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		c.File, c.Target = addr.Path, targetOf(addr)
	}
	return sendSteer(dir, c, *noWait, stdout, stderr)
}
