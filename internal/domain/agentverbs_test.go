package domain

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/wtclaim"
)

// spawnFixture: a repo, a worktree, a global config allowing "Sleeper", and
// a manually started overseer session in the main worktree.
func spawnFixture(t *testing.T, cap int) (main, wt string, svc *Service, overseer string) {
	t.Helper()
	useSessions(t)
	cfgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	os.MkdirAll(filepath.Join(cfgDir, "gg"), 0o755)
	os.WriteFile(filepath.Join(cfgDir, "gg", "config.toml"), []byte(
		"[agents]\nspawn = [\"Sleeper\"]\nmax_spawned = "+strconv.Itoa(cap)+"\n\n"+
			"[[tools.command]]\ncategory = \"session\"\nname = \"Sleeper\"\nmode = \"session\"\n"+
			"command = \"sh -c 'sleep 600' <prompt>\"\n\n"+
			"[[tools.command]]\ncategory = \"session\"\nname = \"Bare\"\nmode = \"session\"\ncommand = \"sh -c 'sleep 600'\"\n"), 0o644)
	main, svc = newRealRepo(t)
	svc.UseSessionRegistryDir(t.TempDir())
	wt = addWT(t, main, "job")
	s, _, err := svc.StartAgentSession(context.Background(), sleeper(), main, "", 80, 24, nil, "http://x", SpawnRecord{}, "")
	if err != nil {
		t.Fatal(err)
	}
	cacheService(main, svc)
	return main, wt, svc, FullSessionID(s.Info().ID)
}

func approveAll(string, string) bool { return true }

func TestSpawnAgentStartsHandsOverAndRecords(t *testing.T) {
	_, wt, _, ov := spawnFixture(t, 4)
	res, sess, err := SpawnAgent(context.Background(), SpawnSpec{Req: AgentStartRequest{Caller: ov, Worktree: wt,
		Tool: "sleeper", Prompt: "fix issue 7", Note: "issue 7"}, Cols: 80, Rows: 24, MCPURL: "http://x", Approved: approveAll})
	if err != nil {
		t.Fatal(err)
	}
	if res.ID != FullSessionID(sess.Info().ID) || res.Warning != "" {
		t.Fatalf("result %+v", res)
	}
	rec, ok := AgentRecord(res.ID)
	if !ok || rec.Parent != ov || rec.Brief != "fix issue 7" || !rec.Spawned {
		t.Fatalf("record %+v", rec)
	}
	c, ok, _ := wtclaim.Read(git.GitDirAt(wt))
	if !ok || c.Session != res.ID || c.Parent != ov || c.Note != "issue 7" {
		t.Fatalf("claim %+v", c)
	}
	if b, err := AgentTask(res.ID); err != nil || b.Brief != "fix issue 7" {
		t.Fatalf("agent_task = %+v %v", b, err)
	}
	if _, err := AgentTask(ov); err == nil {
		t.Fatal("a manual session has no task")
	}
}

func TestSpawnRefusals(t *testing.T) {
	_, wt, _, ov := spawnFixture(t, 1)
	base := SpawnSpec{Req: AgentStartRequest{Caller: ov, Worktree: wt, Tool: "Sleeper", Prompt: "x"}, Cols: 80, Rows: 24, MCPURL: "http://x", Approved: approveAll}
	try := func(mut func(*SpawnSpec)) error {
		sp := base
		mut(&sp)
		_, _, err := SpawnAgent(context.Background(), sp)
		return err
	}
	cases := map[string]struct {
		mut  func(*SpawnSpec)
		want string
	}{
		"dead caller":  {func(sp *SpawnSpec) { sp.Req.Caller = "0-1/s1" }, "not a running agent of this gg"},
		"not allowed":  {func(sp *SpawnSpec) { sp.Req.Tool = "Bare" }, "not in [agents] spawn"},
		"unknown tool": {func(sp *SpawnSpec) { sp.Req.Tool = "Nope" }, "no session command named"},
		"unapproved":   {func(sp *SpawnSpec) { sp.Approved = func(string, string) bool { return false } }, "approve Sleeper once"},
		"empty prompt": {func(sp *SpawnSpec) { sp.Req.Prompt = "" }, "prompt is empty"},
		"huge prompt":  {func(sp *SpawnSpec) { sp.Req.Prompt = strings.Repeat("x", MaxBriefBytes+1) }, "prompt is larger than"},
		"huge note":    {func(sp *SpawnSpec) { sp.Req.Note = strings.Repeat("n", MaxNoteBytes+1) }, "note is larger than"},
		"bad worktree": {func(sp *SpawnSpec) { sp.Req.Worktree = "nowhere" }, "no worktree"},
	}
	for name, c := range cases {
		if err := try(c.mut); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", name, err, c.want)
		}
	}
	// Cap 1: the first start wins, the second is refused.
	res, _, err := SpawnAgent(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	if err := try(func(sp *SpawnSpec) {}); err == nil || !strings.Contains(err.Error(), "max_spawned") {
		t.Fatalf("over the cap = %v", err)
	}
	// A worker may not spawn.
	if err := try(func(sp *SpawnSpec) { sp.Req.Caller = res.ID }); err == nil || !strings.Contains(err.Error(), "may not start agents") {
		t.Fatalf("nested = %v", err)
	}
}

func TestSpawnRefusesUnapprovedCommand(t *testing.T) {
	_, wt, _, ov := spawnFixture(t, 4)
	var asked []string
	_, _, err := SpawnAgent(context.Background(), SpawnSpec{Req: AgentStartRequest{Caller: ov, Worktree: wt, Tool: "Sleeper", Prompt: "x"},
		Cols: 80, Rows: 24, Approved: func(key, cmd string) bool { asked = append(asked, cmd); return false }})
	if err == nil || len(asked) != 1 || asked[0] != "sh -c 'sleep 600' <prompt>" {
		t.Fatalf("approval must be asked for the resolved block's command TEXT: %v %v", asked, err)
	}
	if _, ok, _ := wtclaim.Read(git.GitDirAt(wt)); ok {
		t.Fatal("a refused spawn leaves no claim")
	}
}

func TestFailedWorkerClaimRevertsToOverseer(t *testing.T) {
	main, wt, _, ov := spawnFixture(t, 4)
	os.WriteFile(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "gg", "config.toml"), []byte(
		"[agents]\nspawn = [\"Quick\"]\n\n[[tools.command]]\ncategory = \"session\"\nname = \"Quick\"\nmode = \"session\"\ncommand = \"sh -c 'exit 3' <prompt>\"\n"), 0o644)
	_, sess, err := SpawnAgent(context.Background(), SpawnSpec{Req: AgentStartRequest{Caller: ov, Worktree: wt, Tool: "Quick", Prompt: "x"}, Cols: 80, Rows: 24, Approved: approveAll})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-sess.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the worker never exited")
	}
	infos, _ := ServiceForDir(main).WorktreeInventory(context.Background(), pol(), false)
	if w := find(t, infos, wt); w.Claim() == nil || w.Claim().Session != ov {
		t.Fatalf("a dead worker's claim must revert to the live overseer: %+v", w.Claim())
	}
}

func TestAgentSendTypesAndEnters(t *testing.T) {
	_, wt, svc, ov := spawnFixture(t, 4)
	shTool := sleeper()
	shTool.Command = "sh -i" // a plain interactive shell: typed text runs as a command line
	child, _, err := svc.StartAgentSession(context.Background(), shTool, wt, "", 80, 24, nil, "http://x", SpawnRecord{Parent: ov, Spawned: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	target := FullSessionID(child.Info().ID)
	if err := AgentSend(ov, target, "echo gg-$((6*7))", true, nil); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, text, err := AgentScreen(target)
		if err == nil && strings.Contains(text, "gg-42") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("screen never showed gg-42:\n%s", text)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := AgentSend(target, ov, "x", true, nil); err == nil || !strings.Contains(err.Error(), "not an agent you started") {
		t.Fatalf("send to a non-descendant = %v", err)
	}
	if err := AgentKill(ov, target, true); err != nil {
		t.Fatal(err)
	}
	if err := AgentKill(target, ov, false); err == nil {
		t.Fatal("kill of a non-descendant must refuse")
	}
}

func TestAgentSendRefusesHugeText(t *testing.T) {
	_, wt, svc, ov := spawnFixture(t, 4)
	child, _, err := svc.StartAgentSession(context.Background(), sleeper(), wt, "", 80, 24, nil, "http://x", SpawnRecord{Parent: ov, Spawned: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	target := FullSessionID(child.Info().ID)
	if err := AgentSend(ov, target, strings.Repeat("x", MaxSendBytes+1), true, nil); err == nil || !strings.Contains(err.Error(), "text is larger than") {
		t.Fatalf("huge send = %v", err)
	}
	if err := AgentSend(ov, target, strings.Repeat("x", MaxSendBytes), false, nil); err != nil {
		t.Fatalf("a send at the cap = %v", err)
	}
}

func TestReachNamesAnUnknownSession(t *testing.T) {
	_, _, _, ov := spawnFixture(t, 4)
	ghost := ov[:strings.LastIndex(ov, "/")+1] + "s999"
	for name, err := range map[string]error{
		"send": AgentSend(ov, ghost, "x", true, nil),
		"kill": AgentKill(ov, ghost, false),
	} {
		if err == nil || !strings.Contains(err.Error(), "no session "+ghost) || strings.Contains(err.Error(), "started by") {
			t.Errorf("%s to an unknown id = %v, want \"no session %s\"", name, err, ghost)
		}
	}
}

func TestAgentListMarksMine(t *testing.T) {
	_, wt, _, ov := spawnFixture(t, 4)
	res, _, err := SpawnAgent(context.Background(), SpawnSpec{Req: AgentStartRequest{Caller: ov, Worktree: wt, Tool: "Sleeper", Prompt: "x"}, Cols: 80, Rows: 24, Approved: approveAll})
	if err != nil {
		t.Fatal(err)
	}
	var mine, theirs int
	for _, e := range AgentList(ov) {
		switch e.ID {
		case res.ID:
			if !e.Mine || e.Parent != ov || !e.Spawned || e.Tool != "Sleeper" || e.State != "running" {
				t.Fatalf("worker row %+v", e)
			}
			mine++
		case ov:
			if e.Mine {
				t.Fatal("the caller is not its own descendant")
			}
			theirs++
		}
	}
	if mine != 1 || theirs != 1 {
		t.Fatalf("list %+v", AgentList(ov))
	}
}

func TestParseConsoleKeyName(t *testing.T) {
	for in, want := range map[string]ConsoleKey{
		"enter": {K: "enter"}, "esc": {K: "esc"}, "ctrl+c": {K: "char", Mod: ModCtrl, Text: "c"},
		"1": {K: "char", Text: "1"}, "space": {K: "char", Text: " "}, "shift+tab": {K: "tab", Mod: ModShift},
	} {
		got, err := ParseConsoleKeyName(in)
		if err != nil || got != want {
			t.Errorf("%q = %+v %v, want %+v", in, got, err, want)
		}
	}
	if _, err := ParseConsoleKeyName("ctrl+nope"); err == nil {
		t.Error("unknown key accepted")
	}
}

func TestResolveWorktreeArg(t *testing.T) {
	t.Parallel()
	main, svc := newRealRepo(t)
	wt := addWT(t, main, "feature-x")
	for _, arg := range []string{wt, "feature-x"} {
		got, err := svc.ResolveWorktreeArg(context.Background(), arg)
		if err != nil || !SameCheckout(got, wt) {
			t.Errorf("%q = %q %v", arg, got, err)
		}
	}
	if _, err := svc.ResolveWorktreeArg(context.Background(), "nope"); err == nil {
		t.Error("an unknown name resolved")
	}
}
