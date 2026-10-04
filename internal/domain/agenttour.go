package domain

// Agent tours (stage 4): a worker's brief and an agent's latest report as
// overview documents in the session's worktree. domain composes them; the
// frontends file them in the agent-docs store (agentdocs.Store.FileTour).

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// TourMaxBytes is the overview cap a tour's text must fit (agentdocs'
// MaxOverviewBytes; a tui test pins the two equal — domain does not import
// agentdocs).
const TourMaxBytes = 64 << 10

// TourMaxTitle is the overview title cap in runes (agentdocs'
// MaxOverviewTitle; the same tui test pins it).
const TourMaxTitle = 200

// tourCutLine closes a brief cut to TourMaxBytes.
const tourCutLine = "… cut here — the worker has the whole brief (agent_task).\n"

// AgentTourDoc is one tour as the store files it.
type AgentTourDoc struct {
	Key   string // "brief:<full id>" | "report:<full id>"
	Root  string // CheckoutKey(Dir): the store's worktree key
	Dir   string // the session's worktree on disk
	Title string
	Text  string
	Seq   uint64 // report: its seq; brief: 0
}

// AgentTourKinds says which tours full has: a brief (a spawned worker with a
// brief) and a report (it reported at least once).
func AgentTourKinds(full string) (brief, report bool) {
	if rec, ok := AgentRecord(full); ok && rec.Spawned && strings.TrimSpace(rec.Brief) != "" {
		brief = true
	}
	_, report = latestReport(full)
	return brief, report
}

// AgentTour composes full's tour of kind "brief" or "report".
func AgentTour(full, kind string) (AgentTourDoc, error) {
	if kind != "brief" && kind != "report" {
		return AgentTourDoc{}, errors.New(`kind must be "brief" or "report"`)
	}
	s, err := sessionOf(full)
	if err != nil {
		return AgentTourDoc{}, err
	}
	info := s.Info()
	title := func(kind string) string {
		return tourTitle(kind, info.Label, filepath.Base(info.Dir), info.Started.Local().Format("15:04"))
	}
	d := AgentTourDoc{Key: kind + ":" + full, Root: CheckoutKey(info.Dir), Dir: info.Dir}
	if kind == "brief" {
		rec, ok := AgentRecord(full)
		if !ok || !rec.Spawned || strings.TrimSpace(rec.Brief) == "" {
			return AgentTourDoc{}, fmt.Errorf("%s has no brief: an agent did not start it", full)
		}
		d.Title, d.Text = title("Brief"), cutTourText(rec.Brief)
		return d, nil
	}
	rep, ok := latestReport(full)
	if !ok {
		return AgentTourDoc{}, fmt.Errorf("%s has not reported", full)
	}
	d.Title, d.Text, d.Seq = title("Report"), rep.Text, rep.Seq
	if rep.Final {
		d.Title = title("Final report")
	}
	return d, nil
}

// cutTourText fits text into TourMaxBytes: whole lines, then tourCutLine.
func cutTourText(text string) string {
	if len(text) <= TourMaxBytes {
		return text
	}
	limit := TourMaxBytes - len(tourCutLine)
	cut := strings.LastIndexByte(text[:limit], '\n')
	return text[:cut+1] + tourCutLine // cut -1 (one giant line): only the cut line
}

// tourTitle is "<kind> — <label> · <worktree> (<hh:mm>)" on one line and
// within TourMaxTitle runes: a long label or worktree name is cut in the
// middle (the longer one gives way first), the kind and the time stay.
func tourTitle(kind, label, wt, at string) string {
	oneLine := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	label, wt = oneLine(label), oneLine(wt)
	n := utf8.RuneCountInString
	room := TourMaxTitle - n(kind+" — "+" · "+" ("+at+")")
	if n(label)+n(wt) > room {
		half := room / 2
		switch {
		case n(label) <= half:
			wt = cutMiddle(wt, room-n(label))
		case n(wt) <= half:
			label = cutMiddle(label, room-n(wt))
		default:
			label, wt = cutMiddle(label, half), cutMiddle(wt, room-half)
		}
	}
	return kind + " — " + label + " · " + wt + " (" + at + ")"
}

// cutMiddle is s in at most max runes, its middle replaced by "…".
func cutMiddle(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max < 1 {
		return ""
	}
	left := max / 2
	right := max - 1 - left
	return string(r[:left]) + "…" + string(r[len(r)-right:])
}
