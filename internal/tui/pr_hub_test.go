package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/syntax"
)

func hubText(ls []contentLine) string {
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = l.text
	}
	return strings.Join(out, "\n")
}

func TestPRHubHeader(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_010_800, 0) // 3h after testPRs()[0].Updated
	p := testPRs()[0]
	p.Draft = true
	got := hubText(prHubHeader(p, now))
	for _, want := range []string{"#7 Add streaming parser", "open · draft · octocat · feat/x → main · approved · updated 3h ago", "https://github.com/o/r/pull/7"} {
		if !strings.Contains(got, want) {
			t.Errorf("header missing %q:\n%s", want, got)
		}
	}
	merged := testPRs()[1]
	got = hubText(prHubHeader(merged, now))
	if !strings.Contains(got, "merged · hubot · old → main · updated") || strings.Contains(got, "draft") {
		t.Errorf("merged header:\n%s", got)
	}
}

func TestPRHubBody(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_000, 0)
	day := 24 * time.Hour
	p := testPRs()[0]
	p.Body = "First line\r\n\tindented\x1b[31m"
	c := domain.PRComments{
		Hub: []model.ForgeComment{
			{Kind: model.ForgeCommentGeneral, Author: "hubot", Body: "looks good overall", Created: now.Add(-2 * day)},
			{Kind: model.ForgeCommentReview, Author: "octocat", Body: "please split this", Verdict: "changes_requested", Created: now.Add(-day)},
		},
		Outdated: []model.ForgeComment{
			{ID: "t1", Kind: model.ForgeCommentInline, Author: "hubot", Path: "parser.go", Line: 42, Resolved: true,
				Hunk: "@@ -1,9 +1,9 @@\n l1\n l2\n l3\n l4\n l5\n l6\n+	x := 1", Body: "please rename", Created: now.Add(-5 * day)},
			{ID: "t2", ParentID: "t1", Kind: model.ForgeCommentInline, Author: "octocat", Path: "parser.go", Line: 42, Body: "done", Created: now.Add(-4 * day)},
		},
		Truncated: true,
	}
	ls := prHubBody(p, c, now)
	got := hubText(ls)
	for _, want := range []string{
		// The body is markdown: a paragraph line's leading whitespace folds, as
		// it does on the forge (the hunk below still proves tab expansion).
		"Description", "  First line", "  indented·[31m", "+   x := 1",
		"Conversation (2)", "  hubot · 2d ago", "    looks good overall",
		"  octocat · 1d ago · changes requested", "    please split this",
		"Outdated (1)", "  parser.go:42 · hubot · 5d ago · resolved", "    please rename",
		"    ↳ octocat · 4d ago", "      done",
		"truncated",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("body missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\r") || strings.Contains(got, "\x1b") {
		t.Errorf("forge text must be sanitized: %q", got)
	}
	// The hunk is cut to its last 6 lines.
	if strings.Contains(got, " l1") || !strings.Contains(got, " l2") || !strings.Contains(got, "+   x := 1") {
		t.Errorf("hunk snippet:\n%s", got)
	}
	heads := 0
	for _, l := range ls {
		if l.heading {
			heads++
		}
	}
	if heads != 3 {
		t.Errorf("headings = %d, want Description, Conversation, Outdated", heads)
	}
}

func TestPRHubBodyEmpty(t *testing.T) {
	t.Parallel()
	got := hubText(prHubBody(testPRs()[0], domain.PRComments{}, time.Now()))
	if !strings.Contains(got, "(no description)") || !strings.Contains(got, "(no conversation yet)") || strings.Contains(got, "Outdated") {
		t.Fatalf("empty body:\n%s", got)
	}
}

func TestPRHubOpensLoadsAndReturns(t *testing.T) {
	t.Parallel()
	m := prModel(t)
	nm, cmd := m.Update(keyMsg("i"))
	m = nm.(Model)
	hub := layerOf[*prHubPopup](m)
	if hub == nil || cmd == nil {
		t.Fatalf("i must open the hub and start the load (hub=%v cmd=%v)", hub != nil, cmd != nil)
	}
	if got := hubText(hub.lines); !strings.Contains(got, "#7 Add streaming parser") || !strings.Contains(got, "loading") {
		t.Fatalf("opening content:\n%s", got)
	}
	// A load for another PR is dropped.
	nm, _ = m.Update(prHubMsg{number: 12})
	if got := hubText(layerOf[*prHubPopup](nm.(Model)).lines); !strings.Contains(got, "loading") {
		t.Fatal("a stale load for another PR must not fill this hub")
	}
	nm, _ = m.Update(prHubMsg{number: 7, pr: m.prs[0], c: domain.PRComments{}})
	m = nm.(Model)
	if got := hubText(layerOf[*prHubPopup](m).lines); strings.Contains(got, "loading") || !strings.Contains(got, "Conversation") {
		t.Fatalf("filled content:\n%s", got)
	}
	// ctrl+t maximizes; y copies; esc returns to the tab.
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	m = nm.(Model)
	if !layerOf[*prHubPopup](m).maxed() {
		t.Fatal("ctrl+t must maximize the hub")
	}
	if _, cmd := m.Update(keyMsg("y")); cmd == nil {
		t.Fatal("y must copy the PR URL")
	}
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = nm.(Model)
	if layerOf[*prHubPopup](m) != nil || m.focus != panelPRs {
		t.Fatalf("esc must close the hub back onto the tab (focus %v)", m.focus)
	}
}

func TestPRHubLoadFailureOffersRetry(t *testing.T) {
	t.Parallel()
	m := prModel(t)
	nm, _ := m.Update(keyMsg("i"))
	nm, _ = nm.(Model).Update(prHubMsg{number: 7, err: errors.New("HTTP 502\nmore")})
	m = nm.(Model)
	got := hubText(layerOf[*prHubPopup](m).lines)
	if !strings.Contains(got, "HTTP 502") || !strings.Contains(got, "#7 Add streaming parser") {
		t.Fatalf("failure content:\n%s", got)
	}
	nm, cmd := m.Update(keyMsg("r"))
	if cmd == nil || !strings.Contains(hubText(layerOf[*prHubPopup](nm.(Model)).lines), "loading") {
		t.Fatal("r must reload the hub")
	}
}

// Forge prose is markdown: the hub lays the parsed tree out — markers gone,
// styles on the class mask — while a plain-prose body reads exactly as before
// and an outdated thread's HUNK (code, not markdown) stays literal.
func TestPRHubRendersMarkdown(t *testing.T) {
	t.Parallel()
	got := prMarkdownLines("# T\n\n- [x] a **b**\n- see [doc](https://d.x)", "  ")
	var texts []string
	for _, l := range got {
		texts = append(texts, l.text)
		if len(l.cls) != len([]rune(l.text)) {
			t.Fatalf("mask/text mismatch on %q", l.text)
		}
	}
	if want := "  T|  |  ☑ a b|  • see doc (https://d.x)"; strings.Join(texts, "|") != want {
		t.Fatalf("texts = %q, want %q", strings.Join(texts, "|"), want)
	}
	at := func(line int, sub string) syntax.Class {
		i := strings.Index(got[line].text, sub)
		return got[line].cls[len([]rune(got[line].text[:i]))]
	}
	if at(0, "T") != mdHeading || at(2, "b") != mdStrong || at(3, "doc") != mdLink || at(3, "(https") != mdDim || got[2].cls[0] != syntax.Plain {
		t.Fatalf("classes: %v / %v / %v", got[0].cls, got[2].cls, got[3].cls)
	}

	plain := "nice work\nreally\n\nsecond paragraph"
	var a, b []string
	for _, l := range prMarkdownLines(plain, "    ") {
		a = append(a, l.text)
	}
	for _, l := range prTextLines(plain, "    ") {
		b = append(b, strings.TrimRight(l.text, " "))
	}
	for i := range a {
		a[i] = strings.TrimRight(a[i], " ")
	}
	if strings.Join(a, "|") != strings.Join(b, "|") {
		t.Fatalf("plain prose changed:\n got %q\nwant %q", a, b)
	}

	body := prHubBody(model.PullRequest{Number: 7, Body: "**bold** body"},
		domain.PRComments{
			Hub:      []model.ForgeComment{{ID: "H", Author: "bob", Body: "`code` here"}},
			Outdated: []model.ForgeComment{{ID: "O", Author: "eve", Path: "a.go", Line: 3, Hunk: "@@ -1 +1 @@\n-**old**\n+new", Body: "> quoted"}},
		}, time.Now())
	var all []string
	for _, l := range body {
		all = append(all, l.text)
	}
	joined := strings.Join(all, "\n")
	for _, want := range []string{"  bold body", "    code here", "    -**old**", "    │ quoted"} {
		if !strings.Contains(joined, want) {
			t.Errorf("hub body misses %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "`code`") || strings.Contains(joined, "**bold**") {
		t.Errorf("markers survived:\n%s", joined)
	}
}
