package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/homeend/gigagit/internal/agentlink"
	"github.com/homeend/gigagit/internal/domain"
)

// agentGetenv reads the console's environment (a test seam).
var agentGetenv = os.Getenv

const outsideGG = "run this inside a gg console (GG_MCP_URL and GG_SESSION_TOKEN are unset)"

// noChannel: a gg console (GG_SESSION_ID set) that was given no channel.
const noChannel = "this gg console has no agent channel — an Open terminal never gets one; " +
	"for an agent, its gg could not start the channel (see its status line; restart gg)"

func cmdAgent(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: gg agent <start|list|screen|send|kill|task|wait|report> [args]")
		return 2
	}
	verb, rest := args[0], args[1:]
	c, inside := agentlink.FromEnv(agentGetenv)
	if verb == "list" && !inside {
		return agentListOutside(svc, rest, stdout, stderr)
	}
	if !inside {
		why := outsideGG
		if agentGetenv("GG_SESSION_ID") != "" {
			why = noChannel
		}
		fmt.Fprintln(stderr, "agent "+verb+": "+why)
		return 2
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	switch verb {
	case "start":
		return agentStart(ctx, c, rest, stdin, stdout, stderr)
	case "list":
		return agentList(ctx, c, rest, stdout, stderr)
	case "screen":
		return agentOneID(ctx, c, "screen", rest, stdout, stderr)
	case "send":
		return agentSend(ctx, c, rest, stdout, stderr)
	case "kill":
		return agentKill(ctx, c, rest, stdout, stderr)
	case "task":
		return agentTask(ctx, c, rest, stdout, stderr)
	case "wait":
		return agentWait(c, rest, stdout, stderr) // its own ctx: the timeout plus slack
	case "report":
		return agentReport(ctx, c, rest, stdin, stdout, stderr)
	}
	fmt.Fprintf(stderr, "agent: unknown verb %q\n", verb)
	return 2
}

// call runs tool and maps the outcome to an exit code: a refusal prints the
// tool's text and is 2, a channel failure is 1.
func call(ctx context.Context, c *agentlink.Client, verb, tool string, in any, out any, stderr io.Writer) int {
	res, err := c.Call(ctx, tool, in)
	if err != nil {
		fmt.Fprintln(stderr, "agent "+verb+":", err)
		return 1
	}
	if res.IsError {
		fmt.Fprintln(stderr, "agent "+verb+": "+agentlink.ResultText(res))
		return 2
	}
	if out != nil {
		if err := agentlink.Decode(res, out); err != nil {
			fmt.Fprintln(stderr, "agent "+verb+":", err)
			return 1
		}
	}
	return 0
}

func agentStart(ctx context.Context, c *agentlink.Client, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("agent start", flag.ContinueOnError)
	fs.SetOutput(stderr)
	wt := fs.String("worktree", "", "worktree path, directory name or branch")
	tool := fs.String("tool", "", "session command name, as in [agents] spawn")
	prompt := fs.String("prompt", "", "the worker's task")
	file := fs.String("prompt-file", "", "read the task from this file (- = stdin)")
	note := fs.String("note", "", "note on the worktree claim (e.g. the issue URL)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *prompt != "" && *file != "" {
		fmt.Fprintln(stderr, "agent start: pass --prompt or --prompt-file, not both")
		return 2
	}
	text := *prompt
	if *file != "" {
		var data []byte
		var err error
		if *file == "-" {
			data, err = io.ReadAll(io.LimitReader(stdin, domain.MaxBriefBytes+1))
		} else {
			data, err = os.ReadFile(*file)
		}
		if err != nil {
			fmt.Fprintln(stderr, "agent start:", err)
			return 1
		}
		text = string(data)
	}
	if *wt == "" || *tool == "" || text == "" {
		fmt.Fprintln(stderr, "usage: gg agent start --worktree <path|name> --tool <name> (--prompt <text> | --prompt-file <file|->) [--note <text>]")
		return 2
	}
	var res domain.AgentStartResult
	in := map[string]any{"worktree": *wt, "tool": *tool, "prompt": text, "note": *note}
	if code := call(ctx, c, "start", "agent_start", in, &res, stderr); code != 0 {
		return code
	}
	fmt.Fprintln(stdout, res.ID)
	if res.Warning != "" {
		fmt.Fprintln(stderr, "warning: "+res.Warning)
	}
	return 0
}

func agentList(ctx context.Context, c *agentlink.Client, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("agent list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "the full rows as JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	var out struct {
		Agents []domain.AgentEntry `json:"agents"`
	}
	if code := call(ctx, c, "list", "agent_list", map[string]any{}, &out, stderr); code != 0 {
		return code
	}
	if *asJSON {
		data, _ := json.Marshal(out.Agents)
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	for _, a := range out.Agents {
		line := a.ID + "  " + a.State
		if a.Activity != "" {
			line += "  " + a.Activity
		}
		if a.Stalled {
			line += "  stalled"
		}
		if !a.ReportAt.IsZero() {
			if a.ReportFinal {
				line += "  done"
			} else {
				line += "  reported"
			}
		}
		line += "  " + a.Tool + "  " + a.Worktree
		if a.Parent != "" {
			line += "  parent " + a.Parent
		}
		if a.Mine {
			line += "  mine"
		}
		fmt.Fprintln(stdout, line)
	}
	return 0
}

// agentListOutside prints the live TUIs from their registry files: no
// channel call (there is no token outside gg).
func agentListOutside(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("agent list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "the live TUIs as JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	hosts := svc.LiveAgentHosts()
	if *asJSON {
		data, _ := json.Marshal(hosts)
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	if len(hosts) == 0 {
		fmt.Fprintln(stdout, "no gg TUI is running")
		return 0
	}
	for _, h := range hosts {
		fmt.Fprintf(stdout, "gg TUI pid %d in %s\n", h.PID, h.Worktree)
		for _, s := range h.Sessions {
			name := s.Agent
			if name == "" {
				name = s.Label
			}
			fmt.Fprintf(stdout, "  %s  %s  %s  %s\n", s.ID, s.State, name, s.Dir)
		}
	}
	return 0
}

func agentOneID(ctx context.Context, c *agentlink.Client, verb string, args []string, stdout, stderr io.Writer) int {
	reports := false
	var ids []string
	for _, a := range args { // flags anywhere: `screen <id> --reports`
		if a == "--reports" || a == "-reports" {
			reports = true
		} else {
			ids = append(ids, a)
		}
	}
	if len(ids) != 1 {
		fmt.Fprintln(stderr, "usage: gg agent "+verb+" <id> [--reports]")
		return 2
	}
	var out struct {
		Text     string                  `json:"text"`
		Activity string                  `json:"activity"`
		Options  []domain.ActivityOption `json:"options"`
		Report   *domain.AgentReport     `json:"report"`
		Reports  []domain.AgentReport    `json:"reports"`
	}
	if code := call(ctx, c, verb, "agent_"+verb, map[string]any{"id": ids[0]}, &out, stderr); code != 0 {
		return code
	}
	if reports { // the kept reports, oldest first, instead of the screen
		for _, r := range out.Reports {
			fmt.Fprintln(stdout, reportLines(r))
		}
		return 0
	}
	if out.Activity != "" {
		fmt.Fprintln(stdout, activityLine(out.Activity, out.Options))
	}
	if out.Report != nil {
		fmt.Fprintln(stdout, "report: "+domain.ReportFirstLine(out.Report.Text))
	}
	fmt.Fprintln(stdout, out.Text)
	return 0
}

// agentWait: gg agent wait [<id>] [--until <what>] [--timeout <s>] — exit
// 0 on an event, 3 on a timeout (loop on it), 1 on a channel failure, 2 on
// a refusal or usage.
func agentWait(c *agentlink.Client, args []string, stdout, stderr io.Writer) int {
	const usage = "usage: gg agent wait [<id>] [--until idle|question|exit|report|any] [--timeout <seconds>]"
	id, until, timeout := "", "", 0 // timeout 0: none given (the host's default)
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case flagIs(a, "until"):
			v, ok := flagValue(a, args, &i)
			if !ok {
				fmt.Fprintln(stderr, usage)
				return 2
			}
			until = v
		case flagIs(a, "timeout"):
			v, ok := flagValue(a, args, &i)
			n, err := strconv.Atoi(v)
			if !ok || err != nil {
				fmt.Fprintln(stderr, usage)
				return 2
			}
			if n < 1 || n > int(domain.WaitMaxTimeout.Seconds()) {
				fmt.Fprintln(stderr, "gg agent wait: --timeout is 1 … "+strconv.Itoa(int(domain.WaitMaxTimeout.Seconds()))+" seconds")
				return 2
			}
			timeout = n
		case strings.HasPrefix(a, "-"):
			fmt.Fprintln(stderr, usage)
			return 2
		default:
			if id != "" {
				fmt.Fprintln(stderr, usage)
				return 2
			}
			id = a
		}
	}
	slack := domain.WaitDefaultTimeout + 15*time.Second
	if timeout > 0 {
		slack = time.Duration(timeout)*time.Second + 15*time.Second
	}
	// An interrupt cancels the call: the host's waiter ends with it, so an
	// event that arrives later is not delivered to a process that is gone.
	sigCtx, stop := waitSignalContext(context.Background())
	defer stop()
	ctx, cancel := context.WithTimeout(sigCtx, slack)
	defer cancel()
	in := map[string]any{"id": id, "until": until}
	if timeout > 0 {
		in["timeout_s"] = timeout // left out, the host waits its default
	}
	var out domain.AgentWaitResult
	if code := call(ctx, c, "wait", "agent_wait", in, &out, stderr); code != 0 {
		return code
	}
	printWaitResult(stdout, out)
	return waitExitCode(out)
}

// waitSignalContext ends with SIGINT / SIGTERM (a test seam).
var waitSignalContext = func(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
}

func waitExitCode(out domain.AgentWaitResult) int {
	if out.TimedOut {
		return 3
	}
	return 0
}

func printWaitResult(w io.Writer, out domain.AgentWaitResult) {
	if out.TimedOut {
		fmt.Fprintln(w, "timed out")
	} else {
		fmt.Fprintln(w, "event: "+out.Event)
	}
	if out.ID != "" {
		fmt.Fprintln(w, "id: "+out.ID)
	}
	if out.State != "" {
		fmt.Fprintln(w, "state: "+out.State)
	}
	if out.Activity != "" {
		fmt.Fprintln(w, activityLine(out.Activity, out.Options))
	}
	if out.ExitCode != nil {
		fmt.Fprintf(w, "exit code: %d\n", *out.ExitCode)
	}
	if out.Report != nil {
		fmt.Fprintln(w, reportLines(*out.Report))
	}
}

// reportLines: "report #3 (final) 12:04:05:" then the text, indented.
func reportLines(r domain.AgentReport) string {
	head := fmt.Sprintf("report #%d", r.Seq)
	if r.Final {
		head += " (final)"
	}
	head += " " + r.At.Local().Format("15:04:05") + ":"
	return head + "\n  " + strings.ReplaceAll(strings.TrimRight(r.Text, "\n"), "\n", "\n  ")
}

// agentReport: gg agent report [--final] (<text>… | -F <file> | -F -)
// flagIs: a is the named flag, with one dash or two, alone or "=value".
func flagIs(a, name string) bool {
	a = strings.TrimPrefix(strings.TrimPrefix(a, "-"), "-")
	return a == name || strings.HasPrefix(a, name+"=")
}

// flagValue: the value of the flag at args[*i] — after its "=", or the next
// argument (consumed).
func flagValue(a string, args []string, i *int) (string, bool) {
	if _, v, ok := strings.Cut(a, "="); ok {
		return v, true
	}
	if *i+1 >= len(args) {
		return "", false
	}
	*i++
	return args[*i], true
}

func agentReport(ctx context.Context, c *agentlink.Client, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	const usage = "usage: gg agent report [--final] [--] (<text>… | -F <file> | -F -)"
	final, file := false, ""
	var words []string
	// Flags come before the text: a "--final" or "-F" inside the prose is
	// a word of it, and "--" ends the flags outright.
	i := 0
flags:
	for ; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--":
			i++
			break flags
		case a == "--final" || a == "-final":
			final = true
		case a == "-F" || a == "--file":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, usage)
				return 2
			}
			i++
			file = args[i]
		default:
			break flags
		}
	}
	words = args[i:]
	text := strings.Join(words, " ")
	if file != "" {
		if text != "" {
			fmt.Fprintln(stderr, usage)
			return 2
		}
		var r io.Reader = stdin
		if file != "-" {
			f, err := os.Open(file)
			if err != nil {
				fmt.Fprintln(stderr, "agent report:", err)
				return 1
			}
			defer f.Close()
			r = f
		}
		// Read one byte past the cap: no more is needed to refuse.
		b, err := io.ReadAll(io.LimitReader(r, domain.MaxReportBytes+1))
		if err != nil {
			fmt.Fprintln(stderr, "agent report:", err)
			return 1
		}
		if len(b) > domain.MaxReportBytes {
			fmt.Fprintf(stderr, "agent report: the report is larger than %d bytes\n", domain.MaxReportBytes)
			return 2
		}
		text = string(b)
	}
	if strings.TrimSpace(text) == "" {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	var out domain.AgentReport
	if code := call(ctx, c, "report", "agent_report", map[string]any{"text": text, "final": final}, &out, stderr); code != 0 {
		return code
	}
	fmt.Fprintf(stdout, "reported #%d\n", out.Seq)
	return 0
}

// splitSendArgs reads `send` arguments with its flags anywhere (Go's flag
// package stops at the first positional): <id> [text…] [--no-enter]
// [--key <name>]….
func splitSendArgs(args []string) (id, text string, noEnter bool, keys []string, err error) {
	var words []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--no-enter" || a == "-no-enter":
			noEnter = true
		case a == "--key" || a == "-key":
			if i+1 >= len(args) {
				return "", "", false, nil, fmt.Errorf("--key needs a key name")
			}
			i++
			keys = append(keys, args[i])
		case strings.HasPrefix(a, "--key="):
			keys = append(keys, strings.TrimPrefix(a, "--key="))
		default:
			words = append(words, a)
		}
	}
	if len(words) == 0 {
		return "", "", false, nil, fmt.Errorf("missing <id>")
	}
	return words[0], strings.Join(words[1:], " "), noEnter, keys, nil
}

func agentSend(ctx context.Context, c *agentlink.Client, args []string, stdout, stderr io.Writer) int {
	const usage = "usage: gg agent send <id> [<text>…] [--no-enter] [--key <name>]…"
	id, text, noEnter, keys, err := splitSendArgs(args)
	if err != nil || (text == "" && len(keys) == 0) {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	in := map[string]any{"id": id, "text": text, "enter": !noEnter && text != "", "keys": keys}
	return call(ctx, c, "send", "agent_send", in, nil, stderr)
}

func agentKill(ctx context.Context, c *agentlink.Client, args []string, stdout, stderr io.Writer) int {
	remove := false
	var ids []string
	for _, a := range args { // flags anywhere: `kill <id> --remove`
		if a == "--remove" || a == "-remove" {
			remove = true
		} else {
			ids = append(ids, a)
		}
	}
	if len(ids) != 1 {
		fmt.Fprintln(stderr, "usage: gg agent kill <id> [--remove]")
		return 2
	}
	return call(ctx, c, "kill", "agent_kill", map[string]any{"id": ids[0], "remove": remove}, nil, stderr)
}

func agentTask(ctx context.Context, c *agentlink.Client, args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "usage: gg agent task")
		return 2
	}
	var out struct {
		Brief string `json:"brief"`
	}
	if code := call(ctx, c, "task", "agent_task", map[string]any{}, &out, stderr); code != 0 {
		return code
	}
	fmt.Fprint(stdout, out.Brief)
	if !strings.HasSuffix(out.Brief, "\n") {
		fmt.Fprintln(stdout)
	}
	return 0
}

// activityLine: "activity: question (1. Yes · 2. No)" — the first line of
// `gg agent screen` when gg can tell what the agent is doing.
func activityLine(activity string, opts []domain.ActivityOption) string {
	line := "activity: " + activity
	if len(opts) == 0 {
		return line
	}
	parts := make([]string, len(opts))
	for i, o := range opts {
		parts[i] = o.Key + ". " + o.Label
	}
	return line + " (" + strings.Join(parts, " · ") + ")"
}
