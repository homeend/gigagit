package domain

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestAgentReportStoredAndRead(t *testing.T) {
	_, wt, _, ov := spawnFixture(t, 4)
	res, sess, err := SpawnAgent(context.Background(), SpawnSpec{Req: AgentStartRequest{Caller: ov, Worktree: wt, Tool: "Sleeper", Prompt: "x"}, Cols: 80, Rows: 24, MCPURL: "http://x", Approved: approveAll})
	if err != nil {
		t.Fatal(err)
	}
	restore := UseSessionStates(NewStaticStates(nil))
	defer restore()
	seq0 := SessionStates().NoticeSeq()
	rep, err := AgentReportVerb(res.ID, "merged feat/x\ntwo tests skipped\n", false)
	if err != nil || rep.Seq == 0 || rep.Final || rep.Text != "merged feat/x\ntwo tests skipped" {
		t.Fatalf("report = %+v %v", rep, err)
	}
	if _, err := AgentReportVerb(res.ID, "  \n", false); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("an empty report must be refused: %v", err)
	}
	if _, err := AgentReportVerb(res.ID, strings.Repeat("x", MaxReportBytes+1), false); err == nil {
		t.Fatal("an oversized report must be refused")
	}
	list, err := AgentReports(res.ID)
	if err != nil || len(list) != 1 || list[0].Seq != rep.Seq {
		t.Fatalf("reports = %+v %v", list, err)
	}
	if got, ok := SessionReportOf(sess.Info().ID); !ok || got.Seq != rep.Seq {
		t.Fatalf("SessionReportOf = %+v %v", got, ok)
	}
	// Level reads.
	var row AgentEntry
	for _, e := range AgentList(ov) {
		if e.ID == res.ID {
			row = e
		}
	}
	if row.ReportAt.IsZero() || row.ReportFinal {
		t.Fatalf("row %+v", row)
	}
	sc, _ := AgentScreen(res.ID)
	if sc.Report == nil || sc.Report.Seq != rep.Seq {
		t.Fatalf("screen report = %+v", sc.Report)
	}
	// One notice of kind report, with the first line.
	ns := SessionStates().Notices(seq0)
	if len(ns) != 1 || ns[0].Kind != "report" || ns[0].Text != "merged feat/x" || ns[0].ID != sess.Info().ID {
		t.Fatalf("notices = %+v", ns)
	}
	// Answered: input after the report clears the badge view, not the store.
	time.Sleep(2 * time.Millisecond)
	sess.SendText("ok")
	if _, ok := SessionReportOf(sess.Info().ID); ok {
		t.Fatal("an answered report must not show on the row")
	}
	if list, _ = AgentReports(res.ID); len(list) != 1 {
		t.Fatal("answering must not drop the report")
	}
	// A final report replaces the badge.
	fin, _ := AgentReportVerb(res.ID, "done", true)
	if got, ok := SessionReportOf(sess.Info().ID); !ok || !got.Final || got.Seq != fin.Seq {
		t.Fatalf("final badge = %+v %v", got, ok)
	}
	for _, e := range AgentList(ov) {
		if e.ID == res.ID && !e.ReportFinal {
			t.Fatal("report_final must follow the latest report")
		}
	}
}

func TestAgentReportCapKeepsSeqs(t *testing.T) {
	_, wt, _, ov := spawnFixture(t, 4)
	res, _, err := SpawnAgent(context.Background(), SpawnSpec{Req: AgentStartRequest{Caller: ov, Worktree: wt, Tool: "Sleeper", Prompt: "x"}, Cols: 80, Rows: 24, MCPURL: "http://x", Approved: approveAll})
	if err != nil {
		t.Fatal(err)
	}
	defer UseSessionStates(NewStaticStates(nil))()
	first, _ := AgentReportVerb(res.ID, "r", false)
	for i := 1; i < maxReportsKept+5; i++ {
		if _, err := AgentReportVerb(res.ID, "r", false); err != nil {
			t.Fatal(err)
		}
	}
	list, _ := AgentReports(res.ID)
	if len(list) != maxReportsKept || list[0].Seq != first.Seq+5 || list[len(list)-1].Seq != first.Seq+uint64(maxReportsKept+4) {
		t.Fatalf("kept %d, first %d, last %d (first ever %d)", len(list), list[0].Seq, list[len(list)-1].Seq, first.Seq)
	}
}

func TestAgentReportRefusesExitedAndPrunesOnRemove(t *testing.T) {
	_, wt, _, ov := spawnFixture(t, 4)
	res, sess, err := SpawnAgent(context.Background(), SpawnSpec{Req: AgentStartRequest{Caller: ov, Worktree: wt, Tool: "Sleeper", Prompt: "x"}, Cols: 80, Rows: 24, MCPURL: "http://x", Approved: approveAll})
	if err != nil {
		t.Fatal(err)
	}
	defer UseSessionStates(NewStaticStates(nil))()
	if _, err := AgentReportVerb(res.ID, "before exit", true); err != nil {
		t.Fatal(err)
	}
	_ = Sessions().Kill(sess.Info().ID)
	<-sess.Done()
	if _, err := AgentReportVerb(res.ID, "after exit", false); err == nil || !strings.Contains(err.Error(), "exited") {
		t.Fatalf("an exited session cannot report: %v", err)
	}
	if list, _ := AgentReports(res.ID); len(list) != 1 {
		t.Fatal("reports must outlive the exit")
	}
	_ = Sessions().Remove(sess.Info().ID)
	if _, err := AgentReports(res.ID); err == nil {
		t.Fatal("a removed session has no reports")
	}
	r := registry()
	r.mu.Lock()
	r.prune()
	_, kept := r.reports[res.ID]
	r.mu.Unlock()
	if kept {
		t.Fatal("prune must drop the removed session's reports")
	}
}

func TestReportFirstLine(t *testing.T) {
	cases := map[string]string{
		"merged\r\nnext":            "merged",
		"\n\n  lead blank\nx":       "lead blank",
		"\x1b[31mred\x1b[0m tail\n": "red tail",
		strings.Repeat("é", 130):    strings.Repeat("é", 119) + "…",
		"":                          "",
	}
	for in, want := range cases {
		if got := ReportFirstLine(in); got != want {
			t.Errorf("ReportFirstLine(%q) = %q, want %q", in, got, want)
		}
	}
}
