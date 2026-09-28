package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// The notice dialog's width follows its content, and nothing inside it is
// cut: the maintainer's drift notice had its title truncated with "…" while
// ctrl+t stretched the box across the whole terminal with two thirds of it
// empty. Every test renders the real box through noticePopup.box.

const longNoticeTitle = "feat/some-long-feature-name's change set may have drifted from its recorded version"

const longNoticePath = "packages/frontend/src/components/settings/notifications/NotificationPreferencesPanel.stories.tsx"

func driftShapedNotice() notice {
	return notice{
		id:    "n1",
		title: longNoticeTitle,
		detail: []string{
			"feat/some-long-feature-name now introduces changes its recorded version did not:",
			"  A " + longNoticePath,
			"Open Branch versions to compare it against what gg recorded before this operation.",
		},
		actions: []noticeAction{{label: "Dismiss"}},
	}
}

func noticeBoxAt(width int, showActions, maximized bool) string {
	m := Model{}
	m.width, m.height = width, 40
	m.notices = []notice{driftShapedNotice()}
	p := &noticePopup{showActions: showActions}
	p.maximized = maximized
	return plain(p.box(m))
}

func noticeBoxLines(box string) []string {
	var out []string
	for _, l := range strings.Split(box, "\n") {
		l = strings.TrimSpace(strings.Trim(strings.TrimSpace(l), "║"))
		out = append(out, strings.TrimSpace(l))
	}
	return out
}

func TestNoticeDetailTitleWrapsInsteadOfTruncating(t *testing.T) {
	t.Parallel()
	box := noticeBoxAt(80, true, false)
	if strings.Contains(box, "…version") || strings.Contains(box, "its …") {
		t.Fatalf("title was truncated:\n%s", box)
	}
	if !strings.Contains(box, "recorded version") {
		t.Fatalf("title's tail must survive by wrapping:\n%s", box)
	}
}

func TestNoticeDetailPathLineKeepsIndentAndElidesMiddle(t *testing.T) {
	t.Parallel()
	box := noticeBoxAt(80, true, false)
	var pathLine string
	for _, l := range noticeBoxLines(box) {
		if strings.HasPrefix(l, "A ") {
			pathLine = l
		}
		if l == "A" {
			t.Fatalf("status letter must not be split from its path:\n%s", box)
		}
	}
	if pathLine == "" {
		t.Fatalf("no status+path line rendered:\n%s", box)
	}
	if !strings.HasSuffix(pathLine, "NotificationPreferencesPanel.stories.tsx") {
		t.Fatalf("file name must survive at the end: %q", pathLine)
	}
	if !strings.Contains(pathLine, "…") {
		t.Fatalf("a path that does not fit is middle-elided, not wrapped: %q", pathLine)
	}
}

func TestNoticeRootLevelPathIsMiddleElidedNotEndCut(t *testing.T) {
	t.Parallel()
	m := Model{}
	m.width, m.height = 80, 40
	name := "A-really-long-root-level-generated-file-name-with-no-directory-at-all.snapshot.tsx"
	m.notices = []notice{{id: "n", title: "t", detail: []string{"  A " + name}, actions: []noticeAction{{label: "Dismiss"}}}}
	p := &noticePopup{showActions: true}
	var row string
	for _, l := range noticeBoxLines(plain(p.box(m))) {
		if strings.HasPrefix(l, "A ") {
			row = l
		}
	}
	// elideNameMiddle keeps the beginning and the (last) extension.
	if row == "" || !strings.HasPrefix(row, "A A-really-long") || !strings.Contains(row, "…") || !strings.HasSuffix(row, ".tsx") {
		t.Fatalf("a bare name keeps its beginning and extension, never end-cut: %q", row)
	}
}

func TestNoticeNormalWidthGrowsWithContentUpToThreeQuarters(t *testing.T) {
	t.Parallel()
	const termW = 200
	got := lipgloss.Width(noticeBoxAt(termW, true, false))
	wide := popupWideInnerWidth(termW) + 2 // frame = inner + borders
	if got <= wide {
		t.Fatalf("a long notice must widen the box past the fixed %d columns, got %d", wide, got)
	}
	if got > termW*3/4+2 {
		t.Fatalf("normal box must stay within three quarters of the terminal: %d of %d", got, termW)
	}
}

func TestNoticeShortContentKeepsTodaysWidth(t *testing.T) {
	t.Parallel()
	m := Model{}
	m.width, m.height = 200, 40
	m.notices = []notice{{id: "n", title: "short", detail: []string{"one line"}, actions: []noticeAction{{label: "Dismiss"}}}}
	p := &noticePopup{showActions: true}
	got := lipgloss.Width(plain(p.box(m)))
	if want := popupWideInnerWidth(200) + 2; got != want {
		t.Fatalf("short content must render at the usual wide box: got %d, want %d", got, want)
	}
}

func TestNoticeMaximizedFitsContentNotTerminal(t *testing.T) {
	t.Parallel()
	const termW = 200
	maxed := lipgloss.Width(noticeBoxAt(termW, true, true))
	full := popupFullInnerWidth(termW) + 2
	if maxed >= full {
		t.Fatalf("maximized box must fit its content, not the terminal: %d (full would be %d)", maxed, full)
	}
	// Content wider than three quarters of the terminal: normal is capped,
	// maximized is allowed to grow further (but still only to the content).
	normal := lipgloss.Width(noticeBoxAt(120, true, false))
	maxedNarrow := lipgloss.Width(noticeBoxAt(120, true, true))
	if maxedNarrow <= normal {
		t.Fatalf("maximizing must buy room when the normal cap cut the content: %d vs %d", maxedNarrow, normal)
	}
	if maxedNarrow > popupFullInnerWidth(120)+2 {
		t.Fatalf("maximized never exceeds the terminal: %d", maxedNarrow)
	}
}

func TestNoticeMaximizedNeverNarrowerThanNormal(t *testing.T) {
	t.Parallel()
	m := Model{}
	m.width, m.height = 200, 40
	m.notices = []notice{{id: "n", title: "short", actions: []noticeAction{{label: "Dismiss"}}}}
	p := &noticePopup{showActions: true}
	normal := lipgloss.Width(plain(p.box(m)))
	p.maximized = true
	if got := lipgloss.Width(plain(p.box(m))); got != normal {
		t.Fatalf("short content: maximized width %d must equal normal %d", got, normal)
	}
}

func TestNoticeListWidthFollowsTitles(t *testing.T) {
	t.Parallel()
	m := Model{}
	m.width, m.height = 200, 40
	title := "feat/a-really-long-branch-name-that-someone-actually-typed's change set may have drifted from its recorded version"
	m.notices = []notice{{id: "n", title: title}}
	box := plain((&noticePopup{}).box(m))
	if got := lipgloss.Width(box); got <= popupWideInnerWidth(200)+2 {
		t.Fatalf("the list widens for a long title too, got %d", got)
	}
	if strings.Contains(box, "…") {
		t.Fatalf("a title that fits the widened list is not cut:\n%s", box)
	}
}
