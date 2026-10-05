package agentstate

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// State is what an agent is doing, as its profile reads it.
type State string

const (
	Unknown  State = ""         // blank, mid-redraw, or nothing matched
	Working  State = "working"  // spinner / running step visible
	Waiting  State = "waiting"  // empty prompt: turn finished
	Question State = "question" // a dialog wants a decision
)

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

// StallKey is the tail without what moves while nothing happens — a leading
// spinner glyph and the elapsed-time counters — so two screens with the same
// key show no progress between them. Everything else counts, a token
// counter included.
func StallKey(lines []string) string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = timerRe.ReplaceAllString(spinnerGlyphRe.ReplaceAllString(l, ""), "")
	}
	return strings.Join(out, "\n")
}

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
