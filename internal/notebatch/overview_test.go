package notebatch

import (
	"errors"
	"strings"
	"testing"
)

func TestParseOverviewAccepts(t *testing.T) {
	t.Parallel()
	d, err := ParseOverview([]byte("```json\n" + `{"version":1,"title":" How locks work ","overview":"[gate](a.go:3)",
"files":[{"path":"a.go","annotations":[{"newRange":[3,4],"summary":"the gate","rationale":"why"}]}]}` + "\n```"))
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "How locks work" || d.Overview != "[gate](a.go:3)" || len(d.Files) != 1 || d.Files[0].Notes[0].Range != [2]int{3, 4} {
		t.Fatalf("doc = %+v", d)
	}
	back, err := ParseOverview(d.Canonical())
	if err != nil || back.Title != d.Title || back.Overview != d.Overview || len(back.Files) != 1 {
		t.Fatalf("canonical round trip = %+v %v", back, err)
	}
	if strings.Contains(string(d.Canonical()), `"summary": ""`) {
		t.Fatalf("canonical carries an empty summary: %s", d.Canonical())
	}
}

func TestParseOverviewTitledFallback(t *testing.T) {
	t.Parallel()
	d, err := ParseOverviewTitled([]byte(`{"version":1,"overview":"x"}`), " From flag ")
	if err != nil || d.Title != "From flag" {
		t.Fatalf("doc = %+v %v", d, err)
	}
	d, err = ParseOverviewTitled([]byte(`{"version":1,"title":"Own","overview":"x"}`), "From flag")
	if err != nil || d.Title != "Own" {
		t.Fatalf("the document's own title must win: %+v %v", d, err)
	}
}

func TestParseOverviewRefuses(t *testing.T) {
	t.Parallel()
	for name, doc := range map[string]string{
		"no version":    `{"title":"t","overview":"x"}`,
		"no title":      `{"version":1,"overview":"x"}`,
		"no overview":   `{"version":1,"title":"t"}`,
		"a summary":     `{"version":1,"title":"t","overview":"x","summary":"s"}`,
		"meta":          `{"version":1,"title":"t","overview":"x","meta":{"verdict":"approve"}}`,
		"file summary":  `{"version":1,"title":"t","overview":"x","files":[{"path":"a","summary":"s"}]}`,
		"old range":     `{"version":1,"title":"t","overview":"x","files":[{"path":"a","annotations":[{"oldRange":[1,1],"summary":"s"}]}]}`,
		"empty remark":  `{"version":1,"title":"t","overview":"x","files":[{"path":"a","annotations":[{"newRange":[1,1],"summary":" "}]}]}`,
		"long title":    `{"version":1,"title":"` + strings.Repeat("t", 201) + `","overview":"x"}`,
		"huge overview": `{"version":1,"title":"t","overview":"` + strings.Repeat("x", MaxOverviewBytes+1) + `"}`,
		"not json":      `the overview is in gg`,
	} {
		if _, err := ParseOverview([]byte(doc)); !errors.Is(err, ErrNotOverviewDoc) {
			t.Errorf("%s: err = %v, want ErrNotOverviewDoc", name, err)
		}
	}
}
