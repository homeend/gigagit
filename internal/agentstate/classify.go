package agentstate

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// State is what the bottom of an agent's screen says it is doing.
type State string

const (
	Unknown  State = ""         // blank, mid-redraw, or nothing matched
	Working  State = "working"  // spinner / running step visible
	Waiting  State = "waiting"  // empty prompt: turn finished
	Question State = "question" // a dialog wants a decision
)

// Rules are the per-agent patterns. Checks are ordered Working →
// Waiting → Own → Question so that a phrase quoted in scrollback can never
// outrank the prompt box that is still on screen. Patterns run in
// multi-line mode over the joined tail lines, so ^ and $ bound a line and
// "\n" lets a rule span two adjacent lines (e.g. rule-line + prompt).
//
// Own are the agent's OWN menus (a settings or server menu the user opened),
// matched against the LAST tail line (their footer): they read as Unknown, so the session keeps the state it had — idle stays
// idle, and a dialog's sub-step keeps its question. Built-in only.
//
// Title* match the agent's window title (OSC 0/2) and ProgressBusy reads its
// OSC 9;4 report: herdr's order puts a title question and a title spinner
// above every screen rule and an idle title below them (ClassifyWith).
// Built-in only.
type Rules struct {
	Working, Waiting, Question             []*regexp.Regexp
	Own                                    []*regexp.Regexp
	TitleWorking, TitleQuestion, TitleIdle []*regexp.Regexp
	ProgressBusy                           bool // OSC 9;4: 1/3 working, 0 idle
}

// defaults: [working, waiting, question] pattern lists per gg agent id
// (the exttool tool id). "" is the generic fallback. Claude Code entries come from live captures
// (2026-09-08); codex entries are from its TUI's known strings, unverified.
var defaults = map[string][3][]string{
	"claude": {
		// The spinner line starts with one of Claude Code's glyphs and
		// carries "… (" — matching the glyph, not "(<seconds>", keeps the
		// hooks screen ("✽ Onioning… (running PostToolUse hooks… 1/2 ·
		// 12s …)") in working while the input box is still visible.
		{`^[·✢✳✶✻✽*] [^\n]*… \(`, `⎿\s+Running…`},
		// The input box is a ❯ line directly under a horizontal rule —
		// whether or not text is typed in it (an unsubmitted message
		// still means the agent is idle). A user message echoed in the
		// transcript also starts with ❯ but has no rule above it. Once the
		// session has a name the rule carries it ("──── merge conflict
		// resolution ─", Claude Code seen 2026-10-03), so the rule only has
		// to LEAD its line.
		{`^─{8,}[^\n]*\n❯`},
		{`^❯ \d+\.`, `Esc to cancel`, `\(y/n\)`, `\[Y/n\]`, `Do you want to proceed`},
	},
	"codex": {
		{`Working \(\d+`, `(?i)esc to interrupt`},
		// The input box is a › line (placeholder "Ask Codex to do anything"
		// or typed text) directly above the status line "<model> <effort>
		// · <cwd>" (a / root, or a drive on Windows). A user message echoed
		// in the transcript also starts with › but is followed by other
		// text. Captured live 2026-09-08 (codex 0.153.4).
		// At startup (codex 0.160.0, captured 2026-10-03) the status line is
		// not there yet: the box sits over "? for shortcuts".
		{`^›[^\n]*\n[^\n]*· (?:/|[A-Za-z]:)`, `^›[^\n]*\n\s*\? for shortcuts`},
		{`\(y/n\)`, `\[Y/n\]`, `Press Enter`, `^\s*[›>] \d+\.`},
	},
	// Antigravity CLI 1.2.15, captured live from gg's emulator 2026-10-03: a
	// braille spinner line ("⢿  Generating...", "⡿  Running command...");
	// the input box is a ">" line BETWEEN two rules (the echoed user
	// message has a rule above it only); dialogs are the trust question
	// and the command permission ("Run this command?", "> 1. Yes, run
	// command") with the "↑/↓ Navigate" hint.
	"antigravity": {
		{`^[⠁-⣿] `},
		{`^─{8,}\n>[^\n]*\n─{8,}`},
		{`Do you trust the contents`, `Run this command\?`, `↑/↓ Navigate`, `^\s*> \d+\. `},
	},
	// Junie and kimi: erbrus's agent presets (live captures 2026-09-08/09).
	// Any junie spinner line is working — "Running <cmd>" carries no "esc
	// to stop" suffix and the input box stays visible while it works; the
	// idle box "> Type your prompt..." sits right above the "~ <dir>" bar.
	"junie": {
		{`^[⠋-⠿] `},
		{`^>[^\n]*\n~ `},
		{`Trust this project`, `needs your trust decision`, `Allow running this command\?`, `Or reject with a reason`, `space to select`, `Or type your own answer`},
	},
	// Kimi's working rule saw only the retry spinner live: a best guess.
	"kimi": {
		{`^[🌑🌒🌓🌔🌕🌖🌗🌘] `, `Retrying \(\d+/\d+\)`},
		{`^│ >[^\n]*\n╰`},
		{`Trust this folder\?`, `Enter select`, `Esc exit`},
	},
	"": {
		nil,
		{`^[❯›>$]\s*$`},
		{`\(y/n\)`, `\[Y/n\]`, `Press Enter`, `Do you want to`},
	},
}

// ownMenus: the footers of an agent's own menus, per gg agent id. Claude
// Code's (2.1.289, captured live 2026-10-04): a server menu "Esc to back",
// the login screens "Press Esc to go back", /config "Esc to close" / "Esc to
// clear". Its permission dialogs say "Esc to cancel" — a question.
var ownMenus = map[string][]string{
	"claude": {`Esc to (?:go )?back\b`, `Esc to close\b`, `Esc to clear\b`},
}

// titleRules: [working, question, idle] title patterns per gg agent id,
// from live captures (2026-10-04). Claude Code 2.1.289 titles a turn
// "◐ <topic>" / "◑ <topic>" (◒◓ reserved; braille up to 2.1.227) and an idle
// or a dialog "✳ <topic>" — under a multiplexer always "✳", which is why
// title-idle is only a fallback. Codex 0.160.0 prefixes a braille spinner
// while working, says "Action Required" when blocked (herdr) and drops the
// glyph when idle.
var titleRules = map[string][3][]string{
	"claude": {{`^[◐◑◒◓⠀-⣿] `}, nil, {`^✳ `}},
	"codex":  {{`(?:^| )[⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏](?: |$)`}, {`Action Required`}, {`\S`}},
}

// progressAgents report their turn through OSC 9;4 (Kimi Code 2.1.1: 3 while
// working, 0 when done).
var progressAgents = map[string]bool{"kimi": true}

// DefaultRules returns the built-in rules for an agent id, or the generic
// set for ids gg has no verified rules for.
func DefaultRules(agentID string) Rules {
	d, ok := defaults[agentID]
	if !ok {
		d = defaults[""]
	}
	r, err := Compile(d[0], d[1], d[2])
	if err != nil {
		panic(err) // built-ins are constants; a bad one is a programming error
	}
	if r.Own, err = compileAll(ownMenus[agentID]); err != nil {
		panic(err)
	}
	tr := titleRules[agentID]
	if r.TitleWorking, err = compileAll(tr[0]); err != nil {
		panic(err)
	}
	if r.TitleQuestion, err = compileAll(tr[1]); err != nil {
		panic(err)
	}
	if r.TitleIdle, err = compileAll(tr[2]); err != nil {
		panic(err)
	}
	r.ProgressBusy = progressAgents[agentID]
	return r
}

// Compile builds Rules from RE2 pattern lists (config overrides).
func Compile(working, waiting, question []string) (Rules, error) {
	var r Rules
	var err error
	if r.Working, err = compileAll(working); err != nil {
		return Rules{}, err
	}
	if r.Waiting, err = compileAll(waiting); err != nil {
		return Rules{}, err
	}
	if r.Question, err = compileAll(question); err != nil {
		return Rules{}, err
	}
	return r, nil
}

func compileAll(ps []string) ([]*regexp.Regexp, error) {
	out := make([]*regexp.Regexp, 0, len(ps))
	for _, p := range ps {
		re, err := regexp.Compile("(?m)" + p)
		if err != nil {
			return nil, fmt.Errorf("agentstate pattern %q: %w", p, err)
		}
		out = append(out, re)
	}
	return out, nil
}

// Tail returns the last n non-empty, whitespace-trimmed lines of text.
func Tail(text string, n int) []string {
	all := strings.Split(text, "\n")
	out := make([]string, 0, n)
	for i := len(all) - 1; i >= 0 && len(out) < n; i-- {
		if l := strings.TrimSpace(all[i]); l != "" {
			out = append(out, l)
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// Classify applies r to the tail lines in structural order.
func Classify(r Rules, lines []string) State {
	text := strings.Join(lines, "\n")
	switch {
	case anyMatch(r.Working, text):
		return Working
	case anyMatch(r.Waiting, text):
		return Waiting
	case len(lines) > 0 && anyMatch(r.Own, lines[len(lines)-1]):
		// Only the last line: a menu's footer sits there, and the same
		// words quoted higher up (a diff the agent asks to apply) must
		// not hide the dialog below them.
		return Unknown
	case anyMatch(r.Question, text):
		return Question
	}
	return Unknown
}

// Signal is what the agent announces outside its screen. Trusted: this
// session has shown a working title or progress before — only then does an
// idle one count (a title frozen at "✳" says nothing).
type Signal struct {
	Title    string
	Progress int // OSC 9;4 state, −1 none
	Trusted  bool
}

// SignalState is what the title and progress say on their own: Question,
// Working, Waiting (idle) or Unknown. Trusted is not consulted.
func SignalState(r Rules, s Signal) State {
	if s.Title != "" {
		switch {
		case anyMatch(r.TitleQuestion, s.Title):
			return Question
		case anyMatch(r.TitleWorking, s.Title):
			return Working
		}
	}
	if r.ProgressBusy && (s.Progress == 1 || s.Progress == 3) {
		return Working
	}
	if s.Title != "" && anyMatch(r.TitleIdle, s.Title) {
		return Waiting
	}
	if r.ProgressBusy && s.Progress == 0 {
		return Waiting
	}
	return Unknown
}

// ClassifyWith classifies the screen together with the signal, first match
// wins: a title question, a title spinner (or busy progress), the screen
// (Classify), and last a trusted idle title when the screen decides
// nothing. titleIdle reports a trusted idle title whichever step decided —
// the watcher holds such an idle for less. An own menu stays Unknown.
func ClassifyWith(r Rules, lines []string, s Signal) (st State, titleIdle bool) {
	says := SignalState(r, s)
	if says == Question || says == Working {
		return says, false
	}
	titleIdle = says == Waiting && s.Trusted
	st = Classify(r, lines)
	if st == Unknown && titleIdle && !(len(lines) > 0 && anyMatch(r.Own, lines[len(lines)-1])) {
		st = Waiting
	}
	return st, titleIdle
}

func anyMatch(res []*regexp.Regexp, text string) bool {
	for _, re := range res {
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

// stepRe matches Claude Code's spinner timer: "… (27s ·", "… (7m 5s ·",
// "… (1h 2m 3s ·".
var stepRe = regexp.MustCompile(`… \((?:(\d+)h )?(?:(\d+)m )?(\d+)s`)

// StepDuration reports how long the current step has been running
// according to the spinner line, 0 when there is none.
func StepDuration(lines []string) time.Duration {
	for _, l := range lines {
		m := stepRe.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		var d time.Duration
		for i, unit := range []time.Duration{time.Hour, time.Minute, time.Second} {
			if m[i+1] != "" {
				var n int
				fmt.Sscanf(m[i+1], "%d", &n)
				d += time.Duration(n) * unit
			}
		}
		return d
	}
	return 0
}

// spinnerGlyphRe: a spinner frame leading a line — Claude Code's glyphs,
// braille (junie, antigravity) and kimi's moon phases.
var spinnerGlyphRe = regexp.MustCompile(`^(?:[·✢✳✶✻✽*]|[⠀-⣿]|[🌑🌒🌓🌔🌕🌖🌗🌘]) `)

// timerRe: an elapsed-time counter ("5s", "7m 12s", "1h 2m 3s").
var timerRe = regexp.MustCompile(`\b(?:\d+h )?(?:\d+m )?\d+s\b`)

// Progress is the tail without what moves while nothing happens — a leading
// spinner glyph and the elapsed-time counters — so two screens with the same
// Progress show no progress between them. Everything else counts, a token
// counter included.
func Progress(lines []string) string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = timerRe.ReplaceAllString(spinnerGlyphRe.ReplaceAllString(l, ""), "")
	}
	return strings.Join(out, "\n")
}

// HasDefaults reports whether gg ships dedicated rules for this agent id
// (the generic set does not count).
func HasDefaults(agentID string) bool { _, ok := defaults[agentID]; return ok && agentID != "" }

// Option is one choice of a dialog: numbered ("› 1. Try new model", Key
// is the digit to press) or cursor-style ("> Yes, I trust this folder" /
// "  No, exit", Key is "pick:<i>" and the server walks the cursor there).
type Option struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Pick    bool   `json:"pick,omitempty"`    // cursor-style: select by moving, then Enter
	Current bool   `json:"current,omitempty"` // the cursor is on this one now
}

// DialogOptions: numbered options when the dialog has them, else the
// cursor-style ones read from the untrimmed screen (indentation matters
// there). lines are the trimmed tail used for classification.
func DialogOptions(raw string, lines []string) []Option {
	if o := Options(lines); len(o) > 0 {
		return o
	}
	return CursorOptions(raw)
}

var optionRe = regexp.MustCompile(`^[❯›>]?\s*(\d)[.)]\s+(.+)$`)

// cursorNumberedRe: the numbered line the dialog's cursor sits on.
var cursorNumberedRe = regexp.MustCompile(`^[❯›>]\s*\d[.)]\s`)

// Options extracts the numbered choices a dialog offers, in screen order,
// so the UI can show real buttons instead of a fixed 1/2/3. The dialog is
// the contiguous run of numbered lines around the line carrying the cursor
// marker — a numbered list in the transcript right above it must not supply
// the labels; with no marker, the LAST numbered run is the dialog.
func Options(lines []string) []Option {
	numbered := make([]bool, len(lines))
	anchor := -1
	for i, l := range lines {
		l = strings.TrimSpace(l)
		numbered[i] = optionRe.MatchString(l)
		if numbered[i] && cursorNumberedRe.MatchString(l) {
			anchor = i
		}
	}
	if anchor < 0 {
		for i := len(lines) - 1; i >= 0; i-- {
			if numbered[i] {
				anchor = i
				break
			}
		}
	}
	if anchor < 0 {
		return nil
	}
	lo, hi := anchor, anchor
	for lo > 0 && numbered[lo-1] {
		lo--
	}
	for hi+1 < len(lines) && numbered[hi+1] {
		hi++
	}
	var out []Option
	seen := map[string]bool{}
	for _, l := range lines[lo : hi+1] {
		m := optionRe.FindStringSubmatch(strings.TrimSpace(l))
		if m == nil || seen[m[1]] {
			continue
		}
		label := strings.TrimSpace(m[2])
		if len([]rune(label)) > 48 {
			label = string([]rune(label)[:47]) + "…"
		}
		seen[m[1]] = true
		out = append(out, Option{Key: m[1], Label: label})
	}
	return out
}
