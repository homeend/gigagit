package agentstate

import "slices"

// recipe is how an agent is read: the signal parts consulted before its
// screen (in order), then its screen rules.
type recipe struct {
	signals []Detector
	screen  ScreenPatterns
}

// agents: what each gg agent id (the exttool tool id) is read by — the one
// place that says which evidence counts for which agent. "" is the generic
// fallback for ids gg has no verified rules for.
var agents = map[string]recipe{
	"claude":      {signals: []Detector{claudeTitle}, screen: claudeScreen},
	"codex":       {signals: []Detector{codexTitle}, screen: codexScreen},
	"kimi":        {signals: []Detector{ProgressReport{}}, screen: kimiScreen},
	"junie":       {screen: junieScreen},
	"antigravity": {screen: agyScreen},
	"":            {screen: genericScreen},
}

// Screen rules per agent. Claude Code entries come from live captures
// (2026-09-08); codex entries are from its TUI's known strings, partly
// verified live since.
var (
	claudeScreen = ScreenPatterns{
		// The spinner line starts with one of Claude Code's glyphs and
		// carries "… (" — matching the glyph, not "(<seconds>", keeps the
		// hooks screen ("✽ Onioning… (running PostToolUse hooks… 1/2 ·
		// 12s …)") in working while the input box is still visible.
		Working: []string{`^[·✢✳✶✻✽*] [^\n]*… \(`, `⎿\s+Running…`},
		// The input box is a ❯ line directly under a horizontal rule —
		// whether or not text is typed in it (an unsubmitted message
		// still means the agent is idle). A user message echoed in the
		// transcript also starts with ❯ but has no rule above it. Once the
		// session has a name the rule carries it ("──── merge conflict
		// resolution ─", Claude Code seen 2026-10-03), so the rule only has
		// to LEAD its line.
		Waiting: []string{`^─{8,}[^\n]*\n❯`},
		// Claude Code's own menus (2.1.289, captured live 2026-10-04): a
		// server menu "Esc to back", the login screens "Press Esc to go
		// back", /config "Esc to close" / "Esc to clear". Its permission
		// dialogs say "Esc to cancel" — a question.
		Own:      []string{`Esc to (?:go )?back\b`, `Esc to close\b`, `Esc to clear\b`},
		Question: []string{`^❯ \d+\.`, `Esc to cancel`, `\(y/n\)`, `\[Y/n\]`, `Do you want to proceed`},
	}
	codexScreen = ScreenPatterns{
		Working: []string{`Working \(\d+`, `(?i)esc to interrupt`},
		// The input box is a › line (placeholder "Ask Codex to do anything"
		// or typed text) directly above the status line "<model> <effort>
		// · <cwd>" (a / root, or a drive on Windows). A user message echoed
		// in the transcript also starts with › but is followed by other
		// text. Captured live 2026-09-08 (codex 0.153.4).
		// At startup (codex 0.160.0, captured 2026-10-03) the status line is
		// not there yet: the box sits over "? for shortcuts".
		Waiting:  []string{`^›[^\n]*\n[^\n]*· (?:/|[A-Za-z]:)`, `^›[^\n]*\n\s*\? for shortcuts`},
		Question: []string{`\(y/n\)`, `\[Y/n\]`, `Press Enter`, `^\s*[›>] \d+\.`},
	}
	// Antigravity CLI 1.2.15, captured live from gg's emulator 2026-10-03: a
	// braille spinner line ("⢿  Generating...", "⡿  Running command...");
	// the input box is a ">" line BETWEEN two rules (the echoed user
	// message has a rule above it only); dialogs are the trust question
	// and the command permission ("Run this command?", "> 1. Yes, run
	// command") with the "↑/↓ Navigate" hint.
	agyScreen = ScreenPatterns{
		Working:  []string{`^[⠁-⣿] `},
		Waiting:  []string{`^─{8,}\n>[^\n]*\n─{8,}`},
		Question: []string{`Do you trust the contents`, `Run this command\?`, `↑/↓ Navigate`, `^\s*> \d+\. `},
	}
	// Junie and kimi: erbrus's agent presets (live captures 2026-09-08/09).
	// Any junie spinner line is working — "Running <cmd>" carries no "esc
	// to stop" suffix and the input box stays visible while it works; the
	// idle box "> Type your prompt..." sits right above the "~ <dir>" bar.
	junieScreen = ScreenPatterns{
		Working:  []string{`^[⠋-⠿] `},
		Waiting:  []string{`^>[^\n]*\n~ `},
		Question: []string{`Trust this project`, `needs your trust decision`, `Allow running this command\?`, `Or reject with a reason`, `space to select`, `Or type your own answer`},
	}
	// Kimi's working rule saw only the retry spinner live: a best guess.
	kimiScreen = ScreenPatterns{
		Working:  []string{`^[🌑🌒🌓🌔🌕🌖🌗🌘] `, `Retrying \(\d+/\d+\)`},
		Waiting:  []string{`^│ >[^\n]*\n╰`},
		Question: []string{`Trust this folder\?`, `Enter select`, `Esc exit`},
	}
	genericScreen = ScreenPatterns{
		Waiting:  []string{`^[❯›>$]\s*$`},
		Question: []string{`\(y/n\)`, `\[Y/n\]`, `Press Enter`, `Do you want to`},
	}
)

// Title rules, from live captures (2026-10-04). Claude Code 2.1.289 titles a
// turn "◐ <topic>" / "◑ <topic>" (◒◓ reserved; braille up to 2.1.227) and an
// idle or a dialog "✳ <topic>" — under a multiplexer always "✳", which is why
// an idle title only hints. Codex 0.160.0 prefixes a braille spinner while
// working, says "Action Required" when blocked (herdr) and drops the glyph
// when idle. Kimi Code 2.1.1 reports OSC 9;4 instead (3 while working, 0 when
// done) — only under hosts it recognises (Windows Terminal, ConEmu, ghostty,
// WezTerm).
var (
	claudeTitle = mustTitle(TitlePatterns{Working: []string{`^[◐◑◒◓⠀-⣿] `}, Idle: []string{`^✳ `}})
	codexTitle  = mustTitle(TitlePatterns{Working: []string{`(?:^| )[⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏](?: |$)`}, Question: []string{`Action Required`}, Idle: []string{`\S`}})
)

// compiled holds every built-in profile, compiled once.
var compiled = func() map[string]Profile {
	out := map[string]Profile{}
	for id, r := range agents {
		s, err := CompileScreen(r.screen)
		if err != nil {
			panic(err) // built-ins are constants; a bad one is a programming error
		}
		out[id] = r.profile(s, id != "")
	}
	return out
}()

func (r recipe) profile(s *Screen, dedicated bool) Profile {
	if len(r.signals) == 0 {
		return Profile{Detector: s, Dedicated: dedicated}
	}
	return Profile{Detector: First(append(slices.Clone(r.signals), Detector(s))...), Dedicated: dedicated}
}

// ForAgent is the built-in profile of an agent id; unknown ids get the
// generic one.
func ForAgent(id string) Profile {
	if p, ok := compiled[id]; ok {
		return p
	}
	return compiled[""]
}

// Known reports whether gg ships dedicated rules for this agent id (the
// generic set does not count).
func Known(id string) bool { _, ok := agents[id]; return ok && id != "" }

// WithScreen is the profile of a command with its own screen_* lists. For a
// known agent each empty list keeps the agent's built-in one and the Own,
// title and progress parts stay; for any other command only the given lists
// apply. Either way the rules are dedicated (they know this program).
func WithScreen(id string, working, waiting, question []string) (Profile, error) {
	r, known := agents[id]
	known = known && id != ""
	if !known {
		r = recipe{}
	}
	p := ScreenPatterns{Working: working, Waiting: waiting, Question: question}
	if known {
		if len(p.Working) == 0 {
			p.Working = r.screen.Working
		}
		if len(p.Waiting) == 0 {
			p.Waiting = r.screen.Waiting
		}
		if len(p.Question) == 0 {
			p.Question = r.screen.Question
		}
		p.Own = r.screen.Own
	}
	s, err := CompileScreen(p)
	if err != nil {
		return Profile{}, err
	}
	return r.profile(s, true), nil
}

func mustTitle(p TitlePatterns) *Title {
	t, err := CompileTitle(p)
	if err != nil {
		panic(err)
	}
	return t
}
