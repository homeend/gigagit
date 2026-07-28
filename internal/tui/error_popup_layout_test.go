package tui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// boxLines renders the popup's own box and returns its TEXT AREA lines: the
// content between the borders with the modal's own horizontal padding removed,
// ANSI stripped. The padding has to go — it is on every line by construction,
// so leaving it in would make a right-margin assertion vacuously true. Styling
// is checked separately; every layout assertion here is about columns, so it
// must see the plain text.
func boxLines(t *testing.T, p *contentPopup, m Model) []string {
	t.Helper()
	pad := modalStyle.GetHorizontalPadding() / 2
	var out []string
	for _, l := range strings.Split(strings.TrimRight(p.box(m), "\n"), "\n") {
		s := ansi.Strip(l)
		if !strings.HasPrefix(s, "║") {
			continue // top/bottom border row
		}
		s = strings.TrimSuffix(strings.TrimPrefix(s, "║"), "║")
		if len(s) < 2*pad {
			t.Fatalf("box line narrower than its own padding: %q", s)
		}
		out = append(out, s[pad:len(s)-pad])
	}
	return out
}

// indentOf returns the number of leading spaces on a line, or -1 when the line
// carries no text at all.
func indentOf(s string) int {
	trimmed := strings.TrimLeft(s, " ")
	if trimmed == "" {
		return -1
	}
	return len(s) - len(trimmed)
}

// The message viewer used to indent its three kinds of line by three different
// amounts: the title and the hint sat at the box padding, each message line two
// columns further in, and a WRAPPED message line fell all the way back to the
// padding — so a single wrapped error read as a ragged staircase. Every line
// the box draws now starts in the same column.
func TestMessageViewerAlignsEveryLineAtTheSameMargin(t *testing.T) {
	m := sizedModel(t, 80, 30)
	// Long enough to wrap: the continuation line is the one that used to break
	// the alignment.
	p := newErrorPopup("error: git push failed (exit 128): ssh: Could not resolve hostname " +
		"github-homeend: Temporary failure in name resolution\nfatal: Could not read from remote repository.")

	lines := boxLines(t, p, m)
	want := -1
	for i, l := range lines {
		got := indentOf(l)
		if got < 0 {
			continue // blank separator
		}
		if want < 0 {
			want = got
			continue
		}
		if got != want {
			t.Fatalf("line %d starts at column %d, want %d (every line shares one margin):\n%s",
				i, got, want, strings.Join(lines, "\n"))
		}
	}
	if want <= 0 {
		t.Fatalf("expected the box content to be indented from its border, got %d", want)
	}
}

// The wrapped tail of a message must not run into the box's right edge either:
// the text keeps the same margin on both sides.
func TestMessageViewerKeepsARightMargin(t *testing.T) {
	m := sizedModel(t, 80, 30)
	p := newErrorPopup("error: " + strings.Repeat("z", 400))

	lines := boxLines(t, p, m)
	inner := len(lines[0]) // every rendered line is padded to the box width
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if trailing := len(l) - len(strings.TrimRight(l, " ")); trailing < 1 {
			t.Fatalf("line %d touches the right border (inner width %d):\n%q", i, inner, l)
		}
	}
}

// The message is the popup's subject; the key hints below it are chrome. A
// blank line separates the two so the actions read as their own row rather than
// as one more line of git's stderr.
func TestMessageViewerSeparatesTheActionsFromTheMessage(t *testing.T) {
	m := sizedModel(t, 80, 30)
	p := newErrorPopup("error: git push failed (exit 128)\nfatal: Could not read from remote repository.")

	lines := boxLines(t, p, m)
	hint := -1
	for i, l := range lines {
		if strings.Contains(l, "[q]") {
			hint = i
		}
	}
	if hint < 1 {
		t.Fatalf("hint line not found:\n%s", strings.Join(lines, "\n"))
	}
	if strings.TrimSpace(lines[hint-1]) != "" {
		t.Fatalf("the actions must be separated from the message by a blank line, got %q above them:\n%s",
			lines[hint-1], strings.Join(lines, "\n"))
	}
	if strings.Contains(lines[hint-2], "[q]") || strings.TrimSpace(lines[hint-2]) == "" {
		t.Fatalf("expected the message to end right above the separator:\n%s", strings.Join(lines, "\n"))
	}
}

// The message sits on its own faint background so it reads as the quoted text
// it is, distinct from the popup's title and key hints — which stay on the
// box's own background.
func TestMessageViewerTintsOnlyTheMessage(t *testing.T) {
	forceColor(t)
	m := sizedModel(t, 80, 30)
	p := newErrorPopup("error: git push failed (exit 128)")

	var msg, title, hint string
	for _, l := range strings.Split(p.box(m), "\n") {
		switch {
		case strings.Contains(ansi.Strip(l), "git push failed"):
			msg = l
		case strings.Contains(ansi.Strip(l), "Last error"):
			title = l
		case strings.Contains(ansi.Strip(l), "[q]"):
			hint = l
		}
	}
	if msg == "" || title == "" || hint == "" {
		t.Fatalf("box did not render the expected lines:\n%s", p.box(m))
	}
	bg := "48;5;" + messageBlockColor
	if !strings.Contains(msg, bg) {
		t.Fatalf("the message line must carry the block background %q, got %q", bg, msg)
	}
	for name, l := range map[string]string{"title": title, "hint": hint} {
		if strings.Contains(l, bg) {
			t.Fatalf("the %s line must stay on the box background, got %q", name, l)
		}
	}
}

// The tint spans the whole text area, not just the words: a short message must
// not leave a coloured stub in the middle of the line.
func TestMessageBlockSpansTheBoxWidth(t *testing.T) {
	forceColor(t)
	m := sizedModel(t, 80, 30)
	p := newErrorPopup("error: short\nand a much longer second line of stderr output")

	var short string
	for _, l := range strings.Split(p.box(m), "\n") {
		if strings.Contains(ansi.Strip(l), "error: short") {
			short = l
		}
	}
	if short == "" {
		t.Fatalf("box did not render the short message line:\n%s", p.box(m))
	}
	// The tinted span is what lipgloss wrapped in the background SGR: measure
	// the text it actually covers against the box's own text width.
	span := regexp.MustCompile("\x1b\\[[0-9;]*48;5;" + messageBlockColor + "[0-9;]*m(.*?)\x1b\\[0m").FindStringSubmatch(short)
	if span == nil {
		t.Fatalf("no tinted span on the message line: %q", short)
	}
	if got, want := lipgloss.Width(span[1]), len(boxLines(t, p, m)[0]); got != want {
		t.Fatalf("the tint covers %d columns, want the text area's %d: %q", got, want, span[1])
	}
}

// A viewer whose content is a list of rows (the help window, the files tree)
// keeps its own layout: the tinted block is the prose viewer's shape, not
// every contentPopup's.
func TestOrdinaryContentPopupIsNotTinted(t *testing.T) {
	forceColor(t)
	m := sizedModel(t, 80, 30)
	p := newContentPopup("Help", []contentLine{{text: "row one"}, {text: "row two"}})
	if strings.Contains(p.box(m), "48;5;"+messageBlockColor) {
		t.Fatalf("an ordinary content popup must not be tinted:\n%s", p.box(m))
	}
}

// git separates its stderr paragraphs with blank lines, and a blank row wraps
// to no segments at all — so sizing the window by segment count alone dropped
// one line per blank and clipped the tail off the very message the viewer
// exists to show, even with room to spare.
func TestMessageViewerCountsBlankLinesInItsHeight(t *testing.T) {
	m := sizedModel(t, 96, 24) // far taller than this five-line message needs
	p := newErrorPopup("error: git push failed (exit 128)\nfatal: Could not read from remote repository.\n\n" +
		"Please make sure you have the correct access rights\nand the repository exists.")

	out := strings.Join(boxLines(t, p, m), "\n")
	if !strings.Contains(out, "and the repository exists.") {
		t.Fatalf("the last line of the message must be visible:\n%s", out)
	}
}
