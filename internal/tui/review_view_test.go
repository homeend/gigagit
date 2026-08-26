package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestReviewViewRendersAndScrolls(t *testing.T) {
	t.Parallel()
	rv := newReviewView("Review: main..HEAD", "/tmp/x.md", "line1\nline2\nline3\n")
	m := Model{width: 80, height: 24}
	out := rv.render(m, "")
	if !strings.Contains(out, "line1") || !strings.Contains(out, "Review: main..HEAD") {
		t.Fatalf("render missing content/title:\n%s", out)
	}
}

func TestReviewViewEscPops(t *testing.T) {
	t.Parallel()
	rv := newReviewView("Review: main..HEAD", "/tmp/x.md", "line1\nline2\nline3\n")
	m := Model{width: 80, height: 24}
	m = m.pushLayer(rv)
	m2, _ := rv.update(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m2.topLayer() != nil {
		t.Fatalf("esc should pop the review layer")
	}
}

func TestReviewViewIsFullScreen(t *testing.T) {
	t.Parallel()
	if !isFullScreenLayer(&reviewView{}) {
		t.Fatal("reviewView must be a full-screen layer")
	}
}

func TestReviewViewScrollDownAndUp(t *testing.T) {
	t.Parallel()
	content := strings.Repeat("body line\n", 50)
	rv := newReviewView("Review", "/tmp/x.md", content)
	// Small terminal so the body window is smaller than the content.
	m := Model{width: 40, height: 10}
	m = m.pushLayer(rv)

	m, _ = rv.update(m, keyMsg("down"))
	if rv.scroll != 1 {
		t.Fatalf("down should advance scroll by 1, got %d", rv.scroll)
	}
	m, _ = rv.update(m, keyMsg("up"))
	if rv.scroll != 0 {
		t.Fatalf("up should retreat scroll by 1, got %d", rv.scroll)
	}

	m, _ = rv.update(m, tea.KeyMsg{Type: tea.KeyEnd})
	if rv.scroll == 0 {
		t.Fatalf("end should scroll to the bottom, got %d", rv.scroll)
	}
	endScroll := rv.scroll

	m, _ = rv.update(m, tea.KeyMsg{Type: tea.KeyHome})
	if rv.scroll != 0 {
		t.Fatalf("home should scroll to the top, got %d", rv.scroll)
	}

	m, _ = rv.update(m, tea.KeyMsg{Type: tea.KeyPgDown})
	if rv.scroll == 0 {
		t.Fatalf("pgdown should advance scroll")
	}
	m, _ = rv.update(m, tea.KeyMsg{Type: tea.KeyPgUp})
	if rv.scroll != 0 {
		t.Fatalf("pgup back to top should be 0, got %d", rv.scroll)
	}
	_ = endScroll
}

func TestReviewViewScrollClampsToContent(t *testing.T) {
	t.Parallel()
	rv := newReviewView("Review", "/tmp/x.md", "only one line\n")
	m := Model{width: 80, height: 24}
	m = m.pushLayer(rv)
	m, _ = rv.update(m, tea.KeyMsg{Type: tea.KeyEnd})
	if rv.scroll != 0 {
		t.Fatalf("scroll should clamp to 0 when content fits on screen, got %d", rv.scroll)
	}
}

func TestReviewViewEditorOpensReportFile(t *testing.T) {
	t.Parallel()
	rv := newReviewView("Review", "/tmp/report.md", "line1\n")
	m := Model{width: 80, height: 24}
	m = m.pushLayer(rv)
	_, cmd := rv.update(m, keyMsg("e"))
	if cmd == nil {
		t.Fatal("e should return a command to open the report in the editor")
	}
}

// z cycles the long-line layout through cutoff → wrap → scroll → cutoff.
func TestReviewViewZCyclesMode(t *testing.T) {
	t.Parallel()
	rv := newReviewView("Review", "/tmp/x.md", "line\n")
	m := Model{width: 40, height: 10}
	m = m.pushLayer(rv)
	if rv.mode != modeCutoff {
		t.Fatalf("default mode = %v, want modeCutoff", rv.mode)
	}
	m, _ = rv.update(m, keyMsg("ctrl+w"))
	if rv.mode != modeWrap {
		t.Fatalf("after one z, mode = %v, want modeWrap", rv.mode)
	}
	m, _ = rv.update(m, keyMsg("ctrl+w"))
	if rv.mode != modeScroll {
		t.Fatalf("after two z, mode = %v, want modeScroll", rv.mode)
	}
	m, _ = rv.update(m, keyMsg("ctrl+w"))
	if rv.mode != modeCutoff {
		t.Fatalf("after three z, mode = %v, want modeCutoff (wrap-around)", rv.mode)
	}
}

// In wrap mode a long line's tail — cut off in cutoff mode — becomes visible on
// a wrapped continuation line.
func TestReviewViewWrapShowsTail(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("A", 30) + "TAILWORD"
	rv := newReviewView("Review", "/tmp/x.md", long+"\n")
	m := Model{width: 20, height: 10} // narrower than the line: the tail is off-screen in cutoff
	m = m.pushLayer(rv)

	if strings.Contains(rv.render(m, ""), "TAILWORD") {
		t.Fatal("cutoff mode should truncate the tail away")
	}
	m, _ = rv.update(m, keyMsg("ctrl+w")) // → modeWrap
	if rv.mode != modeWrap {
		t.Fatalf("expected modeWrap, got %v", rv.mode)
	}
	if !strings.Contains(rv.render(m, ""), "TAILWORD") {
		t.Fatalf("wrap mode must reveal the wrapped tail:\n%s", rv.render(m, ""))
	}
}

// Wrap mode inflates the display-line count, and scroll still clamps to it.
func TestReviewViewWrapScrollClamps(t *testing.T) {
	t.Parallel()
	// One logical line far wider than the box → many wrapped display lines.
	rv := newReviewView("Review", "/tmp/x.md", strings.Repeat("W", 400)+"\n")
	m := Model{width: 20, height: 10}
	m = m.pushLayer(rv)
	m, _ = rv.update(m, keyMsg("ctrl+w")) // → modeWrap
	m, _ = rv.update(m, tea.KeyMsg{Type: tea.KeyEnd})
	body := rv.bodyRows(m)
	dl := rv.displayLines(20)
	wantMax := rv.maxScroll(body, len(dl))
	if rv.scroll != wantMax {
		t.Fatalf("end must clamp to maxScroll: scroll=%d want=%d (dl=%d body=%d)", rv.scroll, wantMax, len(dl), body)
	}
	if wantMax == 0 {
		t.Fatal("a 400-wide wrapped line must overflow a 20-wide/short box")
	}
	// Over-scrolling past the end still clamps back.
	m, _ = rv.update(m, tea.KeyMsg{Type: tea.KeyPgDown})
	if rv.scroll > wantMax {
		t.Fatalf("scroll must never exceed maxScroll, got %d > %d", rv.scroll, wantMax)
	}
}

func TestReviewViewSlashSearchJumpsToMatch(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	for i := 0; i < 40; i++ {
		b.WriteString("filler\n")
	}
	b.WriteString("NEEDLE here\n")
	for i := 0; i < 40; i++ {
		b.WriteString("filler\n")
	}
	rv := newReviewView("Review", "/tmp/x.md", b.String())
	m := Model{width: 40, height: 10}
	m = m.pushLayer(rv)

	m, _ = rv.update(m, keyMsg("/"))
	if !rv.typing {
		t.Fatal("/ should enter search-typing mode")
	}
	m, _ = rv.update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("NEEDLE")})
	if rv.query != "NEEDLE" {
		t.Fatalf("typed runes should build the query, got %q", rv.query)
	}
	m, _ = rv.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if rv.typing {
		t.Fatal("enter should commit the search and leave typing mode")
	}
	if rv.lines[rv.scroll] != "NEEDLE here" {
		t.Fatalf("enter should jump scroll to the matching line, got scroll=%d line=%q", rv.scroll, rv.lines[rv.scroll])
	}
}
