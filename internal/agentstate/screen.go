package agentstate

import (
	"regexp"
	"strings"
)

// ScreenPatterns are an agent's screen rules as RE2 sources (built-ins in
// agents.go, or a command's screen_* lists).
type ScreenPatterns struct {
	Working, Waiting, Own, Question []string
}

// Screen reads the last tail lines. Checks are ordered Working → Waiting →
// Own → Question so that a phrase quoted in scrollback can never outrank the
// prompt box that is still on screen. Patterns run in multi-line mode over
// the joined tail lines, so ^ and $ bound a line and "\n" lets a rule span
// two adjacent lines (e.g. rule-line + prompt). Own are the agent's OWN menus
// (a settings or server menu the user opened), matched against the LAST tail
// line (their footer): they read as Unknown, so the session keeps the state
// it had — idle stays idle, and a dialog's sub-step keeps its question.
type Screen struct {
	Working, Waiting, Own, Question []*regexp.Regexp
}

// CompileScreen compiles p; a bad pattern is an error naming it.
func CompileScreen(p ScreenPatterns) (*Screen, error) {
	var s Screen
	var err error
	if s.Working, err = compileAll(p.Working); err != nil {
		return nil, err
	}
	if s.Waiting, err = compileAll(p.Waiting); err != nil {
		return nil, err
	}
	if s.Own, err = compileAll(p.Own); err != nil {
		return nil, err
	}
	if s.Question, err = compileAll(p.Question); err != nil {
		return nil, err
	}
	return &s, nil
}

// Read matches o.Lines in the order above; nothing matching, or an Own
// menu, says nothing (Unknown).
func (s *Screen) Read(o Observation) Verdict {
	text := strings.Join(o.Lines, "\n")
	switch {
	case anyMatch(s.Working, text):
		return Verdict{State: Working}
	case anyMatch(s.Waiting, text):
		return Verdict{State: Waiting}
	case len(o.Lines) > 0 && anyMatch(s.Own, o.Lines[len(o.Lines)-1]):
		// Only the last line: a menu's footer sits there, and the same
		// words quoted higher up (a diff the agent asks to apply) must
		// not hide the dialog below them.
		return Verdict{}
	case anyMatch(s.Question, text):
		return Verdict{State: Question}
	}
	return Verdict{}
}
