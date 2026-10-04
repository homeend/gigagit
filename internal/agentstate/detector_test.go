package agentstate

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

func obs(text, title string, progress int) Observation {
	return Observation{Text: text, Lines: Tail(text, 15), Title: title, Progress: progress}
}

// Every screen fixture of the package × every title/progress the captures
// showed × trust: the new pipeline answers what ClassifyWith answered.
// (Removed with the old API in Task 5; the converted tests keep the cases.)
func TestProfileReadEqualsClassifyWith(t *testing.T) {
	t.Parallel()
	screens := []string{workingScreen, hooksScreen, starSpinnerScreen, waitingScreen, questionScreen, typedScreen,
		titledBoxScreen, codexDialog, codexIdle, codexApproval, ownMenuScreen, trustScreen,
		agyIdle, agyWork, agyRun, agyPerm, agyTrustLive, agyDone, codexStart, codexTrust,
		"just some text\nmid redraw\n", " │ >                    │\n ╰────────────────────╯\n", ""}
	titles := []string{"", "◐ x", "◑ Claude Code", "⠂ x", "✳ x", "Claude Code", "⠋ Run | repo", "Run | repo", "Action Required | repo"}
	progress := []int{-1, 0, 1, 2, 3, 4}
	for _, id := range []string{"claude", "codex", "kimi", "junie", "antigravity", "", "mystery"} {
		old, p := DefaultRules(id), ForAgent(id)
		if p.Dedicated != HasDefaults(id) {
			t.Errorf("%s: Dedicated = %v", id, p.Dedicated)
		}
		for _, sc := range screens {
			for _, ti := range titles {
				for _, pr := range progress {
					o := obs(sc, ti, pr)
					rd := p.Read(o)
					for _, trusted := range []bool{false, true} {
						sg := Signal{Title: ti, Progress: pr, Trusted: trusted}
						st, titleIdle := ClassifyWith(old, o.Lines, sg)
						if rd.State != st || (rd.IdleHint && trusted) != titleIdle {
							t.Fatalf("%s %q %q %d trusted=%v: new %q hint=%v, old %q titleIdle=%v",
								id, firstLine(sc), ti, pr, trusted, rd.State, rd.IdleHint, st, titleIdle)
						}
					}
					if want := SignalState(old, Signal{Title: ti, Progress: pr}) == Working; rd.Spinning != want {
						t.Fatalf("%s %q %q %d: Spinning = %v", id, firstLine(sc), ti, pr, rd.Spinning)
					}
					var wantOpts []Option
					if rd.State == Question {
						wantOpts = DialogOptions(o.Text, o.Lines)
					}
					if !slices.Equal(rd.Options, wantOpts) || rd.StepFor != StepDuration(o.Lines) || rd.Progress != Progress(o.Lines) {
						t.Fatalf("%s %q: extras differ", id, firstLine(sc))
					}
				}
			}
		}
	}
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

// The table carries exactly the old maps' patterns (removed with them in Task 5).
func TestAgentTableCopiesTheOldPatterns(t *testing.T) {
	t.Parallel()
	for id, d := range defaults {
		r := agents[id]
		if !slices.Equal(r.screen.Working, d[0]) || !slices.Equal(r.screen.Waiting, d[1]) || !slices.Equal(r.screen.Question, d[2]) || !slices.Equal(r.screen.Own, ownMenus[id]) {
			t.Errorf("%q: screen patterns differ", id)
		}
	}
	if len(agents) != len(defaults) {
		t.Errorf("agents %d, defaults %d", len(agents), len(defaults))
	}
	for id, tr := range titleRules {
		var ti *Title
		for _, d := range agents[id].signals {
			if x, ok := d.(*Title); ok {
				ti = x
			}
		}
		if ti == nil {
			t.Fatalf("%s: no title part", id)
		}
		for i, got := range [][]*regexpT{ti.Working, ti.Question, ti.Idle} {
			var src []string
			for _, re := range got {
				src = append(src, strings.TrimPrefix(re.String(), "(?m)"))
			}
			if !slices.Equal(src, tr[i]) {
				t.Errorf("%s title list %d: %v vs %v", id, i, src, tr[i])
			}
		}
	}
	for id := range agents {
		_, hasProgress := func() (ProgressReport, bool) {
			for _, d := range agents[id].signals {
				if p, ok := d.(ProgressReport); ok {
					return p, true
				}
			}
			return ProgressReport{}, false
		}()
		if hasProgress != progressAgents[id] {
			t.Errorf("%s progress part = %v", id, hasProgress)
		}
	}
}

type regexpT = regexp.Regexp
