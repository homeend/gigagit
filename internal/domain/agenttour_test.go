package domain

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func tourWorker(t *testing.T, brief string) (ov, worker string, sess *AgentSession) {
	t.Helper()
	_, wt, _, ov := spawnFixture(t, 4)
	res, s, err := SpawnAgent(context.Background(), SpawnSpec{Req: AgentStartRequest{Caller: ov, Worktree: wt, Tool: "Sleeper", Prompt: brief}, Cols: 80, Rows: 24, MCPURL: "http://x", Approved: approveAll})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(UseSessionStates(NewStaticStates(nil)))
	return ov, res.ID, s
}

func TestAgentTourBriefAndReport(t *testing.T) {
	ov, w, s := tourWorker(t, "# Fix the parser\nStart at [parse](internal/x/parse.go:40-60).")
	if b, r := AgentTourKinds(w); !b || r {
		t.Fatalf("kinds before a report: brief %v report %v", b, r)
	}
	if b, _ := AgentTourKinds(ov); b {
		t.Fatal("a manual session has no brief")
	}
	d, err := AgentTour(w, "brief")
	info := s.Info()
	wantTitle := "Brief — " + info.Label + " · " + filepath.Base(info.Dir) + " (" + info.Started.Local().Format("15:04") + ")"
	if err != nil || d.Key != "brief:"+w || d.Dir != info.Dir || d.Root != CheckoutKey(info.Dir) || d.Title != wantTitle || !strings.Contains(d.Text, "parse.go:40-60") {
		t.Fatalf("brief tour = %+v %v (want title %q)", d, err, wantTitle)
	}
	if _, err := AgentTour(w, "report"); err == nil || !strings.Contains(err.Error(), "has not reported") {
		t.Fatalf("report before any: %v", err)
	}
	rep, _ := AgentReportVerb(w, "done: [check](src/a.go:10-30)", false)
	d, err = AgentTour(w, "report")
	if err != nil || d.Key != "report:"+w || !strings.HasPrefix(d.Title, "Report — ") || d.Seq != rep.Seq || d.Text != rep.Text {
		t.Fatalf("report tour = %+v %v", d, err)
	}
	AgentReportVerb(w, "all done", true)
	if d, _ = AgentTour(w, "report"); !strings.HasPrefix(d.Title, "Final report — ") || d.Text != "all done" {
		t.Fatalf("final report tour = %+v", d)
	}
	if _, r := AgentTourKinds(w); !r {
		t.Fatal("kinds after a report")
	}
	if _, err := AgentTour(w, "notes"); err == nil {
		t.Fatal("unknown kind")
	}
	if _, err := AgentTour(ov, "brief"); err == nil || !strings.Contains(err.Error(), "no brief") {
		t.Fatalf("manual session brief: %v", err)
	}
}

func TestAgentTourCutsALongBrief(t *testing.T) {
	line := strings.Repeat("é", 50) + "\n"               // 101 bytes, multibyte
	_, w, _ := tourWorker(t, strings.Repeat(line, 2500)) // ~247 KiB, under MaxBriefBytes
	d, err := AgentTour(w, "brief")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Text) > TourMaxBytes || !utf8.ValidString(d.Text) || !strings.HasSuffix(d.Text, tourCutLine) {
		t.Fatalf("cut: %d bytes, valid %v, tail %q", len(d.Text), utf8.ValidString(d.Text), d.Text[len(d.Text)-80:])
	}
	if body := strings.TrimSuffix(d.Text, tourCutLine); !strings.HasSuffix(body, "\n") {
		t.Fatal("the cut must fall on a line end")
	}
}
