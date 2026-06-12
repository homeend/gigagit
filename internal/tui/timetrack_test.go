package tui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/observ"
)

// TestStartOpEmitsOpSpan drives a real op through the TUI plumbing and
// asserts the sink got an "op …" span (emitted from the op goroutine — the
// -race gate covers the concurrency).
func TestStartOpEmitsOpSpan(t *testing.T) {
	var buf bytes.Buffer
	observ.SetSpanSink(&buf)
	t.Cleanup(func() { observ.SetSpanSink(nil) })

	dir, repo := newRepoDir(t)
	runGit(t, dir, "branch", "feat/spanned")
	m := loadModel(t, repo)
	m.focus = panelBranches
	for vi := 0; vi < m.panelLen(panelBranches); vi++ {
		m.sel[panelBranches] = vi
		if bi, ok := m.backingIndex(panelBranches); ok && m.branches[bi].Name == "feat/spanned" {
			break
		}
	}

	// d on the Branches panel -> DeleteBranch op; answer the confirm modal.
	updated, cmd := m.Update(keyMsg("d"))
	m = updated.(Model)
	for i := 0; i < 100 && m.running; i++ {
		if m.modal != nil {
			u, _ := m.Update(keyMsg("enter")) // option 0 = "delete"
			m = u.(Model)
			continue
		}
		if cmd == nil {
			t.Fatal("no command but op still running")
		}
		u, next := m.Update(cmd())
		m = u.(Model)
		cmd = next
	}

	if !strings.Contains(buf.String(), `"name":"op DeleteBranch"`) {
		t.Fatalf("sink missing the op span:\n%s", buf.String())
	}
}
