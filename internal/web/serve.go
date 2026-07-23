package web

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
)

// Serve runs the probe server on a loopback address until ctx is cancelled
// or the process is interrupted. launch opens the system browser at the
// served URL (best-effort).
func Serve(ctx context.Context, workdir, addr string, launch bool) error {
	ln, url, err := listen(addr)
	if err != nil {
		return err
	}
	svc := domain.Open(workdir)
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
