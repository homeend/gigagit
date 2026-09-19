package domain

import (
	"context"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// TestLinkDescForms pins LinkDesc's table (spec §4.3), plus the fallback
// kind ("link") a caller passes for a shape the table has no row for.
func TestLinkDescForms(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, kind, id, subject, want string
	}{
		{"branch", "branch", "feat/x", "", "branch: feat/x"},
		{"bookmark", "bookmark", "auth fix", "", "bookmark: auth fix"},
		{"shelf", "shelf", "WIP parser", "", "shelf: WIP parser"},
		{"preview", "preview", "main...feat/x", "", "preview: main...feat/x"},
		{"commit", "commit", "abc123", "fix auth", "commit: abc123 fix auth"},
		{"stash", "stash", "", "WIP on main", "stash: WIP on main"},
		{"file", "file", "src/main.go", "", "file: src/main.go"},
		{"fallback", "link", "gg://gigagit@1234567..89abcde", "", "link: gg://gigagit@1234567..89abcde"},
	}
	for _, c := range cases {
		if got := LinkDesc(c.kind, c.id, c.subject); got != c.want {
			t.Errorf("%s: LinkDesc(%q,%q,%q) = %q, want %q", c.name, c.kind, c.id, c.subject, got, c.want)
		}
	}
}

// TestLinkDescTruncatesLongFreeText pins DescMax=60: a commit subject or a
// bookmark/shelf label is otherwise unbounded, and a stored Desc must stay
// one line.
func TestLinkDescTruncatesLongFreeText(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 100)
	if got := LinkDesc("commit", "abc123", long); got != "commit: abc123 "+strings.Repeat("x", 60) {
		t.Errorf("commit subject not truncated to 60 runes: %q", got)
	}
	if got := LinkDesc("bookmark", long, ""); got != "bookmark: "+strings.Repeat("x", 60) {
		t.Errorf("bookmark label not truncated to 60 runes: %q", got)
	}
}

// TestDescribeLinkTable walks the FIELD derivation (the half LinkDesc's own
// table cannot see): hint first, then target, then path, then the fallback.
func TestDescribeLinkTable(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	writeIn(t, dir, "a.txt", "a\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-qm", "seed subject")
	sha := headHash(t, dir)
	cases := []struct{ name, link, want string }{
		{"branch", "gg://r@ref:feat/x", "branch: feat/x"},
		{"preview", "gg://r@main...feat/x", "preview: main...feat/x"},
		{"file", "gg://r/a/b.go", "file: a/b.go"},
		{"commit", "gg://r@" + sha, "commit: " + sha[:7] + " seed subject"},
		// The fallback's free text is the link itself, cut at DescMax like any other.
		{"pair falls back", "gg://r@" + sha + ".." + sha, "link: " + ("gg://r@" + sha + ".." + sha)[:DescMax]},
		{"hint wins over target", "gg://r@" + sha + "?bookmark=nosuch", "bookmark: nosuch"},
	}
	for _, c := range cases {
		l, err := model.ParseLink(c.link)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := svc.DescribeLink(context.Background(), l); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestRecordCopiedLinkRecordsLinksOnly(t *testing.T) {
	t.Parallel()
	_, svc := newRealRepo(t)
	svc.UseLinkHistDir(t.TempDir())
	ctx := context.Background()
	svc.RecordCopiedLink(ctx, "not a link")
	svc.RecordCopiedLink(ctx, "gg://r@ref:main")
	h := svc.LinkHistory(ctx)
	if len(h) != 1 || h[0].Link != "gg://r@ref:main" || h[0].Desc != "branch: main" {
		t.Fatalf("history = %+v", h)
	}
}

// The defect UseSavedCompareDir shipped with in plan 3a, pre-empted: setting
// the root must re-arm a store that was already resolved on the OLD one.
func TestUseLinkHistDirReArmsAResolvedStore(t *testing.T) {
	t.Parallel()
	_, svc := newRealRepo(t)
	ctx := context.Background()
	svc.UseLinkHistDir(t.TempDir())
	svc.RecordCopiedLink(ctx, "gg://r@ref:one")
	if h := svc.LinkHistory(ctx); len(h) != 1 {
		t.Fatalf("fixture: the first dir did not record: %+v", h)
	}
	svc.UseLinkHistDir(t.TempDir())
	if h := svc.LinkHistory(ctx); len(h) != 0 {
		t.Fatalf("store still on the old dir: %+v", h)
	}
}
