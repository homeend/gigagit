package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
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
		fmt.Fprintln(stderr, "usage: gg agent <start|list|screen|send|kill|task> [args]")
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
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: gg agent "+verb+" <id>")
		return 2
	}
	var out struct {
		Text     string                  `json:"text"`
		Activity string                  `json:"activity"`
		Options  []domain.ActivityOption `json:"options"`
	}
	if code := call(ctx, c, verb, "agent_"+verb, map[string]any{"id": args[0]}, &out, stderr); code != 0 {
		return code
	}
	if out.Activity != "" {
		fmt.Fprintln(stdout, activityLine(out.Activity, out.Options))
	}
	fmt.Fprintln(stdout, out.Text)
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
