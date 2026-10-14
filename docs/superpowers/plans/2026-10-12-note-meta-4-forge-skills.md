# Note meta — plan 4: the forge renderer and the skills

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (this repo: NO implementer subagents — the session that wrote the plan executes it; only the final review is a read-only subagent). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** What gg posts to GitHub is rendered by the forge from a neutral comment (quote, badge line in a level alert, summary, details, signature, marker), meta stays inside gg, and every note-writing skill plus the engine's review prompt teaches the suggested vocabulary and the default display block.

**Architecture:**
- `internal/forge` gains the neutral `Comment` / `ReviewBody`, a `Renderer` interface (`RenderComment`, `RenderReview`, `Capabilities`, `Unrender`) and `GitHubRenderer`, which absorbs today's `sendBody` / `quoteLines` / `reviewSendBody` text building. `*GH` embeds it; `RendererFor(p)` picks a provider's renderer and falls back to GitHub.
- `internal/domain` stops building text: `forgeComment` maps a note (author meta under its effective declaration, NO added meta) onto a `forge.Comment`; the send planner renders through the renderer chosen once per plan.
- Read-back (§8.3): the PR view's echo of gg's own comment (it carries the marker) drops its badge block, so its summary row is the summary again. `Unrender` parses the body alone — a sent note is removed locally once GitHub has it, so there is no local item to match (ruling made during Task 3).
- `engine.DefaultReviewDisplay` holds the default display block once; the review prompt and the skills carry it; tests parse and validate every copy.

**Tech Stack:** Go 1.26 (`internal/forge`, `internal/domain`, `internal/engine`, `internal/agentskill`, `internal/notemeta`). Markdown skill bodies.

**Spec:** `docs/superpowers/specs/2026-10-12-note-meta-design.md` — §8 (forge adapters), §9 (skills), §11 (tests), §12 (docs), §2.3 (inheritance), §2.4 (added meta), §2.6 (limits), §3, R7, R8, R11, R12, R13, R14, Appendix A/B. Earlier plans: `2026-10-12-note-meta-1-core.md`, `-2-tui.md`, `-3-web.md`.

## Global Constraints

- **Stored-only meta, added meta and the declaration never leave gg** (§8.1). The send path resolves with `notemeta.Resolve(n.Meta, display, nil)` — never `NoteMeta` (which overlays added meta: a user's Mark `status: agreed` would become a badge).
- GitHub body order (§8.2): 1 quote; 2 badge line `**issue · high**` (alert by Level: high `> [!CAUTION]`, medium `> [!WARNING]`, low `> [!NOTE]`; `info`/unset = plain bold line; no badges = no line); 3 summary; 4 `<details>` = rationale then the `line` entries; 5 signature `— <author> via gg` + marker (last line).
- The raw tag list and the `confidence:` line are gone from bodies. `Audience` is not used by GitHub. Suggestion fences in a summary/rationale pass through untouched.
- Badge words are the VALUES only, joined by ` · `; no key, no glyph (ruling: spec example `**issue · high**`; GitHub has no colour).
- Agent keys/values are data: every value interpolated into markdown is backslash-escaped (`\ ` * _ [ ] < > ~ |` and `` ` ``).
- **Never send anything to GitHub without asking the user** — the alert check on a sandbox PR is a STOP (Task 7).
- Skills: five independent counters (`Version`, `ReviewVersion`, `GGReviewVersion`, `GGCrossReviewVersion`, `GGOverviewVersion`) — bump each one whose body changes; regenerate every tracked `.claude/skills/*/SKILL.md` copy with `go run ./cmd/gg init --update` (the `TestDogfood*CopyInSync` tests fail otherwise).
- The default display block uses coloured words, no glyphs (R14). The old `meta.severity = bug | risk | design | nit` instruction is removed everywhere.
- Meta never triggers anything (R12): skill text must say so; no skill may tell an agent that a value blocks, resolves or sends.
- Commits: `gg add <paths>` + `git commit -F <msgfile>` (gg commit has no -F); never `git add -A`.

## Review Focus

1. A note the user Marked (`Added` status) and then sent — the Mark must not appear in the body (Task 2 test `TestForgeCommentLeavesAddedMetaOut`).
2. A pre-format-3 remark with no declaration (`severity: bug`, `confidence: high` converted to meta) — R13 makes every key a `line`, so those lines go in `<details>`; pinned so nobody "fixes" it silently (Task 2 `TestForgeCommentWithoutDeclarationSendsLines`).
3. A badge value or line value holding markdown/HTML (`**`, `</details>`, `<!--`) — escaped, the body structure intact (Task 1 `TestGitHubEscapesMetaText`).
4. gg's own comment echoed back in the PR view (alert or plain badge line, with or without a quote) — its summary row must be the note's summary, not `quote`/the badge words; a lone-bold-line summary is the known ambiguity (Task 3 tests).
5. A user's own note (no agent) with declared lines — rationale stays plain, lines folded, no signature (Task 1 `TestGitHubUserNoteWithLines`).

---

### Task 1: forge — the neutral comment and the GitHub renderer

**Files:**
- Create: `internal/forge/render.go` (Comment, ReviewBody, Capabilities, Renderer, RendererFor)
- Create: `internal/forge/gh_render.go` (GitHubRenderer)
- Create: `internal/forge/gh_render_test.go`
- Modify: `internal/forge/gh.go:42` (`type GH struct{ GitHubRenderer; r gitexec.Runner }`)

**Interfaces:**
- Produces:
  ```go
  type Comment struct {
  	Quote     []string // the lines a file-level thread stands in for
  	QuoteLine int
  	Summary   string
  	Rationale string
  	Badges    []notemeta.Badge
  	Lines     []notemeta.Line
  	Level     string
  	Audience  string
  	Author    string // agent signature; "" for the user's own
  	MarkerKey string
  }
  type ReviewBody struct{ Summary, Author, MarkerKey string }
  type Capabilities struct{ Level, Audience bool }
  type Renderer interface {
  	RenderComment(Comment) string
  	RenderReview(ReviewBody) string
  	Capabilities() Capabilities
  	// Unrender is a rendered body as gg's own reader should see it: the
  	// badge block c renders to taken out (read-back, §8.3).
  	Unrender(body string) string // final shape: Task 3 (structural)
  }
  func RendererFor(p Provider) Renderer // p implements Renderer → it; else GitHubRenderer{}
  type GitHubRenderer struct{ NoAlerts bool } // NoAlerts: the plain bold line at every level
  ```

- [ ] **Step 1: Write the failing tests** — `internal/forge/gh_render_test.go`:

```go
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
	for _, level := range []string{"high", ""} {
		c := agentComment(level)
		if got := (GitHubRenderer{}).Unrender(GitHubRenderer{}.RenderComment(c), c); got != ghTail {
			t.Errorf("level %q: %q", level, got)
		}
	}
	plain := Comment{Summary: "**bold** start", MarkerKey: "k"}
	body := GitHubRenderer{}.RenderComment(plain)
	if got := (GitHubRenderer{}).Unrender(body, plain); got != body {
		t.Errorf("no badges: the body is untouched: %q", got)
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
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/forge -run 'GitHub|RendererFor' 2>&1 | tail -5`
Expected: FAIL — `undefined: Comment` / `GitHubRenderer`.

- [ ] **Step 3: Implement** — `internal/forge/render.go`:

```go
package forge

import "github.com/homeend/gigagit/internal/notemeta"

// Comment is one note as gg hands it to a forge (spec 2026-10-12-note-meta
// §8.1): neutral parts, never text. Badges and Lines are the author's
// declared meta only — stored-only keys, meta added later and the
// declaration itself never leave gg.
type Comment struct {
	Quote     []string // the lines a file-level thread stands in for
	QuoteLine int
	Summary   string
	Rationale string
	Badges    []notemeta.Badge
	Lines     []notemeta.Line
	Level     string
	Audience  string
	Author    string // agent signature; "" for the user's own
	MarkerKey string
}

// ReviewBody is a whole review's body: its summary, signed by its agent.
type ReviewBody struct{ Summary, Author, MarkerKey string }

// Capabilities are the gg semantics a renderer reads (R7): the rest of a
// comment's meta is simply not drawn by it.
type Capabilities struct{ Level, Audience bool }

// Renderer turns neutral parts into one forge's posted text. Domain hands
// it parts; only a renderer knows the forge's markup.
type Renderer interface {
	RenderComment(Comment) string
	RenderReview(ReviewBody) string
	Capabilities() Capabilities
	// Unrender is a body this renderer wrote for c as gg's own reader shows
	// it: the badge block taken out, so the summary leads again (§8.3).
	Unrender(body string) string // final shape: Task 3 (structural)
}

// RendererFor is p's renderer; a provider without one posts GitHub's markup.
func RendererFor(p Provider) Renderer {
	if r, ok := p.(Renderer); ok {
		return r
	}
	return GitHubRenderer{}
}
```

`internal/forge/gh_render.go` (moves `quoteLines` from `domain/forge_send_body.go` here as `ghQuote`):

```go
package forge

import (
	"fmt"
	"strings"
)

// GitHubRenderer is GitHub's markup (spec §8.2): the quote, the badge line
// (in an alert when the level asks), the summary, <details> with the
// rationale and the line entries, the signature, the marker.
type GitHubRenderer struct {
	// NoAlerts draws the plain bold badge line at every level: the fallback
	// if GitHub does not render alerts inside review comments.
	NoAlerts bool
}

func (GitHubRenderer) Capabilities() Capabilities { return Capabilities{Level: true} }

func (g GitHubRenderer) RenderComment(c Comment) string {
	var b strings.Builder
	if len(c.Quote) > 0 {
		b.WriteString(ghQuote(c.QuoteLine, c.Quote))
	}
	if bl := g.badgeBlock(c); bl != "" {
		b.WriteString(bl + "\n\n")
	}
	b.WriteString(strings.TrimSpace(c.Summary))
	agent := c.Author != ""
	rat := strings.TrimSpace(c.Rationale)
	var fold []string
	if rat != "" {
		if agent {
			fold = append(fold, rat)
		} else {
			b.WriteString("\n\n" + rat)
		}
	}
	if len(c.Lines) > 0 {
		var ls []string
		for _, l := range c.Lines {
			ls = append(ls, "- "+mdEscape(l.Label)+": "+mdEscape(l.Value))
		}
		fold = append(fold, strings.Join(ls, "\n"))
	}
	if len(fold) > 0 {
		b.WriteString("\n\n<details><summary>Details</summary>\n\n" + strings.Join(fold, "\n\n") + "\n\n</details>")
	}
	if agent {
		b.WriteString("\n\n— " + c.Author + " via gg")
	}
	b.WriteString("\n\n" + SendMarker(c.MarkerKey))
	return b.String()
}

func (GitHubRenderer) RenderReview(r ReviewBody) string {
	return GitHubRenderer{}.RenderComment(Comment{Summary: r.Summary, Author: r.Author, MarkerKey: r.MarkerKey})
}

func (GitHubRenderer) Unrender(body string) string { // superseded: see Task 3 (structural parse)
	bl := g.badgeBlock(c)
	if bl == "" {
		return body
	}
	return strings.Replace(body, bl+"\n\n", "", 1)
}

// badgeBlock is the badge words in bold, in the level's alert; "" without
// badges.
func (g GitHubRenderer) badgeBlock(c Comment) string {
	if len(c.Badges) == 0 {
		return ""
	}
	words := make([]string, len(c.Badges))
	for i, bd := range c.Badges {
		words[i] = mdEscape(bd.Value)
	}
	line := "**" + strings.Join(words, " · ") + "**"
	if tag := g.alert(c.Level); tag != "" {
		return "> [!" + tag + "]\n> " + line
	}
	return line
}

// alert is the GitHub alert a level opens; "" = the plain line.
func (g GitHubRenderer) alert(level string) string {
	if g.NoAlerts {
		return ""
	}
	switch level {
	case "high":
		return "CAUTION"
	case "medium":
		return "WARNING"
	case "low":
		return "NOTE"
	}
	return ""
}

// mdEscape backslash-escapes the markdown and HTML punctuation an agent's
// key or value could use to break the body's structure.
func mdEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune("\\`*_[]<>~|", r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// ghQuote opens a file-level thread with the lines it is really about. The
// fence outgrows any backtick run in them, so a quoted ``` cannot close it.
func ghQuote(line int, text []string) string {
	run := 2
	for _, l := range text {
		n := 0
		for _, r := range l {
			if r == '`' {
				n++
				run = max(run, n)
			} else {
				n = 0
			}
		}
	}
	fence := strings.Repeat("`", run+1)
	var b strings.Builder
	fmt.Fprintf(&b, "> Line %d:\n> %s\n", line, fence)
	for _, l := range text {
		b.WriteString("> " + l + "\n")
	}
	b.WriteString("> " + fence + "\n\n")
	return b.String()
}
```

`gh.go`: `type GH struct {` `GitHubRenderer` `; r gitexec.Runner }` (embedding gives `*GH` the Renderer methods). Check `NewGH`/other `GH{...}` literals still compile (keyed `r:` fields).

Note `TestGitHubEscapesMetaText`: `</details>` escapes to `\<\/details\>`? — `/` is not escaped, `<` and `>` are: `\</details\>` does not parse as a tag on GitHub, and the literal `</details>` substring is gone, so `Count == 1`. `<!--` becomes `\<!--`, so `<!-- y` is absent.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/forge 2>&1 | tail -5`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
gg add internal/forge/render.go internal/forge/gh_render.go internal/forge/gh_render_test.go internal/forge/gh.go
git commit -F <msgfile>   # "forge: the neutral comment and the GitHub renderer"
```

---

### Task 2: domain — notes become comments; the planner renders through the forge

**Files:**
- Create: `internal/domain/forge_send_comment.go` (forgeComment, reviewBodyOf, sendRenderer)
- Create: `internal/domain/forge_send_comment_test.go`
- Delete: `internal/domain/forge_send_body.go`, `internal/domain/forge_send_body_test.go` (their cases now live in forge and in the new test)
- Modify: `internal/domain/forge_send.go` — `planSend` (pick the renderer once), `planActions` (param), `sendPlace` (field `rnd`), `noteItem`, `remarkItem`, `threadFor`, the review case, `applyBodyFrom` (param)

**Interfaces:**
- Consumes: `forge.Comment`, `forge.ReviewBody`, `forge.Renderer`, `forge.RendererFor`, `forge.GitHubRenderer` (Task 1).
- Produces:
  ```go
  func forgeComment(n model.Note, root *model.Note, key string) forge.Comment
  func reviewBodyOf(r Review, summary string) forge.ReviewBody
  func (s *Service) sendRenderer() forge.Renderer // never probes: the active provider's, else GitHub
  func threadFor(path string, side model.NoteSide, rng [2]int, text []string, hunks []model.Hunk, c forge.Comment, rnd forge.Renderer) forge.Thread
  ```

- [ ] **Step 1: Write the failing tests** — `internal/domain/forge_send_comment_test.go`:

```go
package domain

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/forge"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notemeta"
)

var kindDisplay = notemeta.Display{
	"kind":     {Show: []string{"badge"}, Order: 1, Values: map[string]notemeta.Value{"issue": {Color: "red", Audience: "author"}}},
	"severity": {Show: []string{"badge"}, Order: 2, Values: map[string]notemeta.Value{"high": {Color: "red", Level: "high"}}},
	"intent":   {Show: []string{"line"}, Order: 4},
	"secret":   {}, // declared with no place: stored-only
}

func TestForgeCommentMapsDeclaredMeta(t *testing.T) {
	t.Parallel()
	n := model.Note{Source: model.NoteSourceAgent, Author: "claude", Summary: " s ", Rationale: " r ",
		Meta:    []notemeta.KV{{"kind", "issue"}, {"severity", "high"}, {"intent", "checked x"}, {"secret", "s3"}, {"confidence", "high"}},
		Display: kindDisplay}
	c := forgeComment(n, nil, "k1")
	if c.Summary != "s" || c.Rationale != "r" || c.Author != "claude" || c.MarkerKey != "k1" {
		t.Fatalf("parts: %+v", c)
	}
	if len(c.Badges) != 2 || c.Badges[0].Value != "issue" || c.Badges[1].Value != "high" || c.Level != "high" || c.Audience != "author" {
		t.Fatalf("badges/level/audience: %+v", c)
	}
	if len(c.Lines) != 1 || c.Lines[0] != (notemeta.Line{Label: "intent", Value: "checked x"}) {
		t.Fatalf("only the declared line goes: %+v", c.Lines)
	}
}

func TestForgeCommentLeavesAddedMetaOut(t *testing.T) {
	t.Parallel()
	n := model.Note{Source: model.NoteSourceUser, Summary: "s", Meta: []notemeta.KV{{"kind", "issue"}}, Display: kindDisplay,
		Added: []notemeta.Added{{Key: "status", Value: "agreed", By: "me"}, {Key: "kind", Value: "nitpick", By: "me"}}}
	body := forge.GitHubRenderer{}.RenderComment(forgeComment(n, nil, "k"))
	for _, leak := range []string{"agreed", "nitpick", "status"} {
		if strings.Contains(body, leak) {
			t.Errorf("added meta %q left gg:\n%s", leak, body)
		}
	}
	if !strings.Contains(body, "**issue**") {
		t.Errorf("the author's own badge goes:\n%s", body)
	}
}

func TestForgeCommentNeverSendsStoredOnlyOrTheDeclaration(t *testing.T) {
	t.Parallel()
	n := model.Note{Source: model.NoteSourceAgent, Summary: "s", Meta: []notemeta.KV{{"secret", "s3"}, {"confidence", "high"}, {"kind", "issue"}}, Display: kindDisplay}
	body := forge.GitHubRenderer{}.RenderComment(forgeComment(n, nil, "k"))
	for _, leak := range []string{"s3", "secret", "confidence", "color", "show", "red"} {
		if strings.Contains(body, leak) {
			t.Errorf("%q left gg:\n%s", leak, body)
		}
	}
}

// R13: a note with NO declaration (a converted pre-format-3 remark) shows
// every key as a line — and so sends them in <details>. Pinned on purpose.
func TestForgeCommentWithoutDeclarationSendsLines(t *testing.T) {
	t.Parallel()
	n := model.Note{Source: model.NoteSourceAgent, Summary: "s", Meta: []notemeta.KV{{"severity", "bug"}, {"confidence", "high"}}}
	c := forgeComment(n, nil, "k")
	if len(c.Badges) != 0 || len(c.Lines) != 2 || c.Lines[0].Value != "bug" || c.Lines[1].Value != "high" {
		t.Fatalf("got %+v", c)
	}
}

func TestForgeCommentSignature(t *testing.T) {
	t.Parallel()
	if c := forgeComment(model.Note{Source: model.NoteSourceAgent, Summary: "s"}, nil, "k"); c.Author != "agent" {
		t.Errorf("an unnamed agent signs as agent: %q", c.Author)
	}
	if c := forgeComment(model.Note{Source: model.NoteSourceUser, Author: "MR", Summary: "s"}, nil, "k"); c.Author != "" {
		t.Errorf("the user's own note is unsigned: %q", c.Author)
	}
}

func TestForgeCommentReplyInheritsItsRootsDeclaration(t *testing.T) {
	t.Parallel()
	root := model.Note{Display: kindDisplay}
	rep := model.Note{Source: model.NoteSourceAgent, Summary: "s", Meta: []notemeta.KV{{"kind", "issue"}}}
	if c := forgeComment(rep, &root, "k"); len(c.Badges) != 1 || c.Badges[0].Value != "issue" {
		t.Fatalf("got %+v", c)
	}
}

func TestReviewBodyOfSignsWithTheAgent(t *testing.T) {
	t.Parallel()
	if b := reviewBodyOf(Review{ID: "r1"}, " x "); b != (forge.ReviewBody{Summary: "x", Author: "agent", MarkerKey: "r1"}) {
		t.Fatalf("got %+v", b)
	}
}
```

Also update `internal/domain/forge_send_test.go`: add a planner-level test next to the existing review-send test (the one asserting `— claude via gg` at line ~160) that saves a review document whose display is `kindDisplay`-shaped and whose remark has `kind: issue, severity: high, intent: x, confidence: high`, plans the send (`svc.planSend(ctx, PRSendRequest{PR: 7, Review: rid})`, same fixture as that test), and asserts the remark thread body: contains `> [!CAUTION]\n> **issue · high**`, contains `- intent: x`, lacks `confidence`. Copy that test's fixture setup verbatim (read it first; it builds the fake PR and saves a review with `gg review save`-equivalent `svc.SaveReview`).

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/domain -run 'ForgeComment|ReviewBodyOf' 2>&1 | tail -5`
Expected: FAIL — `undefined: forgeComment`.

- [ ] **Step 3: Implement** — `internal/domain/forge_send_comment.go`:

```go
package domain

import (
	"strings"

	"github.com/homeend/gigagit/internal/forge"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notemeta"
)

// forgeComment is note n as the forge-neutral comment (spec
// 2026-10-12-note-meta §8.1): its author's meta under its effective
// declaration (a reply's own, else its root's). Meta added later — a Mark,
// a double-check — is left out (it never leaves gg), and so are the
// stored-only keys and the declaration: Resolve draws only declared places.
func forgeComment(n model.Note, root *model.Note, key string) forge.Comment {
	d := n.Display
	if len(d) == 0 && root != nil {
		d = root.Display
	}
	res := notemeta.Resolve(n.Meta, d, nil)
	c := forge.Comment{Summary: strings.TrimSpace(n.Summary), Rationale: strings.TrimSpace(n.Rationale),
		Badges: res.Badges, Lines: res.Lines, Level: res.Level, Audience: res.Audience, MarkerKey: key}
	if n.Source == model.NoteSourceAgent {
		c.Author = agentSignature(n.Author)
	}
	return c
}

// reviewBodyOf is review r's body with summary as its text, signed by its
// agent and marked with the review note's id.
func reviewBodyOf(r Review, summary string) forge.ReviewBody {
	return forge.ReviewBody{Summary: strings.TrimSpace(summary), Author: agentSignature(r.Agent), MarkerKey: r.ID}
}

func agentSignature(name string) string {
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	return "agent"
}

// sendRenderer is the active provider's renderer, else GitHub's. It never
// probes: by the time anything is planned or read back, detection ran.
func (s *Service) sendRenderer() forge.Renderer {
	s.forgeMu.Lock()
	p := s.forgeLocal
	if p == nil {
		p = s.forgeActive
	}
	s.forgeMu.Unlock()
	if p == nil {
		return forge.GitHubRenderer{}
	}
	return forge.RendererFor(p)
}
```

Rewire `forge_send.go` (read each site before editing):
- `planSend`: `rnd := s.sendRenderer()` after `PRRevalidate`; pass to `planActions(ctx, plan, req, rnd)` (both calls, lines ~149 and ~161) and `applyBodyFrom(ctx, &plan, prev.Set, pr, req, rnd)` (both calls); after `sendPlacer` returns `pl`, set `pl.rnd = rnd`.
- `sendPlace` struct: add `rnd forge.Renderer`.
- `planActions`: `Body: rnd.RenderComment(forgeComment(d, s.replyRoot(byID, d), d.ID))` where `replyRoot(byID, d)` returns `&root` when `byID[d.ParentID]` exists, else nil (a draft reply to a GitHub thread has no local root). Add the tiny helper in `forge_send_comment.go`:
  ```go
  // localRoot is reply d's root among the stored notes, nil when it has none
  // (a draft reply to a GitHub thread).
  func localRoot(byID map[string]model.Note, d model.Note) *model.Note {
  	if r, ok := byID[d.ParentID]; ok && d.ParentID != "" {
  		return &r
  	}
  	return nil
  }
  ```
  and call `localRoot(byID, d)`.
- Review case: `plan.Body = pl.rnd.RenderReview(reviewBodyOf(r, r.summaryText()))` — check `summaryText()` is what `reviewSendBody` used (Doc.Summary else Text); if not identical, keep the old expression inline. Edited body: `pl.rnd.RenderReview(reviewBodyOf(r, edited))`. (If `pl` is not built yet at that point, use the local `rnd`.)
- `noteItem`: `threadFor(path, model.NoteSideNew, r.Range, text, pl.hunks[path], forgeComment(n, nil, n.ID), pl.rnd)`; replies: `pl.rnd.RenderComment(forgeComment(rep.Note, &n, rep.Note.ID))`.
- `remarkItem`: keep `note` + `r.remarkMeta(&note, file, dn, fp)`; `threadFor(..., forgeComment(note, nil, key), pl.rnd)`; replies `pl.rnd.RenderComment(forgeComment(rep, nil, rep.ID))` (Ruling: a remark reply resolves its own declaration only — the read-back lookup in Task 3 cannot see the remark as a root; keep both sides identical).
- `threadFor(path, side, rng, text, hunks, c forge.Comment, rnd forge.Renderer)`: placed → `Body: rnd.RenderComment(c)`; else `c.Quote, c.QuoteLine = text, rng[0]` then render.
- `applyBodyFrom(..., rnd forge.Renderer)`: `plan.Body = rnd.RenderReview(reviewBodyOf(r, r.summaryText()))` (same check as above).
- Delete `forge_send_body.go` and `forge_send_body_test.go`.

- [ ] **Step 4: Run to verify it passes**

Run: `go build ./... && go test ./internal/domain ./internal/forge 2>&1 | tail -8`
Expected: `ok` for both (existing send tests still assert `— claude via gg` and pass unchanged).

- [ ] **Step 5: Commit**

```bash
gg add internal/domain/forge_send_comment.go internal/domain/forge_send_comment_test.go internal/domain/forge_send.go internal/domain/forge_send_test.go internal/domain/forge_send_body.go internal/domain/forge_send_body_test.go
git commit -F <msgfile>   # "domain: sends hand the forge neutral comments; added meta never leaves gg"
```

---

### Task 3: read-back — gg's own echo leads with its summary again (§8.3)

> Rewritten during execution (ledger Task 3 ruling): the first design looked the local item up by the marker and re-rendered its badge block, but a send REMOVES the note locally (and settles a fully-sent review away), so the lookup was dead in production. `Unrender(body string)` now recognises the GitHub renderer's own grammar structurally.

**Files:**
- Modify: `internal/forge/render.go`, `internal/forge/gh_render.go` — `Unrender(body string) string`: skip an exact `ghQuote` block at the head (`ghQuoteLen`, no regex backreferences in RE2), then strip one badge block, alert form `> [!CAUTION|WARNING|NOTE]\n> **…**\n\n` or plain `**…**\n\n`, either regardless of `NoAlerts`; a badge word holds no bare `*` (`ghBadgeRe`), so `**a** and **b**` is not a badge line.
- Modify: `internal/domain/forge_notes.go` — `forgeNote(c, tip, body string)`; `forgeNotesFor` unrenders a body that carries the gg marker (`forge.SendMarkerKey`); other comments read as written. `forge_drafts.go` passes `root.Body`.
- Tests: `internal/forge/gh_render_test.go` `TestGitHubUnrenderDropsTheBadgeBlock` (levels high/low/unset, after a quote, NoAlerts both ways, an escaped word, three untouched bodies); `internal/domain/forge_send_comment_test.go` `TestForgeNoteOfOwnEchoLeadsWithTheSummary` + `TestPRViewEchoOfOwnCommentDropsTheBadgeBlock` (NO stored note: an alert echo, a plain echo, someone else's `**bold**\n\nrest` → `bold`).

- [x] RED: PR-view summaries were `quote` / `issue`; GREEN after the change; each guard (quote skip, bare-`*` rule, the domain call) proven to bite by disabling it.

### Task 4: engine — the review prompt teaches the vocabulary and the default block

**Files:**
- Modify: `internal/engine/review_changes.go:133-170` (`ReviewOutputInstruction`) + new const `DefaultReviewDisplay`
- Test: `internal/engine/review_changes_test.go`

**Interfaces:**
- Produces: `const DefaultReviewDisplay string` — the spec §9.3 block as a JSON OBJECT (the `display` value, no wrapper), 2-space indented, exactly:

```json
{
  "kind": {"show": ["badge", "count"], "order": 1, "values": {
    "issue":      {"color": "red",    "audience": "author"},
    "suggestion": {"color": "blue",   "audience": "author"},
    "nitpick":    {"color": "grey",   "audience": "author"},
    "question":   {"color": "purple", "audience": "author"},
    "note":       {"color": "yellow", "audience": "reviewer"},
    "good":       {"color": "green",  "audience": "reviewer"}}},
  "severity": {"show": ["badge", "frame"], "order": 2, "style": "text", "values": {
    "high":   {"color": "red",    "level": "high"},
    "medium": {"color": "orange", "level": "medium"},
    "low":    {"color": "grey",   "level": "low"}}},
  "audience": {"as": "audience", "order": 3},
  "intent": {"show": ["line"], "order": 4}
}
```

- [ ] **Step 1: Write the failing tests**

```go
func TestDefaultReviewDisplayIsValid(t *testing.T) {
	d, probs, err := notemeta.ParseDisplay([]byte(DefaultReviewDisplay))
	if err != nil || len(probs) > 0 || len(d) != 4 {
		t.Fatalf("err %v, problems %v, keys %d", err, probs, len(d))
	}
	for _, k := range d {
		for v, val := range k.Values {
			if val.Glyph != "" {
				t.Errorf("R14: the default block uses no glyphs (%s)", v)
			}
		}
	}
}

func TestReviewOutputInstructionTeachesMeta(t *testing.T) {
	got := ReviewOutputInstruction()
	for _, want := range []string{`"display"`, DefaultReviewDisplay, `"kind"`, "issue | suggestion | nitpick | question | note | good",
		"high | medium | low", `"intent"`, "author", "reviewer", "never triggers"} {
		if !strings.Contains(got, want) {
			t.Errorf("ReviewOutputInstruction() lacks %q", want)
		}
	}
	if strings.Contains(got, "bug | risk | design | nit") {
		t.Error("the old severity vocabulary is gone")
	}
}
```

(`notemeta` import in the engine test file.)

- [ ] **Step 2: Run** `go test ./internal/engine -run 'DefaultReviewDisplay|TeachesMeta' 2>&1 | tail -5` — Expected: FAIL `undefined: DefaultReviewDisplay`.

- [ ] **Step 3: Implement** — add the const (raw string, exactly the block above) and rewrite the JSON example + rules in `ReviewOutputInstruction`:
  - document level: after `"meta": { "verdict": … },` add `"display": <the default display block below>,`
  - annotation meta: `"meta": { "kind": "issue | suggestion | nitpick | question | note | good", "severity": "high | medium | low", "intent": "<what made you write this note>", "confidence": "low | medium | high" } }`
  - after the markdown-summary paragraph, a new paragraph:

```
"meta" is free-form (string values) and is a glance aid for the reader: it
never triggers anything and never leaves gg. Suggested keys: "kind" — issue
(wrong now), suggestion (fine now, could be better), nitpick (style, trivial),
question (you need the author's answer), note (context or impact, nothing to
fix), good (you checked it and it holds); "severity" — high | medium | low on
issue, suggestion and note; "intent" — one line: what made you write the
note; "confidence". Set "audience" on a remark only to override its kind:
author = the author would change code or reply; reviewer = written so the
user understands or trusts the change. Put this "display" on the document —
it shows kind and severity as coloured words:
```
followed by `DefaultReviewDisplay + "\n"`. Keep the existing "Rules:" paragraph, minus its `"meta" is optional and free-form (string values).` sentence (moved up).

- [ ] **Step 4: Run** `go test ./internal/engine 2>&1 | tail -5` — Expected: `ok` (including `TestReviewOutputInstructionAsksForStructuredMarkdown`).

- [ ] **Step 5: Commit** — message "engine: the review prompt suggests kind/severity/intent and the default display".

---

### Task 5: skills — "Meta and display" in every note-writing skill

**Files:**
- Modify: `internal/agentskill/reviewing-with-gg.md` (full section; the batch example line ~196; the review document ~286-291; the paragraph ~319)
- Modify: `internal/agentskill/gg-review.md` (step 3), `gg-cross-review.md` (Merging: severities, `risk`, keys), `gg-overview.md` (step 4), `using-gg.md` (`gg note meta` line 196 pointer + `gg pr notes --json` sentence ~929 values)
- Modify: `internal/agentskill/agentskill.go` — `Version` 169→170, `ReviewVersion` 18→19, `GGReviewVersion` 5→6, `GGCrossReviewVersion` 5→6, `GGOverviewVersion` 8→9 (re-check the current values on the branch first; bump each by one)
- Test: `internal/agentskill/agentskill_test.go` (new `TestDefaultDisplayBlockInSkillsIsValid`)
- Regenerate: `.claude/skills/{using-gg,reviewing-with-gg,gg-review,gg-cross-review,gg-overview}/SKILL.md`

**Interfaces:**
- Consumes: `engine.DefaultReviewDisplay` (Task 4) — the agentskill TEST imports engine (engine does not import agentskill; verify with `go list -deps ./internal/engine | grep agentskill` → empty).

- [ ] **Step 1: Write the failing test**

```go
// The default display block a skill teaches must be one gg accepts whole
// (spec §9: "A test parses the default block from the skill text") and the
// same block the engine's review prompt carries.
func TestDefaultDisplayBlockInSkillsIsValid(t *testing.T) {
	var want any
	if err := json.Unmarshal([]byte(engine.DefaultReviewDisplay), &want); err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, s := range All() {
		for _, block := range jsonBlocks(s.Body()) {
			var doc struct {
				Display json.RawMessage `json:"display"`
			}
			if json.Unmarshal([]byte(block), &doc) != nil || len(doc.Display) == 0 || strings.HasPrefix(strings.TrimSpace(block), "{\n  \"version\"") {
				continue
			}
			found++
			if _, probs, err := notemeta.ParseDisplay(doc.Display); err != nil || len(probs) > 0 {
				t.Errorf("%s: the display block is not clean: %v %v", s.Name, err, probs)
			}
			var got any
			_ = json.Unmarshal(doc.Display, &got)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("%s: the display block differs from engine.DefaultReviewDisplay", s.Name)
			}
		}
	}
	if found == 0 {
		t.Fatal("no skill teaches the default display block")
	}
	for _, s := range All() {
		if strings.Contains(s.Body(), "bug | risk | design | nit") || strings.Contains(s.Body(), "bug > risk") {
			t.Errorf("%s still teaches the old severity vocabulary", s.Name)
		}
	}
}

// jsonBlocks are the bodies of the ```json fences in md.
func jsonBlocks(md string) []string {
	var out []string
	for _, part := range strings.Split(md, "```json\n")[1:] {
		if body, _, ok := strings.Cut(part, "\n```"); ok {
			out = append(out, body)
		}
	}
	return out
}
```

(imports: `encoding/json`, `reflect`, `internal/engine`, `internal/notemeta`; check `All()` and `Body()` exist under those names — `TestAllReturnsEverySkill` uses them.) The "version" skip keeps the review-document example (which shows `"display": <…>` pseudo-JSON and does not unmarshal anyway) out.

- [ ] **Step 2: Run** `go test ./internal/agentskill -run DefaultDisplayBlock 2>&1 | tail -5` — Expected: FAIL `no skill teaches the default display block` (and the old-vocabulary errors).

- [ ] **Step 3: Write the skill text.**

**reviewing-with-gg.md** — insert before `## Caveats` (after "Review document"):

````markdown
## Meta and display

Every note and every review remark may carry `meta` — free key/values,
string values — and a `display` declaration saying how gg shows them
(`gg note add|reply … --meta k=v --display <json|@file>`, `gg note meta
<id> set|unset|display`, `"meta"`/`"display"` in a batch or a review
document). Meta is a glance aid for the reader: it never triggers anything
— nothing is sent, ticked, resolved or blocked because of a value. It never
leaves gg as data: a GitHub send carries only the badge words and line
entries you declare, never a stored-only key, never meta someone added
later, never the declaration.

**The fixed part — gg's, not yours to extend:**

- **Places** (`"show"`): `badge` (a coloured word before the note's title),
  `frame` (the note box's border colour; the first framed key wins),
  `count` (tallied in the review header and per file), `line` (a
  `label: value` row under the title). A key declared with no place is
  stored and listed (`gg note show`), not shown.
- **Colours**: `red`, `orange`, `yellow`, `green`, `blue`, `purple`,
  `grey`. **Style**: `filled` (default) or `text`. **Glyphs** (optional,
  `"glyph"`): one printable ASCII character or one of `◆ · → » •` — leave
  them out unless you have a reason.
- `"label"` renames a key in lines and counts; `"order"` sorts the keys;
  `"other"` styles a value you did not list.
- **Two semantics** gg understands: `level` (`high | medium | low | info`
  — how loud; GitHub wraps a leveled note's badge line in an alert) and
  `audience` (`author | reviewer` — who the note is for). Set them on a
  value (`"high": {"color": "red", "level": "high"}`) or map a key's
  value straight onto one with `"as": "level"` / `"as": "audience"`.
- **Inheritance**: a remark uses remark → file → document, nearest wins,
  and a nearer entry for a key replaces the farther one wholesale; a reply
  its own, else its root's; a standalone note its own.
- **Limits**: ≤ 32 meta keys per note, keys ≤ 64 bytes, values ≤ 256
  bytes, single-line; ≤ 8 declared keys, ≤ 16 values per key. An invalid
  or over-limit entry is dropped and named (`gg review save` prints the
  problems); the write still succeeds.
- A key with no declaration is stored and listed, not shown. A note with
  no declaration at all shows every key as a line.

**Suggested vocabulary — suggested, not required:**

- `kind` — `issue` (wrong now), `suggestion` (fine now, could be better),
  `nitpick` (style or taste, trivial), `question` (you need the author's
  answer), `note` (context or impact, nothing to fix), `good` (you checked
  it and it holds). Wrong now vs could be better → issue vs suggestion; "I
  verified it" → good, not note.
- `severity` — `high` / `medium` / `low`, on issue, suggestion and note.
- `intent` — one short line: what made you write this note ("checked the
  lock path for leaked waiters", "flagging an intended breaking change").
- `confidence` — `low` / `medium` / `high`.
- `audience` — only to override the kind's default: would the author
  change code or reply because of this? → `author`. Did you write it so the
  user understands or trusts the change? → `reviewer`.

**The default display block** — put it on the review document (or on a
note with `--display`) unless you have a reason not to; coloured words,
no glyphs:

```json
{
  "display": <DefaultReviewDisplay, indented two more spaces>
}
```

**Worked examples** (remarks of a review document):

```text
config modules are cached forever       → {"kind": "issue", "severity": "high", "intent": "checked when the config cache is cleared"}
the no-op catch                         → {"kind": "good", "intent": "verified the rejection path"}
unknown callers now get ESM             → {"kind": "note", "severity": "high", "audience": "author"}   (the migration guide needs it)
no fixture for `break`                  → {"kind": "suggestion", "severity": "low"}
browser stub's signature drifted        → {"kind": "nitpick"}
```
````

(The `<DefaultReviewDisplay, …>` line is an instruction to the executor: paste the Task 4 block there, re-indented, so the fenced block is valid JSON `{"display": {...}}`.)

Also in reviewing-with-gg.md:
- the batch example: `"tags":["perf"],"confidence":"high"` → `"meta":{"kind":"suggestion","severity":"medium","confidence":"high"}`;
- the review document example: add `"display": { …the default display block, see "Meta and display"… },` after the document `"meta"` line, and the annotation meta → `"meta": { "kind": "issue | suggestion | nitpick | question | note | good", "severity": "high | medium | low", "intent": "<what made you write this>" } }`;
- the paragraph at ~319: `"meta"` is optional and free-form (string values) on the document, a file and a note; how gg shows it is the `"display"` you declare — see "Meta and display".

**gg-review.md** step 3: replace `` `meta.severity` = bug | risk | design | nit `` with: `` each remark's `meta` uses the suggested vocabulary (`kind`, `severity`, `intent`, `confidence`) and the document carries the default `"display"` block — both in reviewing-with-gg's "Meta and display" section ``.

**gg-cross-review.md** Merging:
- Agreement: "its `kind` and `severity` are the most serious any of them gave (issue > suggestion > nitpick; high > medium > low)".
- Discrepancy bullet: "kinds that disagree (issue vs good) or severities more than one step apart (high vs low)".
- Undecidable: "keep it as a `kind: question` remark, `meta.ruling` = `"open"`…".
- Add after the ruling list: "Use reviewing-with-gg's "Meta and display" vocabulary and put its default `"display"` block on the merged document. `raised_by`, `disputed_by` and `ruling` are stored-only keys unless you declare them; a double-checking agent that agrees or disagrees later writes `gg note meta <review:id:n> set status=agreed|disagreed|deferred` — a glance mark, it never resolves or sends anything."

**gg-overview.md** step 4: append "A note may carry `--meta k=v` and `--display` (reviewing-with-gg's "Meta and display": `kind`, `intent`; meta never triggers anything)."

**using-gg.md**: the `gg note meta` line's comment already points at the review skills — change "see the review skills for the suggested keys" to "see reviewing-with-gg's "Meta and display" for the suggested keys"; in the `gg pr notes --json` sentence: "`severity` (a remark's `meta.severity`: high | medium | low)".

Bump the five counters in `agentskill.go`.

- [ ] **Step 4: Regenerate and run**

```bash
cd /work/gigagit/.claude/worktrees/note-meta-forge && go run ./cmd/gg init --update >/dev/null && git status --short .claude/skills
go test ./internal/agentskill ./internal/engine 2>&1 | tail -5
```
Expected: the five SKILL.md copies modified; `ok` for both packages.

- [ ] **Step 5: Commit** — `gg add internal/agentskill .claude/skills/using-gg/SKILL.md .claude/skills/reviewing-with-gg/SKILL.md .claude/skills/gg-review/SKILL.md .claude/skills/gg-cross-review/SKILL.md .claude/skills/gg-overview/SKILL.md`; message "skills: Meta and display — suggested vocabulary, audience rule, default block (v170)".

---

### Task 6: docs

**Files:**
- Modify: `CHANGELOG.md` (new top section "Note meta on GitHub and in the skills")
- Modify: `README.md` — new `#### Note meta` subsection after "#### Sending to GitHub" (TUI: badges/frame/lines, review header counts, Mark from the `.` menu; web: the same; GitHub: badge line + level alert, details, what never leaves gg; `gg note meta`)
- Modify: `CLAUDE.md` — `forge` row: add "the neutral `Comment` + per-provider `Renderer` (GitHub's markup, `RendererFor`)"; one line
- Modify: `docs/CLAUDE-details.md` — a "Note meta on GitHub (plan 4)" section: body order, the never-sent rule (`Resolve(meta, display, nil)`), escaping, `Unrender` read-back, `NoAlerts` fallback, default block single-sourced in `engine.DefaultReviewDisplay` + the skills test

- [ ] **Step 1:** write the four edits (read each file's neighbouring section first and match its voice).
- [ ] **Step 2:** `go test ./internal/agentskill 2>&1 | tail -3` (CLAUDE.md/README are not tested; nothing else) — Expected `ok`.
- [ ] **Step 3: Commit** — message "docs: note meta on GitHub and in the skills".

---

### Task 7: the gate, the alert question

- [ ] **Step 1:** `./test.sh race > <workspace>/race.log 2>&1` (background); read the tail. Expected: the log ends with `all green`.
- [ ] **Step 2: STOP — ask the user** (never post on your own): alerts inside PR review comments are undocumented. Write one rendered sample body per level to the scratchpad and ask whether to (a) post them to a sandbox PR they name, (b) let them paste one themselves, or (c) ship with alerts and check later. If alerts do not render: `RendererFor`'s fallback and `*GH` switch to `GitHubRenderer{NoAlerts: true}` (one-line change + the per-level golden flips to the plain line), commit, re-run the gate.
- [ ] **Step 3:** final whole-branch review (read-only opus subagent), fix pass, then ask before merging.
