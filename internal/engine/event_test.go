package engine

import "testing"

func TestEventsImplementInterface(t *testing.T) {
	events := []Event{
		Progress{Step: "fetching", Detail: "origin"},
		GitLine{Raw: "remote: counting"},
		DecisionNeeded{Request: DecisionRequest{ID: "x"}},
		Done{Result: Result{Summary: "ok", Changed: true}},
	}
	var got []string
	for _, e := range events {
		switch ev := e.(type) {
		case Progress:
			got = append(got, "progress:"+ev.Step)
		case GitLine:
			got = append(got, "line:"+ev.Raw)
		case DecisionNeeded:
			got = append(got, "decision:"+ev.Request.ID)
		case Done:
			got = append(got, "done:"+ev.Result.Summary)
		default:
			t.Fatalf("unknown event type %T", e)
		}
	}
	want := []string{"progress:fetching", "line:remote: counting", "decision:x", "done:ok"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d]=%q want %q", i, got[i], want[i])
		}
	}
}
