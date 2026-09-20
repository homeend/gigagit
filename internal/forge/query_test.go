package forge

import (
	"context"
	"slices"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
)

func TestPRQueryNormalize(t *testing.T) {
	t.Parallel()
	got, err := PRQuery{}.Normalize()
	if err != nil || got != (PRQuery{State: StateAll, Limit: DefaultSearchLimit}) {
		t.Fatalf("zero query = %+v, %v", got, err)
	}
	got, err = PRQuery{State: StateMerged, Text: "  fix login \n", Limit: MaxSearchLimit}.Normalize()
	if err != nil || got != (PRQuery{State: StateMerged, Text: "fix login", Limit: MaxSearchLimit}) {
		t.Fatalf("full query = %+v, %v", got, err)
	}
	for _, bad := range []PRQuery{{State: "MERGED"}, {State: "draft"}, {Limit: -1}, {Limit: MaxSearchLimit + 1}} {
		if _, err := bad.Normalize(); err == nil {
			t.Errorf("%+v: want an error", bad)
		}
	}
}

// searchArgv runs one Search against a FakeRunner answering rows PR rows and
// returns the argv gh was given.
func searchArgv(t *testing.T, q PRQuery, out string) ([]string, int, bool) {
	t.Helper()
	f := gitexec.NewFakeRunner()
	var argv []string
	f.SetHandler("gh pr search", func(_ context.Context, a []string) (gitexec.Result, error) {
		argv = a
		return gitexec.Result{Stdout: out}, nil
	})
	prs, more, err := NewGHWithRunner(f).Search(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	return argv, len(prs), more
}

func valueAfter(argv []string, flag string) string {
	if i := slices.Index(argv, flag); i >= 0 && i+1 < len(argv) {
		return argv[i+1]
	}
	return "<missing " + flag + ">"
}

func TestGHSearchArgv(t *testing.T) {
	t.Parallel()
	argv, _, _ := searchArgv(t, PRQuery{State: StateClosed, Text: "fix login", Limit: 50}, "[]")
	if argv[0] != "pr" || argv[1] != "list" {
		t.Fatalf("argv = %v", argv)
	}
	if got := valueAfter(argv, "--state"); got != "closed" {
		t.Errorf("--state = %q", got)
	}
	// One row more than asked for: that row is how "more" is known.
	if got := valueAfter(argv, "--limit"); got != "51" {
		t.Errorf("--limit = %q, want 51", got)
	}
	// gh orders a listing by creation and a search by best match; the sort
	// qualifier is what makes a cut at N the N newest-updated.
	if got := valueAfter(argv, "--search"); got != "fix login sort:updated-desc" {
		t.Errorf("--search = %q", got)
	}
	if got := valueAfter(argv, "--json"); got != prFields {
		t.Errorf("--json = %q", got)
	}
	for _, bad := range []string{"-X", "--method", "comment", "review", "edit", "merge", "close", "reopen", "create"} {
		if slices.Contains(argv, bad) {
			t.Errorf("argv %v contains mutating token %q", argv, bad)
		}
	}

	argv, _, _ = searchArgv(t, PRQuery{State: StateAll, Limit: 5}, "[]")
	if got := valueAfter(argv, "--search"); got != "sort:updated-desc" {
		t.Errorf("empty text: --search = %q", got)
	}
	// The user's own sort wins, and a text that starts with a dash stays the
	// VALUE of --search.
	argv, _, _ = searchArgv(t, PRQuery{State: StateAll, Text: "-label:bug sort:created-asc", Limit: 5}, "[]")
	if got := valueAfter(argv, "--search"); got != "-label:bug sort:created-asc" {
		t.Errorf("own sort: --search = %q", got)
	}
}

func TestGHSearchMore(t *testing.T) {
	t.Parallel()
	list := string(fixture(t, "pr-search.json")) // three rows
	if _, n, more := searchArgv(t, PRQuery{State: StateAll, Limit: 2}, list); n != 2 || !more {
		t.Errorf("limit 2 of 3: rows=%d more=%v", n, more)
	}
	if _, n, more := searchArgv(t, PRQuery{State: StateAll, Limit: 3}, list); n != 3 || more {
		t.Errorf("limit 3 of 3: rows=%d more=%v", n, more)
	}
}
