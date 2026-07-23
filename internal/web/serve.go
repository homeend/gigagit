package web

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// Serve runs the probe server on a loopback address until ctx is cancelled
// or the process is interrupted. launch opens the system browser at the
// served URL (best-effort).
func Serve(ctx context.Context, workdir, addr string, launch bool) error {
	svc := domain.Open(workdir)
	// Pre-flight before binding a port or opening a browser: a server whose
	// every request 500s (not a repo; a worktree linked from another
	// environment) must fail loud at startup instead.
	if err := preflight(ctx, svc, workdir); err != nil {
		return err
	}
	ln, url, err := listen(addr)
	if err != nil {
		return err
	}
	httpSrv := &http.Server{Handler: New(svc).Handler()}
	fmt.Fprintln(os.Stderr, "gg web: serving", url)
	if launch {
		openBrowser(url)
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		_ = httpSrv.Shutdown(context.Background())
	}()
	if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// preflight verifies the served directory resolves to a repository. The
// dominant real-world failure is a WORKTREE created in the other
// environment (WSL vs Windows): its .git link file holds a gitdir path the
// local git cannot read ("fatal: not a git repository: (NULL)"), so the
// error names that case explicitly.
func preflight(ctx context.Context, svc *domain.Service, workdir string) error {
	tctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if _, err := svc.TopLevel(tctx); err == nil {
		return nil
	} else if gitFile := filepath.Join(workdir, ".git"); isWorktreeLink(gitFile) {
		return fmt.Errorf("%s is a linked worktree whose .git file points at a path this environment's git cannot read (created under WSL vs Windows?)\nServe a native checkout instead (the main worktree works from both), or run gg web from the environment that created this worktree.\nUnderlying error: %w", workdir, err)
	} else {
		return fmt.Errorf("not a git repository: %s\nRun gg web from inside a repository, or cd there first.\nUnderlying error: %w", workdir, err)
	}
}

// isWorktreeLink reports whether path is a .git FILE (a worktree's gitdir
// link) rather than a real .git directory.
func isWorktreeLink(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	if fi.IsDir() {
		return false
	}
	b, err := os.ReadFile(path)
	return err == nil && strings.HasPrefix(strings.TrimSpace(string(b)), "gitdir:")
}

// listen binds addr (default 127.0.0.1:0) and returns the listener plus the
// browsable URL. Non-loopback hosts are refused: the API exposes repository
// contents with no authentication.
func listen(addr string) (net.Listener, string, error) {
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, "", fmt.Errorf("invalid --addr %q: %w", addr, err)
	}
	if !isLoopbackHost(host) {
		return nil, "", fmt.Errorf("--addr must be loopback (127.0.0.1, ::1 or localhost); got %q", host)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, "", err
	}
	return ln, "http://" + ln.Addr().String(), nil
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch {
	case runtime.GOOS == "darwin":
		cmd = exec.Command("open", url)
	case runtime.GOOS == "windows":
		cmd = exec.Command("cmd", "/c", "start", "", url)
	case isWSL():
		cmd = exec.Command("cmd.exe", "/c", "start", "", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

// isWSL reports whether we are on Linux under WSL, where the Windows
// browser (via cmd.exe) is the right target. Reads the kernel osrelease
// like internal/clipboard's detection (that helper is unexported).
func isWSL() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	b, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return false
	}
	s := strings.ToLower(string(b))
	return strings.Contains(s, "microsoft") || strings.Contains(s, "wsl")
}
