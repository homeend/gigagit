package cli

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/mcp"
)

// agentEnvFor serves a real agent channel with a fake starter and one
// manual agent session; agentGetenv returns its URL + token.
func agentEnvFor(t *testing.T, starter mcp.Starter) (dir, full string) {
	t.Helper()
	restore := domain.UseSessionManager(agentsession.NewManager())
	dir = newRepoDir(t)
	ts := httptest.NewServer(mcp.NewAgentHost().Handler(starter))
	tc := config.ToolCommand{Category: "session", Name: "Sh", Mode: "session", Command: "sh -c 'sleep 600'"}
	s, tok, err := domain.Open(dir).StartAgentSession(context.Background(), tc, dir, "", 80, 24, nil, ts.URL+"/mcp", domain.SpawnRecord{}, "")
	if err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"GG_MCP_URL": ts.URL + "/mcp", "GG_SESSION_TOKEN": tok}
	prev := agentGetenv
	agentGetenv = func(k string) string { return env[k] }
	t.Cleanup(func() {
		agentGetenv = prev
		ts.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		domain.Sessions().KillAll(ctx)
		restore()
	})
	return dir, domain.FullSessionID(s.Info().ID)
}

func runAgentCLI(t *testing.T, dir, stdin string, args ...string) (int, string, string) {
	t.Helper()
	return runCLIStdin(t, dir, stdin, append([]string{"agent"}, args...)...)
}

func TestAgentOutsideGG(t *testing.T) {
	prev := agentGetenv
	agentGetenv = func(string) string { return "" }
	t.Cleanup(func() { agentGetenv = prev })
	dir := newRepoDir(t)
	code, _, errOut := runAgentCLI(t, dir, "", "task")
	if code != 2 || !strings.Contains(errOut, "inside a gg console") {
		t.Fatalf("task outside gg = %d %q", code, errOut)
	}
	code, out, _ := runAgentCLI(t, dir, "", "list")
	if code != 0 || !strings.Contains(out, "no gg TUI is running") {
		t.Fatalf("list outside gg with no TUI = %d %q", code, out)
	}
}

// Inside a gg console (GG_SESSION_ID set) with no channel: say that, not
// "run this inside a gg console".
func TestAgentInsideAConsoleWithoutChannel(t *testing.T) {
	prev := agentGetenv
	agentGetenv = func(k string) string {
		if k == "GG_SESSION_ID" {
			return "p/s1"
		}
		return ""
	}
	t.Cleanup(func() { agentGetenv = prev })
	code, _, errOut := runAgentCLI(t, newRepoDir(t), "", "task")
	if code != 2 || !strings.Contains(errOut, "no agent channel") || strings.Contains(errOut, "run this inside") {
		t.Fatalf("task in a console without a channel = %d %q", code, errOut)
	}
}

func TestAgentStartReadsPromptFromStdin(t *testing.T) {
	gotCh := make(chan domain.AgentStartRequest, 1)
	dir, _ := agentEnvFor(t, func(_ context.Context, r domain.AgentStartRequest) (domain.AgentStartResult, error) {
		gotCh <- r
		return domain.AgentStartResult{ID: "p/s9", Worktree: "/w", Tool: r.Tool, Warning: "claim stayed"}, nil
	})
	code, out, errOut := runAgentCLI(t, dir, "fix issue 7\nsecond line\n", "start", "--worktree", "job", "--tool", "Claude", "--note", "n", "--prompt-file", "-")
	if code != 0 || strings.TrimSpace(out) != "p/s9" || !strings.Contains(errOut, "warning: claim stayed") {
		t.Fatalf("start = %d %q %q", code, out, errOut)
	}
	got := <-gotCh
	if got.Prompt != "fix issue 7\nsecond line\n" || got.Worktree != "job" || got.Tool != "Claude" || got.Note != "n" {
		t.Fatalf("request %+v", got)
	}
}

func TestAgentStartRefusesBothPromptFlags(t *testing.T) {
	dir, _ := agentEnvFor(t, func(context.Context, domain.AgentStartRequest) (domain.AgentStartResult, error) {
		t.Error("the starter must not run")
		return domain.AgentStartResult{}, nil
	})
	code, _, errOut := runAgentCLI(t, dir, "from stdin", "start", "--worktree", "job", "--tool", "Claude", "--prompt", "inline", "--prompt-file", "-")
	if code != 2 || !strings.Contains(errOut, "not both") {
		t.Fatalf("--prompt with --prompt-file = %d %q", code, errOut)
	}
}

func TestAgentListAndRefusal(t *testing.T) {
	dir, full := agentEnvFor(t, nil)
	code, out, _ := runAgentCLI(t, dir, "", "list")
	if code != 0 || !strings.Contains(out, full) || !strings.Contains(out, "running") {
		t.Fatalf("list = %d %q", code, out)
	}
	code, out, _ = runAgentCLI(t, dir, "", "list", "--json")
	if code != 0 || !strings.Contains(out, `"id":`) {
		t.Fatalf("list --json = %d %q", code, out)
	}
	code, _, errOut := runAgentCLI(t, dir, "", "task")
	if code != 2 || !strings.Contains(errOut, "not started by an agent") {
		t.Fatalf("a refusal = %d %q", code, errOut)
	}
	code, _, errOut = runAgentCLI(t, dir, "", "kill", full)
	if code != 2 || !strings.Contains(errOut, "not an agent you started") {
		t.Fatalf("kill of a non-descendant = %d %q", code, errOut)
	}
}

func TestAgentScreenPrintsText(t *testing.T) {
	dir, full := agentEnvFor(t, nil)
	code, _, errOut := runAgentCLI(t, dir, "", "screen", full)
	if code != 0 {
		t.Fatalf("screen = %d %q", code, errOut)
	}
}

func TestAgentSendNeedsTextOrKeys(t *testing.T) {
	dir, full := agentEnvFor(t, nil)
	code, _, errOut := runAgentCLI(t, dir, "", "send", full)
	if code != 2 || !strings.Contains(errOut, "usage: gg agent send") {
		t.Fatalf("send with nothing = %d %q", code, errOut)
	}
}

func TestAgentFlagsAfterPositionals(t *testing.T) {
	if id, text, noEnter, keys, err := splitSendArgs([]string{"p/s3", "hello", "--no-enter", "--key", "esc", "world"}); err != nil ||
		id != "p/s3" || text != "hello world" || !noEnter || len(keys) != 1 || keys[0] != "esc" {
		t.Fatalf("send split = %q %q %v %v %v", id, text, noEnter, keys, err)
	}
	if _, _, _, _, err := splitSendArgs([]string{"p/s3", "--key"}); err == nil {
		t.Fatal("--key without a value must be a usage error")
	}
	dir, full := agentEnvFor(t, nil)
	code, _, errOut := runAgentCLI(t, dir, "", "kill", full, "--remove")
	if code != 2 || !strings.Contains(errOut, "not an agent you started") {
		t.Fatalf("kill <id> --remove must parse and reach the tool: %d %q", code, errOut)
	}
}

// Serial: installs a static state watcher. The list shows the activity word
// after the state; the screen leads with the activity and the dialog's
// choices when there is one.
func TestAgentListAndScreenShowActivity(t *testing.T) {
	dir, full := agentEnvFor(t, nil)
	code, out, _ := runAgentCLI(t, dir, "", "list")
	if code != 0 || strings.Contains(out, "idle") {
		t.Fatalf("unclassified list = %d %q", code, out)
	}
	id := domain.SessionID(full[strings.LastIndex(full, "/")+1:])
	restore := domain.UseSessionStates(domain.NewStaticStates(map[domain.SessionID]domain.SessionActivity{
		id: {State: domain.ActivityQuestion, Since: time.Now(), Stalled: true,
			Options: []domain.ActivityOption{{Key: "1", Label: "Yes"}, {Key: "2", Label: "No"}}},
	}))
	defer restore()
	code, out, _ = runAgentCLI(t, dir, "", "list")
	if code != 0 || !strings.Contains(out, "  running  question  stalled  ") {
		t.Fatalf("list = %d %q", code, out)
	}
	code, out, _ = runAgentCLI(t, dir, "", "list", "--json")
	if code != 0 || !strings.Contains(out, `"activity":"question"`) || !strings.Contains(out, `"stalled":true`) {
		t.Fatalf("list --json = %d %q", code, out)
	}
	code, out, _ = runAgentCLI(t, dir, "", "screen", full)
	if code != 0 || !strings.HasPrefix(out, "activity: question (1. Yes · 2. No)\n") {
		t.Fatalf("screen = %d %q", code, out)
	}
}

func TestAgentReportAndWaitCLI(t *testing.T) {
	dir, full := agentEnvFor(t, nil)
	code, out, _ := runAgentCLI(t, dir, "", "report", "--final", "merged", "feat/x")
	if code != 0 || !strings.HasPrefix(out, "reported #") {
		t.Fatalf("report = %d %q", code, out)
	}
	code, out, _ = runAgentCLI(t, dir, "two lines\nsecond\n", "report", "-F", "-")
	if code != 0 {
		t.Fatalf("report -F - = %d %q", code, out)
	}
	code, out, _ = runAgentCLI(t, dir, "", "screen", full, "--reports")
	if code != 0 || !strings.Contains(out, "merged feat/x") || !strings.Contains(out, "  second") || strings.Index(out, "merged") > strings.Index(out, "second") || !strings.Contains(out, "(final)") {
		t.Fatalf("screen --reports = %d %q", code, out)
	}
	code, out, _ = runAgentCLI(t, dir, "", "screen", full)
	if code != 0 || !strings.Contains(out, "report: two lines\n") {
		t.Fatalf("screen = %d %q", code, out)
	}
	code, out, _ = runAgentCLI(t, dir, "", "list")
	if code != 0 || !strings.Contains(out, "  reported  ") {
		t.Fatalf("list = %d %q", code, out)
	}
	code, out, _ = runAgentCLI(t, dir, "", "list", "--json")
	if code != 0 || !strings.Contains(out, `"report_at":`) || strings.Contains(out, `"report_final":true`) {
		t.Fatalf("list --json = %d %q (the latest report is not final)", code, out)
	}
	// wait: a childless any-wait is a refusal (2); usage slips are 2.
	if code, _, errOut := runAgentCLI(t, dir, "", "wait", "--timeout", "1"); code != 2 || !strings.Contains(errOut, "no workers") {
		t.Fatalf("wait = %d %q", code, errOut)
	}
	if code, _, _ := runAgentCLI(t, dir, "", "wait", "--until"); code != 2 {
		t.Fatal("--until without a value is usage")
	}
	if code, _, _ := runAgentCLI(t, dir, "", "wait", "a", "b"); code != 2 {
		t.Fatal("two ids is usage")
	}
	if code, _, _ := runAgentCLI(t, dir, "", "report"); code != 2 {
		t.Fatal("report without text is usage")
	}
}

func TestWaitExitCodeAndPrint(t *testing.T) {
	at := time.Date(2026, 10, 3, 1, 2, 3, 0, time.Local)
	code := 7
	res := domain.AgentWaitResult{ID: "p/s1", Event: "report", Activity: "idle", Report: &domain.AgentReport{Seq: 3, Text: "a\nb", Final: true, At: at}, ExitCode: &code}
	var b strings.Builder
	printWaitResult(&b, res)
	want := "event: report\nid: p/s1\nactivity: idle\nexit code: 7\nreport #3 (final) 01:02:03:\n  a\n  b\n"
	if b.String() != want || waitExitCode(res) != 0 {
		t.Fatalf("%q (code %d)", b.String(), waitExitCode(res))
	}
	b.Reset()
	printWaitResult(&b, domain.AgentWaitResult{TimedOut: true, ID: "p/s1", Activity: "working"})
	if b.String() != "timed out\nid: p/s1\nactivity: working\n" || waitExitCode(domain.AgentWaitResult{TimedOut: true}) != 3 {
		t.Fatalf("%q", b.String())
	}
}
