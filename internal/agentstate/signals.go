package agentstate

import "regexp"

// TitlePatterns are an agent's window-title (OSC 0/2) rules as RE2 sources.
type TitlePatterns struct {
	Working, Question, Idle []string
}

// Title reads the agent's window title: a question or a spinner decides
// (above every screen rule); an idle title only hints — Claude titles ANY
// waiting screen "✳" (a picker opened mid-turn, a dialog the screen rules
// miss), so it may confirm the screen's idle, never create one.
type Title struct {
	Working, Question, Idle []*regexp.Regexp
}

func CompileTitle(p TitlePatterns) (*Title, error) {
	var t Title
	var err error
	if t.Working, err = compileAll(p.Working); err != nil {
		return nil, err
	}
	if t.Question, err = compileAll(p.Question); err != nil {
		return nil, err
	}
	if t.Idle, err = compileAll(p.Idle); err != nil {
		return nil, err
	}
	return &t, nil
}

func (t *Title) Read(o Observation) Verdict {
	if o.Title == "" {
		return Verdict{}
	}
	switch {
	case anyMatch(t.Question, o.Title):
		return Verdict{State: Question}
	case anyMatch(t.Working, o.Title):
		return Verdict{State: Working, Spinning: true}
	case anyMatch(t.Idle, o.Title):
		return Verdict{IdleHint: true}
	}
	return Verdict{}
}

// ProgressReport reads the OSC 9;4 report: 1 (normal) and 3 (busy) are
// working, 0 (clear) hints idle; 2 (error) and 4 (paused) say nothing.
type ProgressReport struct{}

func (ProgressReport) Read(o Observation) Verdict {
	switch o.Progress {
	case 1, 3:
		return Verdict{State: Working, Spinning: true}
	case 0:
		return Verdict{IdleHint: true}
	}
	return Verdict{}
}
