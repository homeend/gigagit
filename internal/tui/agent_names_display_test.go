package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

func TestSessionRowsShowTheName(t *testing.T) {
	t.Parallel()
	named := domain.SessionInfo{ID: "zz-named", Label: "Claude (yolo)", Name: "viewer", Dir: "/r/wt", Repo: "r", State: domain.SessionRunning, Started: time.Now()}
	plain := domain.SessionInfo{ID: "zz-plain", Label: "Junie", Dir: "/r/wt", Repo: "r", State: domain.SessionExited}
	if got := sessionRowBody(named); !strings.HasPrefix(got, "└ ● Claude (yolo) [viewer]  ") {
		t.Fatalf("named row %q", got)
	}
	if got := sessionRowBody(plain); !strings.HasPrefix(got, "└ ○ Junie  ") {
		t.Fatalf("unnamed row %q", got)
	}
	if label, _, _ := consoleTitleParts(named); label != "Claude (yolo) [viewer]" {
		t.Fatalf("console title %q", label)
	}
	rows, _ := sessionsPopupRows([]domain.SessionInfo{named, plain}, "viewer")
	joined := strings.Join(rows, "\n")
	if !strings.Contains(joined, "Claude (yolo) [viewer]") || strings.Contains(joined, "Junie") {
		t.Fatalf("filtering on the name finds only the named session:\n%s", joined)
	}
}
