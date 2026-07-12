package tui

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

func fakeEnv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestShellEscapeBin(t *testing.T) {
	cases := []struct {
		goos string
		env  map[string]string
		want string
	}{
		{"linux", map[string]string{"SHELL": "/usr/bin/zsh"}, "/usr/bin/zsh"},
		{"linux", map[string]string{}, "/bin/sh"},
		{"darwin", map[string]string{}, "/bin/sh"},
		{"windows", map[string]string{"COMSPEC": `C:\WINDOWS\system32\cmd.exe`}, `C:\WINDOWS\system32\cmd.exe`},
		{"windows", map[string]string{}, "cmd"},
	}
	for _, c := range cases {
		if got := shellEscapeBin(c.goos, fakeEnv(c.env)); got != c.want {
			t.Errorf("shellEscapeBin(%s, %v) = %q, want %q", c.goos, c.env, got, c.want)
		}
	}
}

func TestSubshellExecPosix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shape")
	}
	cmd, script, err := subshellExec("/some/worktree", fakeEnv(map[string]string{"SHELL": "/usr/bin/zsh"}))
	if err != nil {
		t.Fatalf("subshellExec: %v", err)
	}
	defer os.Remove(script)
	if script == "" {
		t.Fatal("POSIX subshell must use a wrapper script")
	}
	body, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "gg subshell — 'exit' returns to gg") {
		t.Fatalf("script missing the banner:\n%s", body)
	}
	if !strings.Contains(string(body), `exec "${SHELL:-/bin/sh}"`) {
		t.Fatalf("script missing the exec line:\n%s", body)
	}
	if len(cmd.Args) != 2 || cmd.Args[0] != "/usr/bin/zsh" || cmd.Args[1] != script {
		t.Fatalf("argv = %v, want [/usr/bin/zsh <script>]", cmd.Args)
	}
	if cmd.Dir != "/some/worktree" {
		t.Fatalf("Dir = %q", cmd.Dir)
	}
	found := false
	for _, e := range cmd.Env {
		if e == "GG=1" {
			found = true
		}
	}
	if !found {
		t.Fatal("env must carry GG=1")
	}
}

func TestShellCommandExecPosix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shape")
	}
	cmd, script, err := shellCommandExec("/some/worktree", "git cherry-pick --skip", fakeEnv(nil))
	if err != nil {
		t.Fatalf("shellCommandExec: %v", err)
	}
	defer os.Remove(script)
	body, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	if !strings.Contains(s, "git cherry-pick --skip\n") {
		t.Fatalf("script missing the command:\n%s", s)
	}
	if !strings.Contains(s, "[exit %s] press enter to return to gg") {
		t.Fatalf("script missing the pause line:\n%s", s)
	}
	if !strings.Contains(s, "read -r _ </dev/tty") {
		t.Fatalf("script missing the tty read:\n%s", s)
	}
	if !strings.Contains(s, `exit "$rc"`) {
		t.Fatalf("script must propagate the command's exit code:\n%s", s)
	}
	if len(cmd.Args) != 2 || cmd.Args[0] != "/bin/sh" || cmd.Args[1] != script {
		t.Fatalf("argv = %v, want [/bin/sh <script>]", cmd.Args)
	}
	if cmd.Dir != "/some/worktree" {
		t.Fatalf("Dir = %q", cmd.Dir)
	}
}

func TestCtrlOOpensSubshellFromAnywhere(t *testing.T) {
	// Bare panels.
	m := footerModel()
	mm, cmd := m.Update(keyMsg("ctrl+o"))
	m = mm.(Model)
	if cmd == nil {
		t.Fatal("ctrl+o on the panels must return the handover cmd")
	}
	// Over an open popup layer (the switcher) — stack untouched.
	m = shellEscTestModelWithLayer()
	mm, cmd = m.Update(keyMsg("ctrl+o"))
	m = mm.(Model)
	if cmd == nil || m.topLayer() == nil {
		t.Fatalf("ctrl+o over a popup must hand over AND keep the stack (cmd=%v top=%v)", cmd, m.topLayer())
	}
	// Over the conflict process — the motivating emergency.
	m = footerModel()
	m.proc = &conflictProcess{st: confListing}
	mm, cmd = m.Update(keyMsg("ctrl+o"))
	m = mm.(Model)
	if cmd == nil || m.proc == nil {
		t.Fatalf("ctrl+o over the conflict process must hand over AND keep the process (cmd=%v proc=%v)", cmd, m.proc)
	}
}

// shellEscTestModelWithLayer builds a model with one popup on the stack.
func shellEscTestModelWithLayer() Model {
	m := footerModel()
	m.width, m.height = 100, 30
	m = m.pushLayer(newShelfPopup(nil))
	return m
}

func TestCtrlOBusyNotices(t *testing.T) {
	m := footerModel()
	m.running = true
	mm, cmd := m.Update(keyMsg("ctrl+o"))
	m = mm.(Model)
	if cmd != nil || !strings.Contains(m.statusMsg, "an operation is running") {
		t.Fatalf("busy ctrl+o must notice, not hand over (cmd=%v msg=%q)", cmd, m.statusMsg)
	}
}

func TestShellDoneReloadsAndCleans(t *testing.T) {
	m := footerModel()
	f, err := os.CreateTemp(t.TempDir(), "gg-shell-*.sh")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	mm, cmd := m.Update(shellDoneMsg{script: f.Name()})
	m = mm.(Model)
	if _, err := os.Stat(f.Name()); !os.IsNotExist(err) {
		t.Fatalf("wrapper script must be removed, stat err=%v", err)
	}
	if m.statusMsg != "returned from shell" || cmd == nil {
		t.Fatalf("return path must set the status and reload (msg=%q cmd=%v)", m.statusMsg, cmd)
	}
}

func TestShellDoneExitErrorIsNotAnError(t *testing.T) {
	m := footerModel()
	exitErr := &exec.ExitError{}
	mm, _ := m.Update(shellDoneMsg{err: exitErr})
	m = mm.(Model)
	if m.statusMsg != "returned from shell" {
		t.Fatalf("a non-zero shell exit is not an error, got %q", m.statusMsg)
	}
	// A genuine launch failure IS an error.
	m = footerModel()
	mm, cmd := m.Update(shellDoneMsg{err: errors.New("fork/exec: no such file")})
	m = mm.(Model)
	if cmd != nil || !strings.HasPrefix(m.statusMsg, "shell: ") {
		t.Fatalf("launch failure must surface (msg=%q)", m.statusMsg)
	}
}
