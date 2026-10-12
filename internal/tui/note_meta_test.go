package tui

import (
	"reflect"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/notemeta"
	"github.com/homeend/gigagit/internal/preflight"
	"github.com/homeend/gigagit/internal/theme"
)

func TestMetaClassPaints(t *testing.T) {
	t.Parallel()
	s := buildStyles(theme.Dark)
	base := lipgloss.NewStyle()
	if metaClass("crimson", notemeta.StyleText) != 0 {
		t.Fatal("unknown colour must be class 0")
	}
	txt := s.metaStyle(base, metaClass("red", notemeta.StyleText))
	if txt.GetForeground() != lipgloss.Color(theme.Dark.MetaRed) {
		t.Fatalf("text fg = %v", txt.GetForeground())
	}
	fill := s.metaStyle(base, metaClass("green", notemeta.StyleFilled))
	if fill.GetBackground() != lipgloss.Color(theme.Dark.MetaGreen) || fill.GetForeground() != lipgloss.Color(theme.Dark.Bg) {
		t.Fatalf("filled = bg %v fg %v", fill.GetBackground(), fill.GetForeground())
	}
	// syntaxStyle routes the meta classes: one mask paints everywhere.
	if s.syntaxStyle(base, metaClass("red", notemeta.StyleText)).GetForeground() != lipgloss.Color(theme.Dark.MetaRed) {
		t.Fatal("syntaxStyle does not reach metaStyle")
	}
	term := buildStyles(theme.Terminal)
	if term.metaStyle(base, metaClass("orange", notemeta.StyleText)).GetForeground() == term.metaStyle(base, metaClass("yellow", notemeta.StyleText)).GetForeground() {
		t.Fatal("Terminal orange and yellow must differ")
	}
}

func TestBadgeRun(t *testing.T) {
	t.Parallel()
	res := notemeta.Resolved{
		Badges: []notemeta.Badge{
			{Key: "kind", Value: "issue", Color: "red", Style: notemeta.StyleFilled},
			{Key: "severity", Value: "high", Color: "orange", Style: notemeta.StyleText, Glyph: "!"},
		},
		Audience: "author",
	}
	text, cls := badgeRun(res)
	if text != " issue  ! high  "+i18n.T("→ author") {
		t.Fatalf("text = %q", text)
	}
	if len(cls) != len([]rune(text)) {
		t.Fatalf("mask %d runes, text %d", len(cls), len([]rune(text)))
	}
	// The filled chip's padding wears the fill; the text badge only its runes.
	fill := metaClass("red", notemeta.StyleFilled)
	if cls[0] != fill || cls[6] != fill {
		t.Fatalf("filled chip mask = %v", cls[:7])
	}
	if cls[7] != 0 || cls[8] != metaClass("orange", notemeta.StyleText) {
		t.Fatalf("gap/text mask = %v", cls[7:14])
	}
	if text, _ := badgeRun(notemeta.Resolved{}); text != "" {
		t.Fatalf("empty = %q", text)
	}
	// The built-in status values are gg's words; any other value is data.
	st, _ := badgeRun(notemeta.Resolved{Badges: []notemeta.Badge{{Key: notemeta.StatusKey, Value: "agreed", Color: "green", Style: notemeta.StyleText}}})
	if st != i18n.T("agreed") {
		t.Fatalf("status = %q", st)
	}
	only, _ := badgeRun(notemeta.Resolved{Audience: "reviewer"})
	if only != i18n.T("for you") {
		t.Fatalf("marker alone = %q", only)
	}
}

func TestAudienceMarker(t *testing.T) {
	t.Parallel()
	if audienceMarker("author") != i18n.T("→ author") || audienceMarker("reviewer") != i18n.T("for you") || audienceMarker("") != "" {
		t.Fatal("markers")
	}
}

func TestMetaLineAndDetailTexts(t *testing.T) {
	t.Parallel()
	res := notemeta.Resolved{
		Lines:    []notemeta.Line{{Label: "intent", Value: "checked\nthe lock"}},
		Stored:   []notemeta.KV{{Key: "model", Value: "opus"}, {Key: "run", Value: "3"}},
		Added:    []notemeta.Change{{Key: "severity", From: "high", To: "low", By: "MR"}, {Key: "status", To: "agreed", By: "MR"}},
		Problems: []notemeta.Problem{`display.kind.values.issue.color: "crimson" is not a colour`},
	}
	if got := metaLineTexts(res); len(got) != 1 || got[0] != sanitizeLine("intent: checked\nthe lock") {
		t.Fatalf("lines = %q", got)
	}
	got := metaDetailTexts(res)
	want := []string{
		i18n.T("meta: %s", "model: opus · run: 3"),
		i18n.T("added: %s", "severity: high → low (MR) · status: agreed (MR)"),
		i18n.T("display problem: %s", `display.kind.values.issue.color: "crimson" is not a colour`),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("details = %q", got)
	}
	if metaDetailTexts(notemeta.Resolved{}) != nil {
		t.Fatal("no meta, no details")
	}
}

func TestMetaCounts(t *testing.T) {
	t.Parallel()
	c := notemeta.Counts{
		Groups: []notemeta.CountGroup{{Key: "kind", Label: "kind", Values: []notemeta.CountValue{{Value: "issue", Color: "red", N: 3}, {Value: "good", Color: "green", N: 8}}}},
		Author: 5, Reviewer: 9,
	}
	if got := metaCountsText(c); got != "3 issue · 8 good · "+i18n.T("%d for author", 5)+" · "+i18n.T("%d for you", 9) {
		t.Fatalf("text = %q", got)
	}
	run, cls := metaCountsRun(c)
	if run != "issue 3 good 8" {
		t.Fatalf("run = %q", run)
	}
	if len(cls) != len([]rune(run)) || cls[6] != metaClass("red", notemeta.StyleText) || cls[0] != 0 || cls[13] != metaClass("green", notemeta.StyleText) {
		t.Fatalf("mask = %v", cls)
	}
	if metaCountsText(notemeta.Counts{}) != "" {
		t.Fatal("empty counts must say nothing")
	}
}

func TestNoticeForNewerNotes(t *testing.T) {
	t.Parallel()
	v := preflight.Resolve(domain.Features(), preflight.Probes{Newer: map[string]bool{domain.StoreNotes: true}})
	var got []notice
	for _, n := range noticesForVerdicts(v, "repo") {
		if n.id == noticeFeatureDisabledPrefix+domain.FeatureNoteEdits {
			got = append(got, n)
		}
	}
	if len(got) != 1 || len(got[0].detail) != 2 {
		t.Fatalf("notices = %+v", got)
	}
}
