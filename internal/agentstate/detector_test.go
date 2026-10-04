package agentstate

import (
	"strings"
	"testing"
)

func obs(text, title string, progress int) Observation {
	return Observation{Text: text, Lines: Tail(text, 15), Title: title, Progress: progress}
}

func firstLine(s string) string { l, _, _ := strings.Cut(strings.TrimSpace(s), "\n"); return l }

type fixed Verdict

func (f fixed) Read(Observation) Verdict { return Verdict(f) }

func TestFirstStopsAtTheFirstVerdict(t *testing.T) {
	t.Parallel()
	o := Observation{Progress: -1}
	cases := []struct {
		name  string
		parts []Detector
		want  Verdict
	}{
		{"decisive first part carries no later hint", []Detector{fixed{State: Question}, fixed{IdleHint: true}, fixed{State: Waiting}}, Verdict{State: Question}},
		{"an earlier hint reaches the deciding part", []Detector{fixed{IdleHint: true}, fixed{State: Waiting}}, Verdict{State: Waiting, IdleHint: true}},
		{"spinning travels with its verdict", []Detector{fixed{State: Working, Spinning: true}, fixed{State: Waiting}}, Verdict{State: Working, Spinning: true}},
		{"no verdict keeps the hints", []Detector{fixed{IdleHint: true}, fixed{}}, Verdict{IdleHint: true}},
		{"empty", nil, Verdict{}},
	}
	for _, c := range cases {
		if got := First(c.parts...).Read(o); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestWithScreen(t *testing.T) {
	t.Parallel()
	// A known agent: the given list replaces its built-in one, the other
	// lists, Own, title and progress stay.
	p, err := WithScreen("claude", nil, nil, []string{`CONFIRM`})
	if err != nil || !p.Dedicated {
		t.Fatalf("err=%v dedicated=%v", err, p.Dedicated)
	}
	for _, c := range []struct {
		o    Observation
		want State
	}{
		{obs("please CONFIRM", "", -1), Question},
		{obs(questionScreen, "", -1), Unknown},                                   // the built-in question list is replaced, not merged
		{obs(waitingScreen, "", -1), Waiting},                                    // built-in waiting kept
		{obs(workingScreen, "", -1), Working},                                    // built-in working kept
		{obs("please CONFIRM\n ❯ 1. View tools\n Esc to back", "", -1), Unknown}, // Own kept
		{obs("noise", "◐ x", -1), Working},                                       // title kept
	} {
		if got := p.Read(c.o).State; got != c.want {
			t.Errorf("claude block %q: %q, want %q", firstLine(c.o.Text), got, c.want)
		}
	}
	k, _ := WithScreen("kimi", nil, []string{`^READY$`}, nil)
	if got := k.Read(obs("noise", "", 3)).State; got != Working {
		t.Errorf("kimi block lost its progress part: %q", got)
	}
	// An unknown command: only its own lists, no Own.
	u, _ := WithScreen("mystery", nil, []string{`^READY$`}, nil)
	if got := u.Read(obs("READY", "", -1)).State; got != Waiting || !u.Dedicated {
		t.Errorf("custom: %q dedicated=%v", got, u.Dedicated)
	}
	if got := u.Read(obs("continue? (y/n)", "", -1)).State; got != Unknown {
		t.Errorf("custom has no generic question list: %q", got)
	}
	if _, err := WithScreen("claude", []string{`(`}, nil, nil); err == nil {
		t.Error("bad pattern accepted")
	}
}

func TestKnown(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"claude", "codex", "junie", "kimi", "antigravity"} {
		if !Known(id) || ForAgent(id).Dedicated != true {
			t.Errorf("%s not known", id)
		}
	}
	if Known("") || Known("mystery") || ForAgent("mystery").Dedicated {
		t.Error("generic ids must not claim dedicated rules")
	}
}
