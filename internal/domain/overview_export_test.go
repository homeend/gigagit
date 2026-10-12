package domain

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/overviewhtml"
)

var exportBlobRE = regexp.MustCompile(`(?s)<script type="application/json" id="gg-data">(.*?)</script>`)

func exportBlob(t *testing.T, page []byte) overviewhtml.Export {
	t.Helper()
	m := exportBlobRE.FindSubmatch(page)
	if m == nil {
		t.Fatal("no data blob")
	}
	var e overviewhtml.Export
	if err := json.Unmarshal(m[1], &e); err != nil {
		t.Fatal(err)
	}
	return e
}

func TestExportStoredOverview(t *testing.T) {
	t.Parallel()
	svc, _, dir := sendRepo(t)
	rid := savePRReview(t, svc, overviewDoc)
	page, name, err := svc.ExportOverviewHTML(context.Background(), OverviewSource{ReviewID: rid})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(name, ".html") || !strings.HasPrefix(name, "review-overview-") {
		t.Errorf("name = %q", name)
	}
	e := exportBlob(t, page)
	if e.Kind != "review" || len(e.Files) != 1 || e.Files[0].Path != "big.go" {
		t.Fatalf("files = %+v", e.Files)
	}
	f := e.Files[0]
	if !f.Changed || len(f.Diff) == 0 || len(f.Lines) < 25 || f.Placeholder != "" {
		t.Errorf("big.go: changed=%v diff=%d lines=%d ph=%q", f.Changed, len(f.Diff), len(f.Lines), f.Placeholder)
	}
	missing := map[string]bool{}
	for _, a := range e.Anchors {
		missing[a.Dest] = a.Missing
	}
	for dest, want := range map[string]bool{"big.go:5": false, "big.go:24-25": false, "big.go": false, "other.go:1": true, "big.go:900": true, "note:t3": true} {
		if got, ok := missing[dest]; !ok || got != want {
			t.Errorf("%s missing = %v (listed %v), want %v", dest, got, ok, want)
		}
	}
	if e.Left == "" || e.Right == "" {
		t.Errorf("range labels empty: %q %q", e.Left, e.Right)
	}
	s := string(page)
	if strings.Contains(s, "check this") { // the review's line note (R3)
		t.Error("a review note leaked into the export")
	}
	if strings.Contains(s, dir) {
		t.Error("the worktree path leaked into the export")
	}
}

func TestExportStoredOverviewAbsent(t *testing.T) {
	t.Parallel()
	svc, _, _ := sendRepo(t)
	rid := savePRReview(t, svc, `{"version":1,"summary":"no overview","files":[{"path":"big.go","annotations":[{"newRange":[5,5],"summary":"x"}]}]}`)
	if _, _, err := svc.ExportOverviewHTML(context.Background(), OverviewSource{ReviewID: rid}); !errors.Is(err, ErrNoOverview) {
		t.Fatalf("err = %v, want ErrNoOverview", err)
	}
}
