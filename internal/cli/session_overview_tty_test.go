//go:build !windows

package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/xpty"
)

// An agent that forgets --file and pipes nothing must not hang on the
// terminal's stdin: it is told how to give the text.
func TestSessionOverviewAddRefusesATerminalStdin(t *testing.T) {
	t.Parallel()
	p, err := xpty.NewUnixPty(80, 24)
	if err != nil {
		t.Skipf("no pty: %v", err)
	}
	defer p.Close()
	tty, err := os.OpenFile(p.SlaveName(), os.O_RDWR, 0)
	if err != nil {
		t.Skipf("open the pty: %v", err)
	}
	defer tty.Close()
	dir := t.TempDir()
	livePresence(t, dir)
	var out, errb bytes.Buffer
	done := make(chan int, 1)
	go func() { done <- runSessionIn(dir, nil, []string{"overview", "add", "--title", "T"}, tty, &out, &errb) }()
	select {
	case code := <-done:
		if code != 2 || !strings.Contains(errb.String(), "give the text with --file or on stdin") {
			t.Fatalf("exit %d stderr %q", code, errb.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("overview add waited on the terminal for text")
	}
}
