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
// waiters and the human. Only a running session reports (its token stops
// working at exit anyway).
func AgentReportVerb(caller, text string, final bool) (AgentReport, error) {
	text = strings.TrimRight(text, "\r\n")
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
	r.mu.Unlock()
	r.bc.Signal()
	SessionStates().PostNotice(ActivityNotice{ID: info.ID, Kind: "report", Label: info.Label, Dir: info.Dir, Text: ReportFirstLine(text)})
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
// while nobody has typed into the session since (spec §5.1 — a report is
// news until somebody talks to the worker). Frontends add the question
// precedence themselves.
func SessionReportOf(id SessionID) (AgentReport, bool) {
	s, ok := Sessions().Get(id)
	if !ok {
		return AgentReport{}, false
	}
	rep, ok := latestReport(FullSessionID(id))
	if !ok || s.LastInput().After(rep.At) {
		return AgentReport{}, false
	}
	return rep, true
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// ReportFirstLine is the notice/row line of a report: its first non-blank
// line, control sequences stripped, cut to reportLineMax runes with an
// ellipsis.
func ReportFirstLine(text string) string {
	text = ansiRe.ReplaceAllString(text, "")
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(strings.ReplaceAll(line, "\r", ""))
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
