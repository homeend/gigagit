package forge

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/notemeta"
)

func agentComment(level string) Comment {
	return Comment{Summary: "config modules are cached forever", Rationale: "the cache key ignores mtime",
		Badges: []notemeta.Badge{{Key: "kind", Value: "issue"}, {Key: "severity", Value: "high"}},
		Lines:  []notemeta.Line{{Label: "intent", Value: "checked when the cache clears"}},
		Level:  level, Author: "claude", MarkerKey: "remark:r1:ab12"}
}

const ghTail = "config modules are cached forever\n\n<details><summary>Details</summary>\n\n" +
	"the cache key ignores mtime\n\n- intent: checked when the cache clears\n\n</details>\n\n— claude via gg\n\n<!-- gg:remark:r1:ab12 -->"

func TestGitHubGoldenBodyPerLevel(t *testing.T) {
	t.Parallel()
	for level, head := range map[string]string{
		"high":   "> [!CAUTION]\n> **issue · high**\n\n",
		"medium": "> [!WARNING]\n> **issue · high**\n\n",
		"low":    "> [!NOTE]\n> **issue · high**\n\n",
		"info":   "**issue · high**\n\n",
		"":       "**issue · high**\n\n",
	} {
		if got := (GitHubRenderer{}).RenderComment(agentComment(level)); got != head+ghTail {
			t.Errorf("level %q:\ngot  %q\nwant %q", level, got, head+ghTail)
		}
	}
}

func TestGitHubNoAlertsIsThePlainLineAtEveryLevel(t *testing.T) {
	t.Parallel()
	for _, level := range []string{"high", "medium", "low"} {
		got := (GitHubRenderer{NoAlerts: true}).RenderComment(agentComment(level))
		if got != "**issue · high**\n\n"+ghTail {
			t.Errorf("level %q: %q", level, got)
		}
	}
}

func TestGitHubNoBadgesNoLine(t *testing.T) {
	t.Parallel()
	c := agentComment("high")
	c.Badges = nil
	if got := (GitHubRenderer{}).RenderComment(c); got != ghTail {
		t.Fatalf("a level with no badges draws nothing: %q", got)
	}
}

func TestGitHubUserNote(t *testing.T) {
	t.Parallel()
	got := GitHubRenderer{}.RenderComment(Comment{Summary: "rename this", Rationale: "it shadows x", MarkerKey: "n1"})
	if want := "rename this\n\nit shadows x\n\n<!-- gg:n1 -->"; got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestGitHubUserNoteWithLines(t *testing.T) {
	t.Parallel()
	got := GitHubRenderer{}.RenderComment(Comment{Summary: "s", Rationale: "r",
		Lines: []notemeta.Line{{Label: "intent", Value: "i"}}, MarkerKey: "n1"})
	want := "s\n\nr\n\n<details><summary>Details</summary>\n\n- intent: i\n\n</details>\n\n<!-- gg:n1 -->"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestGitHubAgentNoteWithNothingToFoldHasNoDetails(t *testing.T) {
	t.Parallel()
	got := GitHubRenderer{}.RenderComment(Comment{Summary: "s", Author: "claude", MarkerKey: "k"})
	if want := "s\n\n— claude via gg\n\n<!-- gg:k -->"; got != want {
		t.Fatalf("got %q", got)
	}
}

func TestGitHubQuoteComesFirst(t *testing.T) {
	t.Parallel()
	c := agentComment("high")
	c.Quote, c.QuoteLine = []string{"\tx := 1"}, 15
	got := GitHubRenderer{}.RenderComment(c)
	if !strings.HasPrefix(got, "> Line 15:\n> ```\n> \tx := 1\n> ```\n\n> [!CAUTION]\n> **issue · high**\n\nconfig") {
		t.Fatalf("got %q", got)
	}
}

func TestGitHubQuoteFenceOutgrowsAQuotedFence(t *testing.T) {
	t.Parallel()
	got := GitHubRenderer{}.RenderComment(Comment{Quote: []string{"s := \"```go\"", "x := 1"}, QuoteLine: 3, Summary: "s", MarkerKey: "k"})
	if !strings.HasPrefix(got, "> Line 3:\n> ````\n") || !strings.Contains(got, "> ````\n\ns") {
		t.Fatalf("a quoted ``` must not close the fence: %q", got)
	}
}

func TestGitHubEscapesMetaText(t *testing.T) {
	t.Parallel()
	c := Comment{Summary: "s", Author: "a", MarkerKey: "k",
		Badges: []notemeta.Badge{{Value: "**x**"}},
		Lines:  []notemeta.Line{{Label: "why", Value: "</details><!-- y -->"}}}
	got := GitHubRenderer{}.RenderComment(c)
	if !strings.Contains(got, `**\*\*x\*\***`) {
		t.Errorf("badge value not escaped: %q", got)
	}
	if strings.Count(got, "</details>") != 1 || !strings.Contains(got, `\<!-- y`) {
		t.Errorf("a line value closed the details or opened a comment: %q", got)
	}
	if !strings.HasSuffix(got, "<!-- gg:k -->") {
		t.Errorf("the marker must stay last: %q", got)
	}
}

func TestGitHubSuggestionFencePassesThrough(t *testing.T) {
	t.Parallel()
	sum := "use this:\n\n```suggestion\nx := 2\n```"
	got := GitHubRenderer{}.RenderComment(Comment{Summary: sum, MarkerKey: "k"})
	if !strings.HasPrefix(got, sum+"\n\n") {
		t.Fatalf("the suggestion fence must pass through: %q", got)
	}
}

func TestGitHubReviewBody(t *testing.T) {
	t.Parallel()
	got := GitHubRenderer{}.RenderReview(ReviewBody{Summary: " looks fine ", Author: "claude", MarkerKey: "r1"})
	if want := "looks fine\n\n— claude via gg\n\n<!-- gg:r1 -->"; got != want {
		t.Fatalf("got %q", got)
	}
}

func TestGitHubCapabilities(t *testing.T) {
	t.Parallel()
	if c := (GitHubRenderer{}).Capabilities(); !c.Level || c.Audience {
		t.Fatalf("GitHub reads level, not audience: %+v", c)
	}
}

func TestGitHubUnrenderDropsTheBadgeBlock(t *testing.T) {
	t.Parallel()
	g := GitHubRenderer{}
	for _, level := range []string{"high", "low", ""} {
		if got := g.Unrender(g.RenderComment(agentComment(level))); got != ghTail {
			t.Errorf("level %q: %q", level, got)
		}
	}
	quoted := agentComment("medium")
	quoted.Quote, quoted.QuoteLine = []string{"s := \"```\"", "x"}, 4
	want := ghQuote(4, quoted.Quote) + ghTail
	if got := g.Unrender(g.RenderComment(quoted)); got != want {
		t.Errorf("after a quote:\ngot  %q\nwant %q", got, want)
	}
	// A body sent with alerts reads back after a NoAlerts flip, and the
	// other way round.
	if got := (GitHubRenderer{NoAlerts: true}).Unrender(g.RenderComment(agentComment("high"))); got != ghTail {
		t.Errorf("NoAlerts reading an alert body: %q", got)
	}
	if got := g.Unrender((GitHubRenderer{NoAlerts: true}).RenderComment(agentComment("high"))); got != ghTail {
		t.Errorf("reading a plain body: %q", got)
	}
	esc := agentComment("")
	esc.Badges = []notemeta.Badge{{Value: "a*b"}}
	if got := g.Unrender(g.RenderComment(esc)); got != ghTail {
		t.Errorf("an escaped badge word: %q", got)
	}
	for _, body := range []string{"**bold** start\n\n<!-- gg:k -->", "**a** and **b**\n\nrest\n\n<!-- gg:k -->", "plain\n\n<!-- gg:k -->"} {
		if got := g.Unrender(body); got != body {
			t.Errorf("not a badge block, untouched: %q → %q", body, got)
		}
	}
}

func TestRendererForFallsBackToGitHub(t *testing.T) {
	t.Parallel()
	if _, ok := RendererFor(nil).(GitHubRenderer); !ok {
		t.Fatal("no provider renderer → GitHub")
	}
	if _, ok := RendererFor(&GH{}).(*GH); !ok {
		t.Fatal("GH renders itself")
	}
}
