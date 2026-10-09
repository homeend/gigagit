package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/linknav"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// steerReplyWaitForTest bounds the wait for a TUI's answer. A variable rather
// than a const so a test can shorten it; production never changes it.
var steerReplyWaitForTest = 2 * time.Second

// steerPRFetchWaitForTest bounds the wait for a navigate to a pull request
// this machine has not fetched: the TUI answers once its fetch is done.
var steerPRFetchWaitForTest = 30 * time.Second

// cmdSession is `gg session`: post a steering command to whatever gg session is
// showing this worktree.
func cmdSession(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	dir, err := sessionInboxDir(svc)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return runSessionIn(dir, svc, args, stdin, stdout, stderr)
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
	return runSessionIn(dir, svc, args, nil, stdout, stderr)
}

// runSessionIn is runSession with stdin (nil = none): an overview's text may
// be piped in.
func runSessionIn(inbox string, svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: gg session <status|navigate|reload|focus|highlight|files|note|overview> [--to tui|web] [flags]")
		return 2
	}
	args, to, err := cutTo(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	dir := sessDir{inbox: inbox, to: to}
	switch args[0] {
	case "status":
		if to != "" {
			fmt.Fprintln(stderr, "--to does not apply to status: it reports both")
			return 2
		}
		return sessionStatus(inbox, svc, args[1:], stdout, stderr)
	case "navigate":
		return sessionNavigate(dir, svc, args[1:], stdout, stderr)
	case "reload":
		return sessionReload(dir, args[1:], stdout, stderr)
	case "focus":
		return sessionFocus(dir, args[1:], stdout, stderr)
	case "highlight":
		return sessionHighlight(dir, svc, args[1:], stdout, stderr)
	case "files":
		return sessionFiles(dir, args[1:], stdout, stderr)
	case "note":
		return sessionNote(dir, svc, args[1:], stdout, stderr)
	case "overview":
		return sessionOverview(dir, svc, args[1:], stdin, stdout, stderr)
	}
	fmt.Fprintf(stderr, "session: unknown subcommand %q\n", args[0])
	return 2
}

// sessDir is where a session verb is delivered: the worktree's inbox, and
// --to's choice of side ("" = the routing table; "tui" or "web" = that side
// alone, even while the other is live).
type sessDir struct {
	inbox string
	to    string
}

// cutTo takes --to <tui|web> (or --to=<…>; one dash works too) out of a
// session verb's args, wherever it sits after the verb. Past "--" nothing is
// a flag, so a --to there stays put (parseSteerFlags hands it out as an
// argument). Given twice, --to is refused rather than last-wins.
func cutTo(args []string) (rest []string, to string, err error) {
	if len(args) > 0 && isToFlag(args[0]) {
		return nil, "", errors.New("--to goes after the verb: gg session <verb> --to tui|web …")
	}
	seen := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			rest = append(rest, args[i:]...)
			return rest, to, nil
		case !isToFlag(a):
			rest = append(rest, a)
			continue
		case seen:
			return nil, "", errors.New("--to given twice: name one side")
		case a == "--to" || a == "-to":
			if i+1 >= len(args) {
				return nil, "", errors.New("--to needs tui or web")
			}
			to = args[i+1]
			i++
		default:
			to = a[strings.IndexByte(a, '=')+1:]
		}
		seen = true
		if to != "tui" && to != "web" {
			return nil, "", fmt.Errorf("--to %q: want tui or web", to)
		}
	}
	return rest, to, nil
}

// isToFlag reports whether a is --to in any of its spellings.
func isToFlag(a string) bool {
	return a == "--to" || a == "-to" || strings.HasPrefix(a, "--to=") || strings.HasPrefix(a, "-to=")
}

// target is where a command goes: the inbox, who is live there — only the
// side --to named — and, when nothing can take it, why (printed, exit 1).
func (d sessDir) target() (inbox string, r sessionRoute, why string) {
	inbox = preferredInboxFor(d.inbox, d.to)
	r = routeFor(inbox)
	switch {
	case d.to == "tui" && !r.tuiOK && r.webOK:
		why = "no gg TUI for this worktree (gg web is live)"
	case d.to == "web" && !r.webOK && r.tuiOK:
		why = "no gg web page for this worktree (a gg TUI is live)"
	}
	switch d.to {
	case "tui":
		r.webOK = false
	case "web":
		r.tuiOK = false
	}
	if why == "" && !r.tuiOK && !r.webOK {
		why = "no gg session for this worktree"
	}
	return inbox, r, why
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
func sendSteer(d sessDir, c steer.Command, noWait bool, stdout, stderr io.Writer) int {
	return sendSteerWaiting(d, c, noWait, defaultSteerWait(), stdout, stderr)
}

// steerWait is how long sendSteer waits for the TUI's answer and what it
// prints when none came.
type steerWait struct {
	d      time.Duration
	queued string
}

func defaultSteerWait() steerWait {
	return steerWait{steerReplyWaitForTest, "queued: no answer from the TUI within 2s"}
}

// navigateWait is a navigate's wait: a pull request this machine has not
// fetched lands only once the TUI's fetch is done, so it waits for that.
func navigateWait(res domain.Resolved) steerWait {
	if p := res.Preview; p != nil && p.Tip == "" {
		if n, ok := domain.PRScopeNumber(p.Target + "..." + p.Source); ok {
			return steerWait{steerPRFetchWaitForTest, fmt.Sprintf("queued: the TUI is fetching pull request #%d — the view opens when the fetch finishes", n)}
		}
	}
	return defaultSteerWait()
}

// sendSteerWaiting is sendSteer with its wait spelled out.
func sendSteerWaiting(d sessDir, c steer.Command, noWait bool, w steerWait, stdout, stderr io.Writer) int {
	dir, r, why := d.target()
	if why != "" {
		fmt.Fprintln(stderr, why)
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
	rep, ok := steer.AwaitReply(dir, id, w.d)
	if !ok {
		// The command is still in the inbox and may yet be applied — a slow
		// answer is not a failure, and reporting one would make an agent retry
		// and move the window twice.
		fmt.Fprintln(stdout, w.queued)
		return 0
	}
	return printSteerReply(rep, stdout, stderr)
}

// backgroundNeedsContent is --background's refusal for anything but a
// content link.
const backgroundNeedsContent = "--background needs a content link (gg link --content <path>)"

// sendBackground posts a background open to the live session(s): it loads
// the file into the open-files list without touching the screen. With nothing
// live it never launches one — a launch IS the screen.
func sendBackground(dir sessDir, c steer.Command, noWait bool, stdout, stderr io.Writer) int {
	c.Background = true
	rep, code, ok := steerLive(dir, c, true, noWait, stdout, stderr)
	if !ok {
		return code
	}
	return printSteerReply(rep, stdout, stderr)
}

// printSteerReply prints a session's answer: the detail on stdout (exit 0)
// or the refusal on stderr (exit 1).
func printSteerReply(rep steer.Reply, stdout, stderr io.Writer) int {
	if rep.OK {
		if rep.Detail != "" {
			fmt.Fprintln(stdout, rep.Detail)
		}
		return 0
	}
	fmt.Fprintln(stderr, rep.Error)
	return 1
}

// steerLive posts c to the live session and waits for its answer: the TUI
// when it is live (its reply decides), else the gg web page's server, which
// answers the open-files verbs itself. With both set (a background open, a
// focus) a live page gets it TOO — as navigate does — and its answer is
// printed labelled "web:". ok is true when a reply came; otherwise code is
// the exit status, the reason already printed (nothing live, --no-wait's id,
// a timeout's "queued", which is exit 0 as in sendSteer).
func steerLive(d sessDir, c steer.Command, both, noWait bool, stdout, stderr io.Writer) (rep steer.Reply, code int, ok bool) {
	dir, r, why := d.target()
	if why != "" {
		fmt.Fprintln(stderr, why)
		return rep, 1, false
	}
	if c.ID == "" {
		c.ID = steer.NewID() // one id for both deliveries, as sendSteer
	}
	if r.webOK && (both || !r.tuiOK) {
		// The web answers synchronously, so even --no-wait gets its reply.
		wrep, err := steer.PostHTTPReply(r.web.URL, c)
		if !r.tuiOK {
			if err != nil {
				fmt.Fprintln(stderr, "web:", err)
				return rep, 1, false
			}
			return wrep, 0, true
		}
		switch {
		case err != nil:
			fmt.Fprintln(stderr, "web:", err)
		case !wrep.OK:
			fmt.Fprintln(stderr, "web:", wrep.Error)
		case wrep.Detail != "":
			fmt.Fprintln(stdout, "web:", wrep.Detail)
		}
	}
	c.Wait = !noWait
	id, err := steer.Post(dir, c)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return rep, 1, false
	}
	if noWait {
		fmt.Fprintln(stdout, id)
		return rep, 0, false
	}
	rep, got := steer.AwaitReply(dir, id, steerReplyWaitForTest)
	if !got {
		fmt.Fprintln(stdout, "queued: no answer from the TUI within 2s")
		return rep, 0, false
	}
	return rep, 0, true
}

// sessionGetenv reads GG_INBOX; a variable so tests stay off the process env.
var sessionGetenv = os.Getenv

// preferredInbox is the inbox a session verb talks to: the one the gg that
// started this process named in $GG_INBOX, while a TUI or web page there is
// live — so an agent in worktree B reaches the gg showing A that launched it
// — else dir (this worktree's, or a link's checkout's).
func preferredInbox(dir string) string { return preferredInboxFor(dir, "") }

// preferredInboxFor is preferredInbox for --to's side: $GG_INBOX wins only
// while that side ("" = either) is live there.
func preferredInboxFor(dir, to string) string {
	if own := sessionGetenv("GG_INBOX"); own != "" {
		if _, ok := steer.Live(own, steer.TUIPresence); ok && to != "web" {
			return own
		}
		if _, ok := steer.Live(own, steer.WebPresence); ok && to != "tui" {
			return own
		}
	}
	// No TUI runs in dir, but one SHOWS it (the fast switch's look): its
	// inbox is where a command about dir's files lands on screen.
	if _, ok := steer.Live(dir, steer.TUIPresence); !ok && to != "web" {
		if home, ok := domain.TUIViewing(dir); ok {
			if _, live := steer.Live(home, steer.TUIPresence); live {
				return home
			}
		}
	}
	return dir
}

// callerWorktree is the checkout this process runs in, for a worktree-bound
// command's Worktree ("" when it cannot be read: the consumer then applies
// the command as it always has).
func callerWorktree(svc *domain.Service) string {
	top, err := svc.TopLevel(context.Background())
	if err != nil {
		return ""
	}
	return top
}

// postWebSteer hands the command to an open gg web page.
func postWebSteer(base string, c steer.Command) error { return steer.PostHTTP(base, c) }

// steerDirFor resolves this worktree's inbox for a caller that must never fail
// because of it: any error is "" (no inbox), which every consumer treats as
// "nothing is live".
func steerDirFor(svc *domain.Service) string {
	dir, err := sessionInboxDir(svc)
	if err != nil {
		return ""
	}
	return dir
}

// targetOf maps a resolved note address onto the wire target (linknav.TargetOf).
func targetOf(a model.FileAddress) *steer.Target { return linknav.TargetOf(a) }

// resolveHunkLine turns --hunk N into the {side,no} the consumer lands on
// (linknav.HunkLine): the FIRST line of the hunk's range, resolved HERE and
// never in the TUI, so one landing path serves --hunk, --new-line and
// --old-line alike.
func resolveHunkLine(ctx context.Context, svc *domain.Service, cached bool, rev, path string, n int) (steer.Line, error) {
	return linknav.HunkLine(ctx, svc, cached, rev, path, n)
}

// sessionStatus prints the routing for this worktree.
func sessionStatus(dir string, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	snapPath := ""
	if cd, err := svc.GitCommonDir(context.Background()); err == nil {
		snapPath = config.SessionSnapshotPath(cd)
	}
	return sessionStatusAt(dir, svc, args, stdout, stderr, snapPath)
}

// sessionStatusAt is sessionStatus against an explicit snapshot path — the
// test seam, mirroring the sessionStatus/sessionOpenViewAt split (the real
// path depends on the state home). Both the view line and the cursor line
// read from this ONE resolved path, so a single svc.GitCommonDir call in
// sessionStatus covers both instead of each resolving it separately.
func sessionStatusAt(dir string, svc *domain.Service, args []string, stdout, stderr io.Writer, snapPath string) int {
	fs := flag.NewFlagSet("session status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print the routing as JSON")
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 0 {
		fmt.Fprintf(stderr, "session status: unexpected argument %q (status takes only --json)\n", pos[0])
		return 2
	}
	dir = preferredInbox(dir)
	r := routeFor(dir)
	view := sessionOpenViewAt(snapPath)
	link := snapshotCursorLink(snapPath)
	showing := snapshotShowing(snapPath)
	if *asJSON {
		out := map[string]any{"worktree": r.worktree(), "view": view, "cursor_link": link}
		if showing != "" {
			out["showing"] = showing // the worktree on the TUI's screen when it is not its own
		}
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
	if showing != "" {
		fmt.Fprintln(stdout, "showing:", showing)
	}
	if r.tuiOK {
		fmt.Fprintf(stdout, "tui: pid %d (since %s)\n", r.tui.PID, r.tui.Started)
	}
	if r.webOK {
		fmt.Fprintf(stdout, "web: pid %d at %s\n", r.web.PID, r.web.URL)
	}
	if view != "" {
		fmt.Fprintln(stdout, "view:", view)
	}
	if link != "" {
		fmt.Fprintln(stdout, "cursor:", link)
	}
	return 0
}

// snapshotCursorLink reads cursor.link out of a session snapshot file. Every
// failure (no file, unreadable, not JSON, no link) is "" — `gg session status`
// must report the ROUTING even when the snapshot is absent or from a gg that
// did not write links.
func snapshotCursorLink(path string) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var snap struct {
		Cursor struct {
			Link string `json:"link"`
		} `json:"cursor"`
	}
	if err := json.Unmarshal(data, &snap); err != nil {
		return ""
	}
	return snap.Cursor.Link
}

// worktree is whichever live presence knows it (both record the same path).
// snapshotShowing reads repo.viewed out of a session snapshot: the worktree
// the TUI's panels show when that is not its own ("" otherwise, or when the
// snapshot cannot be read — the routing is still reported).
func snapshotShowing(path string) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var snap struct {
		Repo struct {
			Viewed string `json:"viewed"`
		} `json:"repo"`
	}
	if json.Unmarshal(data, &snap) != nil {
		return ""
	}
	return snap.Repo.Viewed
}

func (r sessionRoute) worktree() string {
	if r.tuiOK && r.tui.Worktree != "" {
		return r.tui.Worktree
	}
	return r.web.Worktree
}

// sessionOpenViewAt reads the phase-0 session snapshot at an explicit path for
// a one-line "what is on screen" summary. Best-effort: no snapshot, or an
// unreadable one, is "". The path is the test seam (the real path depends on
// the state home — sessionStatus resolves it via svc.GitCommonDir +
// config.SessionSnapshotPath and delegates to sessionStatusAt). The shape
// mirrors the TUI's snapWriter: cursor.commit is an OBJECT carrying the hash,
// so decoding it as a bare string would fail and blank every view line.
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

// The one link-shape refusal a navigate can hit. It is a CALLER mistake
// (exit 2), unlike a git failure (exit 1), and both `gg session navigate` and
// `gg open` map it the same way.
var errNavLinkRepoOnly = linknav.ErrRepoOnly

// navigateCommandFor builds the navigate command a RESOLVED link names
// (linknav.Command): the single builder `gg session navigate <link>`,
// `gg open <link>` and the TUI's paste field share, so no two surfaces can
// post different commands for the same link. svc must be the service for the
// link's OWN checkout (openLinkTarget / linkSteerDir).
func navigateCommandFor(ctx context.Context, svc *domain.Service, res domain.Resolved) (steer.Command, error) {
	return linknav.Command(ctx, svc, res)
}

// navExit maps navigateCommandFor's error onto an exit code: 2 for the
// link-shape refusal and a preview anchor with no new side (a caller
// mistake, same surface as `gg note add <preview link>#N`), 1 for everything
// else (a git failure).
func navExit(verb string, err error, stderr io.Writer) int {
	if errors.Is(err, errNavLinkRepoOnly) || errors.Is(err, domain.ErrPreviewOldSide) {
		fmt.Fprintf(stderr, "%s: %v\n", verb, err)
		return 2
	}
	fmt.Fprintln(stderr, "error:", err)
	return 1
}

// sessionNavigate is the navigate verb: a file + a line, a commit to reveal, or
// a note step.
func sessionNavigate(dir sessDir, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session navigate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tf := addTargetFlags(fs)
	hunk := fs.Int("hunk", 0, "land on the Nth @@ hunk of --file (`gg diff --hunks` numbers them)")
	newLine := fs.Int("new-line", 0, "land on this line of the new side")
	oldLine := fs.Int("old-line", 0, "land on this line of the old side")
	next := fs.Bool("next-comment", false, "step the open diff to the next note")
	prev := fs.Bool("prev-comment", false, "step the open diff to the previous note")
	noWait := fs.Bool("no-wait", false, "post the command and exit without waiting for an answer")
	background := fs.Bool("background", false, "load a content link into the open-files list without showing it")
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if *background && !(len(pos) == 1 && isLinkArg(pos[0])) {
		fmt.Fprintln(stderr, "session navigate: "+backgroundNeedsContent)
		return 2
	}
	if len(pos) == 1 && isLinkArg(pos[0]) {
		// The link carries file, target and line; the flags it replaces are a
		// usage error rather than a silent override.
		if *tf.file != "" || *tf.rev != "" || *tf.cached || *hunk != 0 || *newLine != 0 || *oldLine != 0 || *next || *prev {
			fmt.Fprintln(stderr, "session navigate: a gg:// link already names the target (drop --file, --rev, --cached, --hunk, --new-line, --old-line and --next-comment/--prev-comment)")
			return 2
		}
		ctx := context.Background()
		// navigate opts UP for a pair: it moves a live session to the place
		// a link names, and a range is a fine place to land on (ruling R4).
		res, err := resolveLinkArg(ctx, svc, pos[0], linkShapes{Ref: true, Pair: true, Content: true, UnfetchedPR: true}, "navigate")
		if err != nil {
			return linkExit("session navigate", err, stderr)
		}
		warnAnchor(stderr, res)
		if *background && res.Hint.Kind != model.ContentHintKind {
			fmt.Fprintln(stderr, "session navigate: "+backgroundNeedsContent)
			return 2
		}
		inbox, target, err := linkSteerDir(ctx, dir.inbox, svc, res)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		c, err := navigateCommandFor(ctx, target, res)
		if err != nil {
			return navExit("session navigate", err, stderr)
		}
		if c.File != "" || c.Target != nil {
			c.Worktree = res.Checkout
		}
		dir := sessDir{inbox: inbox, to: dir.to}
		if *background {
			return sendBackground(dir, c, *noWait, stdout, stderr)
		}
		return sendSteerWaiting(dir, c, *noWait, navigateWait(res), stdout, stderr)
	}
	if len(pos) != 0 {
		fmt.Fprintf(stderr, "session navigate: unexpected argument %q (navigate takes a gg:// link or only flags)\n", pos[0])
		return 2
	}

	if *next || *prev {
		if *next && *prev {
			fmt.Fprintln(stderr, "session navigate: --next-comment and --prev-comment are mutually exclusive")
			return 2
		}
		// A step moves the cursor inside whatever diff is ALREADY open, so a
		// target names nothing it could land on. Stepping anyway and dropping
		// the target would leave the caller believing it had reached a place it
		// never asked about — the same silent discard the bare --rev arm made.
		if *tf.file != "" || *tf.rev != "" || *tf.cached || *hunk != 0 || *newLine != 0 || *oldLine != 0 {
			fmt.Fprintln(stderr, "session navigate: --next-comment/--prev-comment take no target (drop --file, --rev, --cached, --hunk, --new-line and --old-line)")
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
		// Every other flag on this verb describes a place INSIDE a file. Without
		// --file they cannot be honoured, and silently revealing the commit
		// instead would drop the caller's real request on the floor: an agent
		// that forgot --file would watch the window move somewhere it never
		// asked for and have no way to tell.
		if *tf.cached || *hunk != 0 || *newLine != 0 || *oldLine != 0 {
			fmt.Fprintln(stderr, "session navigate: --cached, --hunk, --new-line and --old-line need a --file")
			return 2
		}
		if *tf.rev == "" {
			fmt.Fprintln(stderr, "session navigate: pass --file, --rev, or --next-comment/--prev-comment")
			return 2
		}
		// A range is two commits; a landing is one place. Refused up front so
		// the message names the mistake instead of the resolver reporting an
		// unknown revision. (`gg note` refuses the same shape.)
		if strings.Contains(*tf.rev, "..") {
			fmt.Fprintln(stderr, "session navigate: --rev names where to land; pass one commit, not a range")
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
	// 1-based means 1-based. Zero is "unset" and the rule above already caught
	// it; a negative is a mistake, never an offset from the end. Checked before
	// any git work, and worded exactly as `gg note add` words it.
	switch {
	case *hunk < 0:
		fmt.Fprintln(stderr, "session navigate: --hunk must be a 1-based hunk number")
		return 2
	case *newLine < 0:
		fmt.Fprintln(stderr, "session navigate: --new-line must be a 1-based line number")
		return 2
	case *oldLine < 0:
		fmt.Fprintln(stderr, "session navigate: --old-line must be a 1-based line number")
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
		Cmd:      "navigate",
		File:     addr.Path,
		Target:   targetOf(addr),
		Line:     &line,
		Worktree: callerWorktree(svc),
	}, *noWait, stdout, stderr)
}

// parseSteerFlags parses fs allowing flags to follow positional arguments. Go's
// flag package stops at the first non-flag token, so `gg session focus commits
// --no-wait` would otherwise leave --no-wait unparsed and silently wait two
// seconds for an answer the caller said it did not want. A "--" ends the
// flags: everything after it is positional (a file named -x.go). A flag whose
// value is a literal "--" reads the same way — a value nobody means.
func parseSteerFlags(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if n := len(args) - len(rest); n > 0 && args[n-1] == "--" {
			return append(pos, rest...), nil // "--" ends the flags
		}
		if len(rest) == 0 {
			return pos, nil
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
}

// sessionReload re-reads one or more of the TUI's sources.
func sessionReload(dir sessDir, args []string, stdout, stderr io.Writer) int {
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
		fmt.Fprintln(stderr, "usage: gg session reload [notes|status|worktrees|all] [--no-wait]")
		return 2
	}
	switch src {
	case "notes", "status", "worktrees", "all":
	default:
		fmt.Fprintf(stderr, "session reload: unknown source %q (notes, status, worktrees or all)\n", src)
		return 2
	}
	return sendSteer(dir, steer.Command{Cmd: "reload", Sources: []string{src}}, *noWait, stdout, stderr)
}

// sessionFocus switches the TUI (or the web page) to a named panel.
func sessionFocus(dir sessDir, args []string, stdout, stderr io.Writer) int {
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
func sessionHighlight(dir sessDir, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
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

func sessionHighlightAdd(dir sessDir, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
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
	// --side and --tone are validated BEFORE branching on a link (ruling
	// P14): a bogus value is a usage error on either arm, even though the
	// link arm below ignores --side (a link's old:/new prefix already
	// carries the side).
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
	// ruling P21: --side is a REPLACED flag, not an orthogonal one — a link
	// already carries its own side (its old:/new: prefix, or the hunk's
	// resolved side). fs.Visit reports only flags actually PASSED, so this
	// distinguishes "the user typed --side" from "*side just holds its
	// default" (which --side new would be indistinguishable from otherwise).
	sideSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "side" {
			sideSet = true
		}
	})
	if len(pos) == 1 && isLinkArg(pos[0]) {
		if *tf.file != "" || *tf.rev != "" || *tf.cached || *start != 0 || sideSet {
			fmt.Fprintln(stderr, "session highlight add: a gg:// link already names the target, the first line and the side (drop --file, --rev, --cached, --start and --side)")
			return 2
		}
		text := pos[0]
		ctx := context.Background()
		// A change-set link lands its band on commit b, new side — the address
		// a pair's diff is stamped with once its note scope is armed.
		res, err := resolveLinkArg(ctx, svc, text, linkShapes{Ref: true, Pair: true}, "highlight")
		if err != nil {
			return linkExit("session highlight add", err, stderr)
		}
		warnAnchor(stderr, res)
		if res.Pair != nil && res.Side == model.NoteSideOld {
			// The band is keyed on commit b; a pair's old side is commit a's
			// text, which that key does not name.
			fmt.Fprintln(stderr, "session highlight add: a change-set link highlights the new side (drop :old:)")
			return 2
		}
		if res.Addr.Path == "" || (res.Line < 1 && res.Hunk < 1) {
			fmt.Fprintln(stderr, "session highlight add: the link needs a file and a line or hunk (gg://<repo>/<path>[@<target>]:<line>[-<end>] or #<hunk>)")
			return 2
		}
		inbox, target, err := linkSteerDir(ctx, dir.inbox, svc, res)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		dir := sessDir{inbox: inbox, to: dir.to}
		sideVal, first, last, linkEnd := string(res.Side), res.Line, *end, res.End
		switch {
		case res.Hunk > 0:
			// A hunk link already names a range (controller ruling P22): a
			// -<end> suffix or --end alongside it is a usage error, the same
			// as mixing --start with any link.
			if linkEnd > 0 || *end != 0 {
				fmt.Fprintln(stderr, "session highlight add: a #<hunk> link already names a range; drop -<end> and --end")
				return 2
			}
			scope, scoped, serr := noteScopeFromLink(ctx, target, res)
			if serr != nil {
				fmt.Fprintln(stderr, "error:", serr)
				return 1
			}
			if scoped {
				// PreviewHunkAnchor, never a plain HunkRange over
				// linkDiffSpec's patch: it is the ONE place the preview's
				// old-side refusal lives, so a delete-only hunk cannot land a
				// band on a side no stored address names.
				hs, rng, herr := target.PreviewHunkAnchor(ctx, scope.Set, res.Addr.Path, res.Hunk)
				if errors.Is(herr, domain.ErrPreviewOldSide) {
					fmt.Fprintln(stderr, "session highlight add:", herr)
					return 2
				}
				if herr != nil {
					fmt.Fprintln(stderr, "error:", herr)
					return 1
				}
				sideVal, first, last = string(hs), rng[0], rng[1]
				break
			}
			spec, err := linkDiffSpec(ctx, target, res)
			if err != nil {
				fmt.Fprintln(stderr, "error:", err)
				return 1
			}
			hs, rng, err := target.HunkRange(ctx, spec, res.Addr.Path, res.Hunk)
			if err != nil {
				fmt.Fprintln(stderr, "error:", err)
				return 1
			}
			sideVal, first, last = string(hs), rng[0], rng[1]
		case linkEnd > 0:
			if last != 0 {
				fmt.Fprintln(stderr, "session highlight add: the link's -<end> and --end name the same thing; pass one")
				return 2
			}
			last = linkEnd
		}
		if last != 0 && last < first {
			fmt.Fprintln(stderr, "session highlight add: the range ends before it starts")
			return 2
		}
		return sendSteer(dir, steer.Command{
			Cmd: "highlight", File: res.Addr.Path, Target: targetOf(res.Addr),
			Side: sideVal, Start: first, End: last, Tone: *tone, Worktree: res.Checkout,
		}, *noWait, stdout, stderr)
	}
	if len(pos) != 0 {
		fmt.Fprintf(stderr, "session highlight add: unexpected argument %q\n", pos[0])
		return 2
	}
	if *tf.file == "" || *start < 1 {
		fmt.Fprintln(stderr, "session highlight add: --file and a 1-based --start are required")
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
		Side: *side, Start: *start, End: *end, Tone: *tone, Worktree: callerWorktree(svc),
	}, *noWait, stdout, stderr)
}

func sessionHighlightClear(dir sessDir, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
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

// linkSteerDir picks the inbox a link's command goes to. When the link
// resolves to the checkout the caller is already in, the inbox passed to
// runSession is kept — that is the injected test seam, and the common case.
// Otherwise the TARGET checkout's own inbox is computed, which is what lets
// `gg session navigate gg://…` run from /tmp move a window in another
// worktree. The returned service is the one the command's target belongs to.
//
// A failure to read the target's common dir is RETURNED, not swallowed into
// an empty dir: an empty inbox path reads downstream as "no session there",
// which would report a broken repository as a window nobody has open.
func linkSteerDir(ctx context.Context, fallback string, cwd *domain.Service, res domain.Resolved) (string, *domain.Service, error) {
	top, err := cwd.TopLevel(ctx)
	if err == nil && domain.SamePath(top, res.Checkout) {
		return fallback, cwd, nil
	}
	target := openLinkTarget(res)
	cd, err := target.GitCommonDir(ctx)
	if err != nil {
		return "", target, err
	}
	return config.SessionSteerDir(cd, res.Checkout), target, nil
}
