package agentstate

import (
	"strings"
	"testing"
	"time"
)

// Captured 2026-09-08 from live Claude Code windows (ANSI already stripped).
const workingScreen = `  ⎿  Running…
✻ Cogitating… (27s · ↓ 1.5k tokens · thought for 1s)
  ⎿  Tip: Use git worktrees to run multiple Claude sessions in parallel.
──────────────────────────────────────────────────────────────────────
❯ 
──────────────────────────────────────────────────────────────────────
  homeend@homeend-p14s  /mnt/t/others/erbrus (main)  Fable 5.1  19% of 1M
  ⏵⏵ bypass permissions on (shift+tab to cycle) · ← 2 agents
`

// Captured 2026-10-01 while PostToolUse hooks ran: the spinner line has
// no "(Ns" counter and the input box stays on screen, so the old rule
// `\S+… \(\d+` read this as idle mid-task.
const hooksScreen = `⏺ Bash(git status)
  ⎿  On branch main
✽ Onioning… (running PostToolUse hooks… 1/2 · 12s · ↓ 1.2k tokens)
──────────────────────────────────────────────────────────────────────
❯
──────────────────────────────────────────────────────────────────────
  homeend@homeend-p14s  /mnt/t/others/erbrus (main)  Opus 5.5  12% of 1M
  ⏵⏵ bypass permissions on (shift+tab to cycle)
`

// Same shape with the other spinner glyphs Claude Code cycles through.
const starSpinnerScreen = `* Thinking… (3s · ↑ 0 tokens)
──────────────────────────────────────────────────────────────────────
❯
──────────────────────────────────────────────────────────────────────
  homeend@homeend-p14s  /mnt/t/others/erbrus (main)  Opus 5.5  12% of 1M
`

const waitingScreen = `※ recap: Adding parent checkout rows is done. Do you want to proceed with
  the upload when you are ready? Press Enter when done. (disable recaps in
  /config)
──────────────────────────────────────────────────────────────────────
❯ 
──────────────────────────────────────────────────────────────────────
  homeend@homeend-p14s  ~/git-focus (master)  Fable 5.1  14% of 1M
  ⏵⏵ bypass permissions on (shift+tab to cycle) · ← 2 agents
`

const questionScreen = `⏺ I need to delete the old migration file first.

 Do you want to proceed?
 ❯ 1. Yes
   2. Yes, and don't ask again this session
   3. No, and tell Claude what to do differently
 Esc to cancel
`

func TestTail(t *testing.T) {
	got := Tail("a\n\n  b  \nc\n\n", 2)
	if strings.Join(got, "|") != "b|c" {
		t.Errorf("got %v", got)
	}
}

func TestClassifyClaudeCode(t *testing.T) {
	r := ForAgent("claude")
	cases := map[string]State{
		workingScreen:               Working,
		hooksScreen:                 Working, // hooks running: busy although the input box is visible
		starSpinnerScreen:           Working,
		waitingScreen:               Waiting, // scrollback phrases must not win over the prompt box
		questionScreen:              Question,
		"":                          Unknown,
		"just some text\nno prompt": Unknown,
	}
	for in, want := range cases {
		if got := read(r, Tail(in, 15)); got != want {
			t.Errorf("%q: got %q want %q", in[:min(len(in), 30)], got, want)
		}
	}
}

// Captured 2026-09-08: turn finished, next message pasted into the input
// box but not submitted. Idle all the same.
const typedScreen = `● Sent (message 23) — the README.md diff adding the [ui] diff_syntax paragraph.
✻ Crunched for 12s · done 2:28 AM
──────────────────────────────────────────────────────────────────────
❯ provide diff of the next modified file
──────────────────────────────────────────────────────────────────────
  homeend@homeend-p14s  /mnt/…/gigagit.worktrees/feat-diff-syntax-highlighting  Sonnet 5
  ⏵⏵ auto mode on (shift+tab to cycle) · ← 2 agents
`

func TestClassifyTypedButUnsubmitted(t *testing.T) {
	if got := read(ForAgent("claude"), Tail(typedScreen, 15)); got != Waiting {
		t.Errorf("got %q want waiting", got)
	}
	// A user message echoed in the transcript (no rule above it) is not
	// the input box.
	echo := "❯ do the thing\n  (sent from the erbrus chat)\n● Working on it\n"
	if got := read(ForAgent("claude"), Tail(echo, 15)); got != Unknown {
		t.Errorf("echo: got %q want unknown", got)
	}
}

// Reported 2026-10-03 (screenshot, Claude Code with Opus 5.5): once a
// session has a name, Claude Code writes it into the input box's top rule.
// The turn is over; the row kept saying "working" because the idle rule
// wanted a rule of dashes only.
const titledBoxScreen = `  - git worktree prune would remove the two stale T:/ worktree entries.
  - The leftover gg autostashes might be worth clearing if they're no longer needed.

  Neither is urgent for a fixture repo, and I haven't changed anything. If you'd like this as a shareable
  page, I can publish it as one.

✻ Cooked for 37s · done 12:28 PM

──────────────────────────────────────────────────────────────────── merge conflict resolution orderservice ─
❯
──────────────────────────────────────────────────────────────────────────────────────────────────────────────
  homeend@homeend-p14s  /mnt/…/test-1.worktrees/b (conflict-10-a-20261002-222629*)  Opus 5.5  6% of 1M  5h …
  ⏵⏵ bypass permissions on (shift+tab to cycle) · ← for agents
`

func TestClassifyTitledInputBox(t *testing.T) {
	if got := read(ForAgent("claude"), Tail(titledBoxScreen, 15)); got != Waiting {
		t.Errorf("titled box: got %q want waiting", got)
	}
	// Typed but unsubmitted text in a titled box is still idle.
	typed := strings.Replace(titledBoxScreen, "\n❯\n", "\n❯ next question\n", 1)
	if got := read(ForAgent("claude"), Tail(typed, 15)); got != Waiting {
		t.Errorf("titled box with text: got %q want waiting", got)
	}
	// A user message echoed under a line that merely STARTS with text is
	// not the box: the rule must lead the line.
	echo := "see ──────── here\n❯ do the thing\n● Working on it\n"
	if got := read(ForAgent("claude"), Tail(echo, 15)); got != Unknown {
		t.Errorf("echo: got %q want unknown", got)
	}
}

func TestClassifyGenericAndCodex(t *testing.T) {
	if got := read(ForAgent("mystery"), Tail("run tests? (y/n)", 5)); got != Question {
		t.Errorf("generic question: %q", got)
	}
	if got := read(ForAgent("mystery"), Tail("$ ", 5)); got != Waiting {
		t.Errorf("generic waiting: %q", got)
	}
	if got := read(ForAgent("codex"), Tail("• Working (12s • esc to interrupt)\n›", 5)); got != Working {
		t.Errorf("codex working: %q", got)
	}
}

func TestCompileOverridesAndErrors(t *testing.T) {
	r, err := WithScreen("", []string{`Thinking \(`}, []string{`^>\s*$`}, []string{`CONFIRM`})
	if err != nil {
		t.Fatal(err)
	}
	if got := read(r, Tail("please CONFIRM", 5)); got != Question {
		t.Errorf("override: %q", got)
	}
	if _, err := WithScreen("", []string{`(`}, nil, nil); err == nil {
		t.Error("bad pattern accepted")
	}
}

func TestStepDuration(t *testing.T) {
	cases := map[string]time.Duration{
		"✻ Cogitating… (27s · ↓ 1.5k tokens)": 27 * time.Second,
		"✻ Precipitating… (7m 5s · ↓ 5.2k)":   7*time.Minute + 5*time.Second,
		"✶ Working… (1h 2m 3s · x)":           time.Hour + 2*time.Minute + 3*time.Second,
		"no timer here":                       0,
	}
	for in, want := range cases {
		if got := StepDuration([]string{in}); got != want {
			t.Errorf("%q: got %v want %v", in, got, want)
		}
	}
}

// Captured live 2026-09-08 from codex 0.153.4 at startup.
const codexDialog = `  GPT-5.4 Mini will be deprecated soon
  Codex now uses GPT-5.6 Luna in place of GPT-5.4 Mini. Switch to GPT-5.6 Luna to continue.
  Choose how you'd like Codex to proceed.
› 1. Try new model
  2. Use existing model
  Use ↑/↓ to move, press enter to confirm
`

func TestCodexDialogIsQuestionWithOptions(t *testing.T) {
	lines := Tail(codexDialog, 15)
	if got := read(ForAgent("codex"), lines); got != Question {
		t.Fatalf("state = %q", got)
	}
	opts := Options(lines)
	if len(opts) != 2 || opts[0].Key != "1" || opts[0].Label != "Try new model" || opts[1].Label != "Use existing model" {
		t.Fatalf("options = %+v", opts)
	}
	if got := Options(Tail(questionScreen, 15)); len(got) != 3 || got[2].Key != "3" {
		t.Fatalf("claude options = %+v", got)
	}
	if got := Options([]string{"❯", "no options here", "2024. a year"}); len(got) != 0 {
		t.Fatalf("false options = %+v", got)
	}
}

// Captured live 2026-09-08 from codex 0.153.4 after it answered in plain
// text and returned to its prompt (placeholder text in the input box).
const codexIdle = `› ask me using ask user tool: what deser I prefer: icecream, rolls, or fruits?
  (sent from the erbrus chat — the sender sees only the channel, not this terminal)
• I can’t use the ask-user tool in this mode.
  What dessert do you prefer: icecream, rolls, or fruits?
› Ask Codex to do anything
  gpt-5.4-mini low · /mnt/t/others/gigagit.worktrees/chore-comment-audit
`

func TestCodexIdleAndTyped(t *testing.T) {
	r := ForAgent("codex")
	if got := read(r, Tail(codexIdle, 15)); got != Waiting {
		t.Fatalf("idle: %q", got)
	}
	typed := strings.Replace(codexIdle, "› Ask Codex to do anything", "› rolls, thanks", 1)
	if got := read(r, Tail(typed, 15)); got != Waiting {
		t.Fatalf("typed-but-unsubmitted: %q", got)
	}
	// The echoed user message alone (no status line under it) is not the box.
	if got := read(r, Tail("› do the thing\n• Working on it\n", 15)); got != Unknown {
		t.Fatalf("echo: %q", got)
	}
	if got := read(r, Tail("• Working (12s • Esc to interrupt)\n› Ask Codex to do anything\n  gpt-5 low · /x\n", 15)); got != Working {
		t.Fatalf("working: %q", got)
	}
}

// Codex command-approval dialog, from a screenshot 2026-09-08.
const codexApproval = `  Would you like to run the following command?
  Environment: local
  Reason: Need to send the requested report through the local erbrus service.
  $ /mnt/t/others/erbrus/bin/erbrus msg send --report 'Current worktree is clean.'
› 1. Yes, proceed (y)
  2. Yes, and don't ask again for commands that start with ` + "`" + `/mnt/t/others/erbrus/bin/erbrus msg send --report` + "`" + ` (p)
  3. No, and tell Codex what to do differently (esc)
  Press enter to confirm or esc to cancel
`

func TestCodexApprovalDialog(t *testing.T) {
	lines := Tail(codexApproval, 15)
	if got := read(ForAgent("codex"), lines); got != Question {
		t.Fatalf("state = %q", got)
	}
	opts := Options(lines)
	if len(opts) != 3 || opts[0].Label != "Yes, proceed (y)" || opts[2].Key != "3" {
		t.Fatalf("options = %+v", opts)
	}
}

// Junie and kimi rules come from erbrus's agent presets (live captures).
func TestClassifyJunieAndKimi(t *testing.T) {
	j := ForAgent("junie")
	if got := read(j, Tail("⠹ Running tests esc to stop\n> Type your prompt...\n~ /work/x\n", 15)); got != Working {
		t.Errorf("junie working: %q", got)
	}
	if got := read(j, Tail("Done.\n> Type your prompt...\n~ /work/x\n", 15)); got != Waiting {
		t.Errorf("junie idle: %q", got)
	}
	if got := read(j, Tail("    Junie needs your trust decision\n  → Trust this project\n    Keep untrusted\n", 15)); got != Question {
		t.Errorf("junie question: %q", got)
	}
	k := ForAgent("kimi")
	// gg's Text() puts a space after a wide glyph (its right-half cell).
	if got := read(k, Tail("🌒  Thinking\n", 15)); got != Working {
		t.Errorf("kimi working (wide glyph): %q", got)
	}
	if got := read(k, Tail("│ > \n╰──────╯\n", 15)); got != Waiting {
		t.Errorf("kimi idle: %q", got)
	}
}

func TestKnownAndGenericFallback(t *testing.T) {
	for _, id := range []string{"claude", "codex", "junie", "kimi", "antigravity"} {
		if !Known(id) {
			t.Errorf("%s has no rules", id)
		}
	}
	if Known("") || Known("mystery") {
		t.Error("generic ids must not claim dedicated rules")
	}
	if got := read(ForAgent("mystery"), Tail("continue? (y/n)", 5)); got != Question {
		t.Errorf("generic fallback: %q", got)
	}
}

// Captured live 2026-10-04 (Claude Code 2.1.289): an MCP server's menu the
// USER opened from /mcp. Numbered like a permission dialog, but its footer
// steps back inside Claude's own menus — not the agent asking anything.
const ownMenuScreen = `     Docs: https://code.claude.com/docs/en/sub-agents
▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔
   Claude.ai Claude Docs MCP Server
   Status:           ✔ connected
   Protocol:         2026-07-28
   URL:              https://api.anthropic.com
   Config location:  claude.ai
   Capabilities: tools
   Tools: 8 tools
   ❯ 1. View tools
     2. Clear authentication
     3. Reconnect
     4. Disable
   ↑/↓ to navigate · Enter to select · Esc to back
`

// Captured live 2026-10-04: the trust dialog at start (cursor-style).
const trustScreen = ` Claude Code'll be able to read, edit, and execute files here.
 Security guide
 ❯ No, exit
   Yes, I trust this folder
 Enter to confirm · Esc to cancel
`

// Claude's own menus (their footers step back, close or clear) read as
// unknown, so a session keeps the state it had; the agent's dialogs (Esc to
// cancel) stay questions.
func TestClaudeOwnMenusAreNotQuestions(t *testing.T) {
	r := ForAgent("claude")
	cases := map[string]State{
		ownMenuScreen: Unknown,
		"   Settings\n   ❯ Auto-compact   true\n   Enter/Space to change · / to search · Esc to close\n": Unknown,
		"   Press Esc to go back\n": Unknown,
		"   Type to filter · Enter/↓ to select · ↑ to tabs · Esc to clear\n": Unknown,
		questionScreen: Question,
		trustScreen:    Question,
	}
	for in, want := range cases {
		if got := read(r, Tail(in, 15)); got != want {
			t.Errorf("%q: got %q want %q", in[max(0, len(in)-50):], got, want)
		}
	}
}

// StallKey ignores what moves while nothing happens — the spinner's glyph
// and its timer (live 2026-10-04: a think shows only "(5s · thinking with
// high effort)") — and keeps everything else, the token counter included.
func TestStallKeyIgnoresTheSpinnerAndItsTimer(t *testing.T) {
	a := StallKey(Tail("● Read 2 files\n✶ Slithering… (5s · thinking with high effort)\n❯ \n", 15))
	b := StallKey(Tail("● Read 2 files\n* Slithering… (7m 12s · thinking with high effort)\n❯ \n", 15))
	if a != b {
		t.Fatalf("glyph/timer changed the progress:\n%q\n%q", a, b)
	}
	c := StallKey(Tail("● Read 2 files\n✶ Slithering… (8s · ↓ 1.2k tokens)\n❯ \n", 15))
	d := StallKey(Tail("● Read 2 files\n✶ Slithering… (9s · ↓ 1.3k tokens)\n❯ \n", 15))
	if c == d {
		t.Fatal("a moving token counter is progress")
	}
	if StallKey(Tail("⠋ Running cargo build\n", 15)) != StallKey(Tail("⠙ Running cargo build\n", 15)) {
		t.Fatal("a braille spinner frame is not progress")
	}
	if StallKey(Tail("● Read 2 files\n", 15)) == StallKey(Tail("● Read 3 files\n", 15)) {
		t.Fatal("new transcript text is progress")
	}
}

// An own-menu footer is the LAST line: the same words quoted higher up — in
// the diff of an edit the agent asks to make — must not hide the dialog.
func TestOwnMenuFooterOnlyOnTheLastLine(t *testing.T) {
	editDialog := ` Edit file README.md
   12 -Press Esc to close the panel.
   12 +Press Esc to close or Esc to clear the filter; Esc to back out.
 Do you want to make this edit to README.md?
 ❯ 1. Yes
   2. Yes, allow all edits during this session
   3. No, and tell Claude what to do differently
 Esc to cancel
`
	if got := read(ForAgent("claude"), Tail(editDialog, 15)); got != Question {
		t.Fatalf("a quoted footer hid the edit dialog: %q", got)
	}
	if got := read(ForAgent("claude"), Tail(ownMenuScreen, 15)); got != Unknown {
		t.Fatalf("the menu itself: %q", got)
	}
}

// read is the state a profile reads from a screen with no title or progress.
func read(p Profile, lines []string) State {
	return p.Read(Observation{Text: strings.Join(lines, "\n"), Lines: lines, Progress: -1}).State
}

// Captured live 2026-10-04 (Claude Code 2.1.289, Codex 0.160.0, Kimi 2.1.1).
// The signal parts read over an empty screen: a question or a spinner
// decides; an idle title only hints.
func TestSignalParts(t *testing.T) {
	t.Parallel()
	const hint State = "idle hint"
	cases := []struct {
		name     string
		id       string
		title    string
		progress int
		want     State
	}{
		{"claude ◐", "claude", "◐ Math and shell command sequence", -1, Working},
		{"claude ◑", "claude", "◑ Claude Code", -1, Working},
		{"claude ◓ (2.1.228+ set)", "claude", "◓ x", -1, Working},
		{"claude braille (≤2.1.227)", "claude", "⠂ Claude Code", -1, Working},
		{"claude ✳", "claude", "✳ Claude Code", -1, hint},
		{"claude empty (exit)", "claude", "", -1, Unknown},
		{"claude plain", "claude", "Claude Code", -1, Unknown},
		{"codex spinner", "codex", "⠋ Run sleep 5 command | repo", -1, Working},
		{"codex spinner alone", "codex", "⠹ repo", -1, Working},
		{"codex idle", "codex", "Run sleep 5 command | repo", -1, hint},
		{"codex action required", "codex", "Action Required | repo", -1, Question},
		{"codex empty", "codex", "", -1, Unknown},
		{"kimi busy", "kimi", "Run the shell", 3, Working},
		{"kimi normal", "kimi", "", 1, Working},
		{"kimi clear", "kimi", "", 0, hint},
		{"kimi error", "kimi", "", 2, Unknown},
		{"kimi none", "kimi", "", -1, Unknown},
		{"junie title", "junie", "◐ Junie", -1, Unknown},
		{"claude ignores progress", "claude", "", 3, Unknown},
	}
	for _, c := range cases {
		v := ForAgent(c.id).Read(Observation{Title: c.title, Progress: c.progress})
		got := v.State
		if got == Unknown && v.IdleHint {
			got = hint
		}
		if got != c.want || v.Spinning != (c.want == Working) {
			t.Errorf("%s: %q spinning=%v, want %q", c.name, got, v.Spinning, c.want)
		}
	}
}

func TestProfileReadsSignalsBeforeTheScreen(t *testing.T) {
	t.Parallel()
	claude := ForAgent("claude")
	idleBox := Tail("done\n────────────────────\n❯ \n────────────────────\n", 15)
	spinner := Tail("✻ Cogitating… (27s · ↓ 1.5k tokens)\n────────────────────\n❯ \n", 15)
	dialog := Tail(" Do you want to create b.txt?\n ❯ 1. Yes\n   2. Yes, and switch\n   3. No\n Esc to cancel · Tab to amend\n", 15)
	noise := Tail("just some text\nmid redraw\n", 15)
	ownMenu := Tail(" ❯ 1. View tools\n Esc to back\n", 15)
	cases := []struct {
		name     string
		lines    []string
		title    string
		want     State
		wantHint bool
	}{
		{"◐ over the idle box (between two steps)", idleBox, "◐ x", Working, false},
		{"◐ over noise", noise, "◐ x", Working, false},
		{"dialog under ✳", dialog, "✳ x", Question, true},
		{"✳ over the idle box", idleBox, "✳ x", Waiting, true},
		{"✳ over noise (a title confirms an idle, never creates one)", noise, "✳ x", Unknown, true},
		{"static ✳ over a spinner", spinner, "✳ x", Working, true},
		{"own menu under ✳", ownMenu, "✳ x", Unknown, true},
		{"no signal", idleBox, "", Waiting, false},
	}
	for _, c := range cases {
		rd := claude.Read(Observation{Lines: c.lines, Title: c.title, Progress: -1})
		if rd.State != c.want || rd.IdleHint != c.wantHint {
			t.Errorf("%s: %q hint=%v, want %q hint=%v", c.name, rd.State, rd.IdleHint, c.want, c.wantHint)
		}
	}
	if st := ForAgent("codex").Read(Observation{Lines: idleBox, Title: "Action Required | repo", Progress: -1}).State; st != Question {
		t.Errorf("codex action required: %q", st)
	}
}
