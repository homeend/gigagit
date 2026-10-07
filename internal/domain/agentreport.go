package domain

// The report channel (agent orchestration stage 3b): a worker's result
// sentence, stored on its session and read by the parent's agent_wait, by
// agent_list/agent_screen and by the human on the session row.

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// AgentReport is one agent_report.
type AgentReport struct {
	Seq   uint64    `json:"seq"`
	Text  string    `json:"text"`
	Final bool      `json:"final"`
	At    time.Time `json:"at"`
}

// MaxReportBytes caps one report's text; maxReportsKept the reports kept
// per session (the oldest drops; Seq stays monotonic so delivery marks
// survive the drop); reportLineMax the first line shown on rows.
const (
	MaxReportBytes = MaxSendBytes
	maxReportsKept = 20
	reportLineMax  = 120
)

// reportNoticeGap: at most one report notice per session this often — a
// looping worker must not flood the status line and push other notices out
// of the ring. A final report always goes out. Every report is stored.
var reportNoticeGap = 5 * time.Second

// UseReportNoticeGap replaces the gap (tests) and returns the restore.
func UseReportNoticeGap(d time.Duration) func() {
	r := registry()
	r.mu.Lock()
	prev := reportNoticeGap
	reportNoticeGap = d
	r.mu.Unlock()
	return func() { r.mu.Lock(); reportNoticeGap = prev; r.mu.Unlock() }
}

// deliveryMark is what one caller's agent_wait already returned about one
// worker: the Since of the idle/question it delivered, the highest report
// seq, and whether the exit went out.
type deliveryMark struct {
	idleSince, questionSince time.Time
	reportSeq                uint64
	exit                     bool
}

// markOf is caller's mark on worker. Caller holds r.mu.
func (r *spawnRegistry) markOf(caller, worker string) deliveryMark {
	return r.marks[caller][worker]
}

// setMark stores caller's mark on worker. Caller holds r.mu.
func (r *spawnRegistry) setMark(caller, worker string, m deliveryMark) {
	if r.marks[caller] == nil {
		r.marks[caller] = map[string]deliveryMark{}
	}
	r.marks[caller][worker] = m
}

// AgentReportVerb records caller's report about itself and tells the
// waiters and the human. The text is stored free of terminal control
// (cleanReportText): it reaches status lines and terminals. Only a running session reports (its token stops
// working at exit anyway).
func AgentReportVerb(caller, text string, final bool) (AgentReport, error) {
	text = strings.TrimRight(cleanReportText(text), "\n")
	if strings.TrimSpace(text) == "" {
		return AgentReport{}, errors.New("the report is empty")
	}
	if len(text) > MaxReportBytes {
		return AgentReport{}, fmt.Errorf("the report is larger than %d bytes", MaxReportBytes)
	}
	s, err := sessionOf(caller)
	if err != nil {
		return AgentReport{}, err
	}
	info := s.Info()
	if info.State != SessionRunning {
		return AgentReport{}, fmt.Errorf("%s has exited", caller)
	}
	r := registry()
	r.mu.Lock()
	r.prune()
	r.reportSeq++
	rep := AgentReport{Seq: r.reportSeq, Text: text, Final: final, At: time.Now()}
	list := append(r.reports[caller], rep)
	if len(list) > maxReportsKept {
		list = list[len(list)-maxReportsKept:]
	}
	r.reports[caller] = list
	tell := final || rep.At.Sub(r.noticed[caller]) >= reportNoticeGap
	if tell {
		r.noticed[caller] = rep.At
	}
	r.mu.Unlock()
	r.bc.Signal()
	if tell {
		SessionStates().PostNotice(ActivityNotice{ID: info.ID, Kind: "report", Label: info.Title(), Dir: info.Dir, Text: ReportFirstLine(text)})
	} else {
		SessionStates().Wake() // no notice; the frontends still file its tour and repaint
	}
	return rep, nil
}

// AgentReports is every kept report of target, oldest first.
func AgentReports(target string) ([]AgentReport, error) {
	if _, err := sessionOf(target); err != nil {
		return nil, err
	}
	r := registry()
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]AgentReport(nil), r.reports[target]...), nil
}

// latestReport is full's newest report.
func latestReport(full string) (AgentReport, bool) {
	r := registry()
	r.mu.Lock()
	defer r.mu.Unlock()
	list := r.reports[full]
	if len(list) == 0 {
		return AgentReport{}, false
	}
	return list[len(list)-1], true
}

// SessionReportOf is the report the session's row shows: the latest one,
// while the session runs and nobody has typed into it since (spec §5.1 — a
// report is news until somebody talks to the worker; an exited session's
// row says exited and nobody can answer it). Frontends add the question
// precedence themselves.
func SessionReportOf(id SessionID) (AgentReport, bool) {
	s, ok := Sessions().Get(id)
	if !ok || s.Info().State != SessionRunning {
		return AgentReport{}, false
	}
	rep, ok := latestReport(FullSessionID(id))
	if !ok || s.LastInput().After(rep.At) {
		return AgentReport{}, false
	}
	return rep, true
}

// Terminal control sequences a report must never carry to a status line, a
// popup row or a terminal the CLI prints to: string sequences (OSC, DCS,
// APC, PM, SOS — up to BEL or ST), CSI, and two-byte / charset escapes.
var (
	escStringRe = regexp.MustCompile(`\x1b[\]P_^X][^\x07\x1b]*(?:\x07|\x1b\\)?`)
	escCSIRe    = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	escOtherRe  = regexp.MustCompile(`\x1b[ -/]*[0-~]`)
)

// cleanReportText is text without terminal control: escape sequences are
// removed, CRLF becomes LF, and every other control character (C0 but tab
// and newline, DEL, C1) is dropped.
func cleanReportText(text string) string {
	text = escStringRe.ReplaceAllString(text, "")
	text = escCSIRe.ReplaceAllString(text, "")
	text = escOtherRe.ReplaceAllString(text, "")
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f):
			return -1
		}
		return r
	}, strings.ReplaceAll(text, "\r\n", "\n"))
}

// ReportFirstLine is the notice/row line of a report: its first non-blank
// line, free of terminal control, cut to reportLineMax runes with an
// ellipsis.
func ReportFirstLine(text string) string {
	for _, line := range strings.Split(cleanReportText(text), "\n") {
		line = strings.TrimSpace(strings.ReplaceAll(line, "\t", " "))
		if line == "" {
			continue
		}
		if utf8.RuneCountInString(line) <= reportLineMax {
			return line
		}
		rs := []rune(line)
		return string(rs[:reportLineMax-1]) + "…"
	}
	return ""
}
