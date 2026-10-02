package template

import (
	"math/rand/v2"
	"reflect"
	"testing"
	"time"
)

func textCtx() Ctx {
	return Ctx{
		ParentBranch: "main", Repo: "gigagit", Branch: "feat/x",
		Seqs: map[string]int{"n": 7},
		Now:  func() time.Time { return time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC) },
		Rand: rand.New(rand.NewPCG(1, 2)),
	}
}

func TestResolveTextKnownTokens(t *testing.T) {
	t.Parallel()
	got, err := ResolveText("## <user:title>\nBranch: <branch> · <date> · #<seq:n:3> · <repo>",
		map[string]string{"title": "Hello"}, textCtx())
	if err != nil {
		t.Fatal(err)
	}
	want := "## Hello\nBranch: feat/x · 2026-10-02 · #007 · gigagit"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestResolveTextUnknownTokenStaysLiteral(t *testing.T) {
	t.Parallel()
	in := "line<br>mail <me@example.com> and <div class=\"x\">"
	got, err := ResolveText(in, nil, textCtx())
	if err != nil || got != in {
		t.Fatalf("got %q, %v; want the input unchanged", got, err)
	}
}

// A bare '<' must not swallow the token that follows it.
func TestResolveTextBareAngleBeforeToken(t *testing.T) {
	t.Parallel()
	got, err := ResolveText("if a < b then <user:name>\nx > y", map[string]string{"name": "N"}, textCtx())
	if err != nil || got != "if a < b then N\nx > y" {
		t.Fatalf("got %q, %v", got, err)
	}
}

// A filled value is inserted verbatim, never re-resolved.
func TestResolveTextValueNotReResolved(t *testing.T) {
	t.Parallel()
	got, err := ResolveText("<user:v>", map[string]string{"v": "<seq:n> <user:v>"}, textCtx())
	if err != nil || got != "<seq:n> <user:v>" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestResolveTextKnownTokenErrors(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"<seq>", "<user>", "<seq:n:x>", "<user:missing>"} {
		if _, err := ResolveText(in, nil, textCtx()); err == nil {
			t.Errorf("%q: want an error", in)
		}
	}
}

func TestResolveTextUnsetBranchIsEmpty(t *testing.T) {
	t.Parallel()
	c := textCtx()
	c.Branch = ""
	got, err := ResolveText("[<branch>]", nil, c)
	if err != nil || got != "[]" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestTextTokens(t *testing.T) {
	t.Parallel()
	got := TextTokens("<user:a> <br> <date> <user:b> <user:a> <seq:n:2> <branch> <date> a < b <user:c>")
	want := TextTokenSet{
		UserLabels: []string{"a", "b", "c"},
		SeqNames:   []string{"n"},
		Automatic:  []string{"<date>", "<seq:n:2>", "<branch>"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// <branch> takes no argument and <user:> needs a label: both are refused
// whatever the context holds, so validation (a dry resolve) catches them.
func TestResolveTextRefusesBranchArgAndEmptyUserLabel(t *testing.T) {
	ctx := textCtx()
	for _, tmpl := range []string{"on <branch:short>", "on <branch:>", "hi <user:>"} {
		if out, err := ResolveText(tmpl, map[string]string{"": "x"}, ctx); err == nil {
			t.Errorf("%q resolved to %q, want an error", tmpl, out)
		}
	}
	ctx.Branch = ""
	if _, err := ResolveText("on <branch:short>", nil, ctx); err == nil {
		t.Error("<branch:short> on a detached HEAD: no error")
	}
}
