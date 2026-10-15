package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

func runSessionAt(t *testing.T, dir string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	svc := domain.Open(newCLIRepo(t))
	code := runSession(dir, svc, args, &out, &errb)
	return code, out.String(), errb.String()
}

// withSessionEnv runs fn with sessionGetenv answering from env (and "" for
// every other key). Not parallel-safe: callers must not t.Parallel().
func withSessionEnv(t *testing.T, env map[string]string, fn func()) {
	t.Helper()
	old := sessionGetenv
	sessionGetenv = func(k string) string { return env[k] }
	defer func() { sessionGetenv = old }()
	fn()
}

func liveIDEPresence(t *testing.T, dir string) {
	t.Helper()
	if err := steer.Touch(dir, steer.IDEPresence, steer.Presence{PID: 99, Worktree: "/w", Started: time.Now().UTC().Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	keepPresenceAlive(t, dir, steer.IDEPresence)
}

func answerIDE(t *testing.T, dir string, reply func(steer.Command) steer.Reply) <-chan steer.Command {
	t.Helper()
	return answer(t, steer.IDEDir(dir), reply)
}

// --to ide posts to <inbox>/ide/ and awaits the reply there.
func TestSessionToIDEPostsToTheSubInbox(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	liveIDEPresence(t, dir)
	got := answerIDE(t, dir, func(c steer.Command) steer.Reply {
		return steer.Reply{ID: c.ID, OK: true, Detail: "opened a.go at line 3"}
	})
	code, out, errs := runSessionAt(t, dir, "navigate", "--file", "a.go", "--new-line", "3", "--to", "ide")
	if code != 0 || !strings.Contains(out, "opened a.go at line 3") {
		t.Fatalf("exit %d out %q err %q", code, out, errs)
	}
	c := <-got
	if c.Cmd != "navigate" || c.File != "a.go" || c.Line == nil || c.Line.No != 3 || !c.Wait {
		t.Fatalf("command = %+v", c)
	}
	if entries, _ := filepath.Glob(filepath.Join(dir, "cmd-*.json")); len(entries) != 0 {
		t.Fatalf("a --to ide command reached the TUI's inbox: %v", entries)
	}
}

// Review focus 1: --to ide with only a TUI live refuses and posts nothing.
func TestSessionToIDERefusesWhenOnlyTUIIsLive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	code, _, errs := runSessionAt(t, dir, "navigate", "--file", "a.go", "--new-line", "1", "--to", "ide")
	if code != 1 || !strings.Contains(errs, "no IDE for this worktree") {
		t.Fatalf("exit %d err %q", code, errs)
	}
	if entries, _ := filepath.Glob(filepath.Join(dir, "cmd-*.json")); len(entries) != 0 {
		t.Fatalf("refused command still reached the TUI: %v", entries)
	}
}

// Never implicit: an ide.json alone makes no verb go anywhere without --to.
func TestSessionIgnoresIDEWithoutTo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	liveIDEPresence(t, dir)
	code, _, errs := runSessionAt(t, dir, "navigate", "--file", "a.go", "--new-line", "1")
	if code != 1 || !strings.Contains(errs, "no gg session for this worktree") {
		t.Fatalf("exit %d err %q", code, errs)
	}
}

// GG_SESSION_TO=ide is the default --to; an explicit --to wins (review
// focus 2); a bogus value is a usage error. Swaps a package variable: not
// parallel.
func TestSessionEnvDefaultAndExplicitTo(t *testing.T) {
	dir := t.TempDir()
	liveIDEPresence(t, dir)
	livePresence(t, dir)
	tui := answer(t, dir, func(c steer.Command) steer.Reply { return steer.Reply{ID: c.ID, OK: true, Detail: "tui"} })
	ide := answerIDE(t, dir, func(c steer.Command) steer.Reply { return steer.Reply{ID: c.ID, OK: true, Detail: "ide"} })
	withSessionEnv(t, map[string]string{"GG_SESSION_TO": "ide"}, func() {
		if code, out, _ := runSessionAt(t, dir, "navigate", "--file", "a.go", "--new-line", "1"); code != 0 || !strings.Contains(out, "ide") {
			t.Fatalf("env default: exit %d out %q", code, out)
		}
		<-ide
		if code, out, _ := runSessionAt(t, dir, "navigate", "--file", "a.go", "--new-line", "1", "--to", "tui"); code != 0 || !strings.Contains(out, "tui") {
			t.Fatalf("explicit --to tui: exit %d out %q", code, out)
		}
		<-tui
	})
	withSessionEnv(t, map[string]string{"GG_SESSION_TO": "vim"}, func() {
		if code, _, errs := runSessionAt(t, dir, "navigate", "--file", "a.go", "--new-line", "1"); code != 2 || !strings.Contains(errs, "GG_SESSION_TO") {
			t.Fatalf("bogus env: exit %d err %q", code, errs)
		}
	})
}

// --to takes ide now; a bogus side names all three.
func TestCutToAcceptsIDE(t *testing.T) {
	t.Parallel()
	rest, to, err := cutTo([]string{"navigate", "--to", "ide", "--file", "a.go"})
	if err != nil || to != "ide" || len(rest) != 3 {
		t.Fatalf("cutTo = %v %q %v", rest, to, err)
	}
	if _, _, err := cutTo([]string{"navigate", "--to", "vim"}); err == nil || !strings.Contains(err.Error(), "ide") {
		t.Fatalf("bogus --to: %v", err)
	}
}

// Review focus 4: status with only the IDE live.
func TestSessionStatusPrintsTheLiveIDE(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := steer.Touch(dir, steer.IDEPresence, steer.Presence{PID: 99, Worktree: "/w", Started: "2026-10-15T12:00:00Z", IDE: "IntelliJ IDEA 2026.2", Plugin: "0.2.0"}); err != nil {
		t.Fatal(err)
	}
	code, out, _ := runSessionAt(t, dir, "status")
	if code != 0 || !strings.Contains(out, "worktree: /w") || !strings.Contains(out, "ide: pid 99, IntelliJ IDEA 2026.2 0.2.0 (since 2026-10-15T12:00:00Z)") {
		t.Fatalf("exit %d out %q", code, out)
	}
	code, out, _ = runSessionAt(t, dir, "status", "--json")
	if code != 0 || !strings.Contains(out, `"ide": {`) || !strings.Contains(out, `"ide": "IntelliJ IDEA 2026.2"`) || !strings.Contains(out, `"plugin": "0.2.0"`) || !strings.Contains(out, `"tui": null`) || !strings.Contains(out, `"web": null`) {
		t.Fatalf("json: exit %d out %s", code, out)
	}
}
