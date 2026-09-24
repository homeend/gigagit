package model

import (
	"errors"
	"strings"
	"testing"
)

const fullSHA = "eb759989a1b2c3d4e5f60718293a4b5c6d7e8f90"

// sha256SHA is a sha-256 repository's commit id: 64 hex characters, which the
// grammar must accept in full (producers always write the FULL sha).
const sha256SHA = "eb759989a1b2c3d4e5f60718293a4b5c6d7e8f90eb759989a1b2c3d4e5f60718"

// TestParseLinkRoundTrip is the grammar table: every row of the spec's
// grammar parses to the stated value AND renders back to the same bytes.
func TestParseLinkRoundTrip(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want Link
	}{
		// NOTE: StateCommitted is FileState's zero value (iota), so
		// StateUnstaged must be written out in every want — omitting Target
		// asserts "committed with an empty sha", which is not what a
		// working-tree link is.
		{"repo only", "gg://gigagit", Link{
			Repo: LinkRepo{Name: "gigagit"}, Side: NoteSideNew,
			Target: LinkTarget{State: StateUnstaged},
		}},
		{"commit, no path", "gg://gigagit@" + fullSHA, Link{
			Repo: LinkRepo{Name: "gigagit"}, Side: NoteSideNew,
			Target: LinkTarget{State: StateCommitted, Commit: fullSHA},
		}},
		{"short sha is accepted verbatim", "gg://gigagit@eb75998", Link{
			Repo: LinkRepo{Name: "gigagit"}, Side: NoteSideNew,
			Target: LinkTarget{State: StateCommitted, Commit: "eb75998"},
		}},
		{"file, working tree", "gg://gigagit/internal/tui/steer.go", Link{
			Repo: LinkRepo{Name: "gigagit"}, Path: "internal/tui/steer.go",
			Side: NoteSideNew, Target: LinkTarget{State: StateUnstaged},
		}},
		{"file and line, working tree", "gg://gigagit/internal/tui/steer.go:42", Link{
			Repo: LinkRepo{Name: "gigagit"}, Path: "internal/tui/steer.go",
			Side: NoteSideNew, Line: 42, Target: LinkTarget{State: StateUnstaged},
		}},
		{"staged", "gg://gigagit/internal/tui/steer.go@staged", Link{
			Repo: LinkRepo{Name: "gigagit"}, Path: "internal/tui/steer.go",
			Side: NoteSideNew, Target: LinkTarget{State: StateStaged},
		}},
		{"staged with line", "gg://gigagit/internal/tui/steer.go@staged:42", Link{
			Repo: LinkRepo{Name: "gigagit"}, Path: "internal/tui/steer.go",
			Side: NoteSideNew, Line: 42, Target: LinkTarget{State: StateStaged},
		}},
		{"commit with line", "gg://gigagit/internal/tui/steer.go@" + fullSHA + ":42", Link{
			Repo: LinkRepo{Name: "gigagit"}, Path: "internal/tui/steer.go",
			Side: NoteSideNew, Line: 42,
			Target: LinkTarget{State: StateCommitted, Commit: fullSHA},
		}},
		{"commit with old-side line", "gg://gigagit/internal/tui/steer.go@" + fullSHA + ":old:38", Link{
			Repo: LinkRepo{Name: "gigagit"}, Path: "internal/tui/steer.go",
			Side: NoteSideOld, Line: 38,
			Target: LinkTarget{State: StateCommitted, Commit: fullSHA},
		}},
		{"commit with hunk", "gg://gigagit/internal/tui/steer.go@" + fullSHA + "#3", Link{
			Repo: LinkRepo{Name: "gigagit"}, Path: "internal/tui/steer.go",
			Side: NoteSideNew, Hunk: 3,
			Target: LinkTarget{State: StateCommitted, Commit: fullSHA},
		}},
		{"working-tree hunk", "gg://gigagit/internal/tui/steer.go#2", Link{
			Repo: LinkRepo{Name: "gigagit"}, Path: "internal/tui/steer.go",
			Side: NoteSideNew, Hunk: 2, Target: LinkTarget{State: StateUnstaged},
		}},
		// A sha-256 repository's commit id is 64 hex characters; the parser's
		// cap must match what all three producers emit (final review, A2).
		{"sha-256 commit, no path", "gg://gigagit@" + sha256SHA, Link{
			Repo: LinkRepo{Name: "gigagit"}, Side: NoteSideNew,
			Target: LinkTarget{State: StateCommitted, Commit: sha256SHA},
		}},
		{"sha-256 commit with line", "gg://gigagit/a/b.go@" + sha256SHA + ":42", Link{
			Repo: LinkRepo{Name: "gigagit"}, Path: "a/b.go",
			Side: NoteSideNew, Line: 42,
			Target: LinkTarget{State: StateCommitted, Commit: sha256SHA},
		}},
		{"nested path", "gg://gigagit/a/b/c/d.go:1", Link{
			Repo: LinkRepo{Name: "gigagit"}, Path: "a/b/c/d.go",
			Side: NoteSideNew, Line: 1, Target: LinkTarget{State: StateUnstaged},
		}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseLink(tc.in)
			if err != nil {
				t.Fatalf("ParseLink(%q) = %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("ParseLink(%q) =\n  %+v\nwant\n  %+v", tc.in, got, tc.want)
			}
			if s := got.String(); s != tc.in {
				t.Errorf("String() = %q, want %q", s, tc.in)
			}
		})
	}
}

// TestParseLinkLocalFormIsTextuallyIdempotent pins the ruling that a PARSED
// local link keeps the checkout and the file path undivided in Repo.Abs: the
// grammar has no delimiter between them, so only the resolver can split them.
// The contract these rows carry is String(Parse(s)) == s.
func TestParseLinkLocalFormIsTextuallyIdempotent(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		in     string
		abs    string
		line   int
		side   NoteSide
		commit string
	}{
		{"posix file and line", "gg:///mnt/t/others/test-1/src/PipelineConfig.kt:42",
			"/mnt/t/others/test-1/src/PipelineConfig.kt", 42, NoteSideNew, ""},
		{"posix with commit", "gg:///mnt/t/others/test-1/src/PipelineConfig.kt@" + fullSHA + ":42",
			"/mnt/t/others/test-1/src/PipelineConfig.kt", 42, NoteSideNew, fullSHA},
		{"posix checkout only", "gg:///mnt/t/others/test-1",
			"/mnt/t/others/test-1", 0, NoteSideNew, ""},
		// The Windows drive colon must never be read as the line separator.
		{"windows file and line", "gg:///C:/src/repo/f.go:42",
			"C:/src/repo/f.go", 42, NoteSideNew, ""},
		{"windows file, no line", "gg:///C:/src/repo/f.go",
			"C:/src/repo/f.go", 0, NoteSideNew, ""},
		{"windows with commit", "gg:///C:/src/repo/f.go@" + fullSHA + ":old:7",
			"C:/src/repo/f.go", 7, NoteSideOld, fullSHA},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseLink(tc.in)
			if err != nil {
				t.Fatalf("ParseLink(%q) = %v", tc.in, err)
			}
			if !got.IsLocal() || got.Repo.Abs != tc.abs || got.Repo.Name != "" {
				t.Errorf("repo = %+v, want local Abs %q", got.Repo, tc.abs)
			}
			if got.Path != "" {
				t.Errorf("Path = %q, want \"\" (a parsed local link keeps the path inside Abs)", got.Path)
			}
			if got.Line != tc.line || got.Side != tc.side {
				t.Errorf("line/side = %d/%s, want %d/%s", got.Line, got.Side, tc.line, tc.side)
			}
			if got.Target.Commit != tc.commit {
				t.Errorf("commit = %q, want %q", got.Target.Commit, tc.commit)
			}
			if s := got.String(); s != tc.in {
				t.Errorf("String() = %q, want %q", s, tc.in)
			}
		})
	}
}

// TestLocalProducerFormRendersAndReparses covers the OTHER local shape: a
// producer that knows both halves sets Abs (the checkout) and Path (the file).
// It renders to the same bytes a parse of that string keeps whole.
func TestLocalProducerFormRendersAndReparses(t *testing.T) {
	t.Parallel()
	built := Link{
		Repo: LinkRepo{Abs: "/mnt/t/others/test-1"},
		Path: "src/PipelineConfig.kt", Side: NoteSideNew, Line: 42,
		Target: LinkTarget{State: StateUnstaged},
	}
	const want = "gg:///mnt/t/others/test-1/src/PipelineConfig.kt:42"
	if s := built.String(); s != want {
		t.Fatalf("String() = %q, want %q", s, want)
	}
	back, err := ParseLink(want)
	if err != nil {
		t.Fatalf("ParseLink: %v", err)
	}
	if s := back.String(); s != want {
		t.Errorf("re-render = %q, want %q", s, want)
	}
	if back.Line != 42 {
		t.Errorf("Line = %d, want 42", back.Line)
	}
}

func TestParseLinkRefusals(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, in, wantMsg string
	}{
		{"no scheme", "/mnt/x/a.go", "starts with gg://"},
		{"scheme only", "gg://", "no repository"},
		{"line and hunk", "gg://gigagit/a.go@" + fullSHA + ":4#3", "line or a hunk"},
		{"bad target word", "gg://gigagit/a.go@zzzzzzz", "staged"},
		{"sha too short", "gg://gigagit/a.go@abc123", "staged"},
		// 65 hex: one past the sha-256 cap.
		{"sha too long", "gg://gigagit/a.go@" + sha256SHA + "f", "7 to 64 hex characters"},
		{"line without a path", "gg://gigagit@" + fullSHA + ":42", "needs a file path"},
		{"hunk without a path", "gg://gigagit@" + fullSHA + "#2", "needs a file path"},
		{"zero line", "gg://gigagit/a.go:0", "1-based"},
		{"negative line", "gg://gigagit/a.go:-4", "1-based"},
		{"zero hunk", "gg://gigagit/a.go#0", "positive"},
		{"non-numeric hunk", "gg://gigagit/a.go#x", "positive"},
		{"trailing slash path", "gg://gigagit/a/", "not a git path"},
		{"double slash path", "gg://gigagit/a//b.go", "not a git path"},
		// A path containing '@' cannot be expressed: the FIRST '@' after the
		// repo segment is the target separator, so what follows must be a
		// target — and "ird.go@abc1234" is not one. The link is refused
		// rather than silently meaning something else.
		{"at in path", "gg://gigagit/we@ird.go@" + fullSHA + ":5", "staged"},
		// B1: only the LAST ":<n>" is the line, so a remote-named path would
		// otherwise keep the earlier colon ("a.go:42") and address a file
		// nobody has. The remote-named branch validates its path.
		{"colon in a remote-named path", "gg://gigagit/a.go:42:99", "not a git path"},
		{"colon in a nested remote-named path", "gg://gigagit/a:b/c.go", "not a git path"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseLink(tc.in)
			if err == nil {
				t.Fatalf("ParseLink(%q) = nil error, want a refusal", tc.in)
			}
			if !errors.Is(err, ErrLink) {
				t.Errorf("error %v does not wrap ErrLink", err)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("error %q does not mention %q", err, tc.wantMsg)
			}
		})
	}
}

func TestLinkAddress(t *testing.T) {
	t.Parallel()
	l, err := ParseLink("gg://gigagit/a/b.go@" + fullSHA + ":9")
	if err != nil {
		t.Fatal(err)
	}
	want := FileAddress{State: StateCommitted, Commit: fullSHA, Path: "a/b.go"}
	if got := l.Address(); got != want {
		t.Errorf("Address() = %+v, want %+v", got, want)
	}
	staged, err := ParseLink("gg://gigagit/a/b.go@staged")
	if err != nil {
		t.Fatal(err)
	}
	if got := staged.Address(); got != (FileAddress{State: StateStaged, Path: "a/b.go"}) {
		t.Errorf("staged Address() = %+v", got)
	}
}

func TestLinkPathOK(t *testing.T) {
	t.Parallel()
	for _, p := range []string{"a/b.go", "internal/tui/steer.go", "x"} {
		if !LinkPathOK(p) {
			t.Errorf("LinkPathOK(%q) = false, want true", p)
		}
	}
	for _, p := range []string{"we@ird.go", "a:b.go", "c#d.go"} {
		if LinkPathOK(p) {
			t.Errorf("LinkPathOK(%q) = true, want false", p)
		}
	}
}

// A3: the checkout half of a local link has its own rule — '@' and '#' are
// fatal wherever they appear, a ':' is not (drive prefix, or a POSIX
// directory that simply contains one).
func TestLinkAbsOK(t *testing.T) {
	t.Parallel()
	ok := []string{
		"/mnt/t/others/test-1", "C:/src/repo", "/C:/src/repo",
		"/mnt/backup:1/repo", "/home/user/repo", "c:/src/repo",
	}
	for _, p := range ok {
		if !LinkAbsOK(p) {
			t.Errorf("LinkAbsOK(%q) = false, want true", p)
		}
	}
	bad := []string{
		"/home/user@corp/repo", "/mnt/backup#1/repo", "C:/src@work/repo",
		"/srv/repo@2", "/srv/#tmp/repo",
	}
	for _, p := range bad {
		if LinkAbsOK(p) {
			t.Errorf("LinkAbsOK(%q) = true, want false", p)
		}
	}
	// The rule earns its keep only if a refused path really would not survive
	// a round trip: a local link built from one is rejected by ParseLink.
	l := Link{Repo: LinkRepo{Abs: "/home/user@corp/repo"}, Path: "a.go",
		Side: NoteSideNew, Target: LinkTarget{State: StateUnstaged}}
	if _, err := ParseLink(l.String()); err == nil {
		t.Errorf("ParseLink(%q) = nil error; the producers' refusal would be pointless", l.String())
	}
}

func TestRepoNameFromURL(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"git@github.com:homeend/gigagit.git", "gigagit"},
		{"https://github.com/homeend/gigagit", "gigagit"},
		{"https://github.com/homeend/gigagit/", "gigagit"},
		{"https://github.com/homeend/gigagit.git", "gigagit"},
		{"ssh://git@host:2222/homeend/gigagit.git", "gigagit"},
		{"git@github.com:gigagit.git", "gigagit"},
		{"file:///srv/git/thing.git", "thing"},
		{"/srv/git/thing.git", "thing"},
		{`C:\src\thing`, "thing"},
		{"  https://example.com/a/b.git  ", "b"},
		{"", ""},
		{".git", ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			if got := RepoNameFromURL(tc.in); got != tc.want {
				t.Errorf("RepoNameFromURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseLinkPreviewFormsRoundTrip(t *testing.T) {
	t.Parallel()
	for _, s := range []string{
		"gg://gigagit@main...feat/login",
		"gg://gigagit/internal/a.go@main...feat/login",
		"gg://gigagit/internal/a.go@main...feat/login:42",
		"gg://gigagit/internal/a.go@main...feat/login#3",
		"gg://gigagit@origin/main...origin/feat/login",
		"gg:///mnt/t/repo/a.go@main...feat/x:7",
	} {
		l, err := ParseLink(s)
		if err != nil {
			t.Fatalf("ParseLink(%q) = %v", s, err)
		}
		if l.Target.Preview == nil {
			t.Fatalf("ParseLink(%q): Target.Preview is nil", s)
		}
		if l.Target.State != StateCommitted || l.Target.Commit != "" {
			t.Errorf("ParseLink(%q): Target = %+v, want StateCommitted with no sha", s, l.Target)
		}
		if got := l.String(); got != s {
			t.Errorf("String(Parse(%q)) = %q", s, got)
		}
	}
}

// A hand-built Link can carry Side == NoteSideOld on a preview target (no
// constructor stops it) even though a preview has no old side — String must
// still render something its own inverse, ParseLink, accepts, rather than an
// "old:" ParseLink refuses outright (see the "the old side is the merge
// base" refusal below).
func TestLinkStringForcesTheNewSideOnAPreviewTarget(t *testing.T) {
	t.Parallel()
	x := Link{
		Repo:   LinkRepo{Name: "gigagit"},
		Path:   "a.go",
		Target: LinkTarget{State: StateCommitted, Preview: &LinkPreview{Source: "feat/x", Target: "main"}},
		Line:   3,
		Side:   NoteSideOld,
	}
	s := x.String()
	if strings.Contains(s, "old:") {
		t.Fatalf("String(%+v) = %q, must not render an old side for a preview", x, s)
	}
	parsed, err := ParseLink(s)
	if err != nil {
		t.Fatalf("ParseLink(%q) = %v, want String's own output accepted", s, err)
	}
	if parsed.Side != NoteSideNew {
		t.Errorf("ParseLink(%q).Side = %v, want NoteSideNew", s, parsed.Side)
	}
	if got := parsed.String(); got != s {
		t.Errorf("String(Parse(String(x))) = %q, want %q", got, s)
	}
}

func TestParseLinkPreviewSplitsThePairInGitOrder(t *testing.T) {
	t.Parallel()
	l, err := ParseLink("gg://gigagit/a.go@origin/main...origin/feat/login:9")
	if err != nil {
		t.Fatal(err)
	}
	if l.Target.Preview.Target != "origin/main" || l.Target.Preview.Source != "origin/feat/login" {
		t.Errorf("Preview = %+v, want Target=origin/main Source=origin/feat/login", *l.Target.Preview)
	}
	if l.Path != "a.go" || l.Line != 9 || l.Side != NoteSideNew {
		t.Errorf("path/line/side = %q/%d/%s", l.Path, l.Line, l.Side)
	}
}

func TestParseLinkPreviewRefusals(t *testing.T) {
	t.Parallel()
	for _, s := range []string{
		"gg://gigagit/a.go@main...feat/x:old:3",                         // the old side is the merge base
		"gg://gigagit/a.go@abc1234...def5678",                           // a sha-looking pair
		"gg://gigagit/a.go@1234567890abcdef1234...abcdef1234567890abcd", // longer shas, same rule
		"gg://gigagit/a.go@...feat/x",                                   // no target half
		"gg://gigagit/a.go@main...",                                     // no source half
		"gg://gigagit/a.go@main...feat@x",                               // '@' is not expressible in a ref here
		"gg://gigagit/a.go@main...feat...x",                             // three dots twice
	} {
		if l, err := ParseLink(s); err == nil {
			t.Errorf("ParseLink(%q) = %+v, want a refusal", s, l)
		} else if !errors.Is(err, ErrLink) {
			t.Errorf("ParseLink(%q) error %v does not wrap ErrLink", s, err)
		}
	}
}

func TestLinkRefOK(t *testing.T) {
	t.Parallel()
	for _, ok := range []string{"main", "feat/login", "origin/main", "release-1.2"} {
		if !LinkRefOK(ok) {
			t.Errorf("LinkRefOK(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "a@b", "a:b", "a#b", "a b", "a...b"} {
		if LinkRefOK(bad) {
			t.Errorf("LinkRefOK(%q) = true, want false", bad)
		}
	}
}

// The hint names WHICH UI surface a link was copied from (spec §3.3). It is
// the last thing in the grammar, it never changes what the link ADDRESSES,
// and it round-trips.
func TestLinkHintRoundTrip(t *testing.T) {
	t.Parallel()
	for _, s := range []string{
		"gg://gigagit@abc1234def?bookmark=auth-fix",
		"gg://gigagit@abc1234def?shelf=commit-x-9f3a1",
		"gg://gigagit?shelf=wt-parser-9f3a1",
		"gg://gigagit/internal/a.go@abc1234def:42?bookmark=b1",
		"gg://gigagit/internal/a.go@abc1234def#3?shelf=s1",
		"gg://gigagit@9c1f2a3456?stash=0",
		// ?preview= names a SAVED entry of the Previews surface — a merge
		// preview or a commit pair — in the name form and the local form.
		"gg://gigagit@main...feat/x?preview=1a2b3c4d",
		"gg://gigagit@abc1234def..9c1f2a3456?preview=1a2b3c4d",
		"gg:///mnt/t/repo@main...feat/x?preview=1a2b3c4d",
	} {
		t.Run(s, func(t *testing.T) {
			t.Parallel()
			l, err := ParseLink(s)
			if err != nil {
				t.Fatalf("ParseLink(%q): %v", s, err)
			}
			if got := l.String(); got != s {
				t.Errorf("String(ParseLink(%q)) = %q", s, got)
			}
		})
	}
}

// The hint parses into its two halves, and the rest of the link is untouched
// by its presence: same address with and without.
func TestLinkHintSplitsAndDoesNotChangeTheAddress(t *testing.T) {
	t.Parallel()
	withHint, err := ParseLink("gg://gigagit/a.go@abc1234def:7?bookmark=auth-fix")
	if err != nil {
		t.Fatal(err)
	}
	if withHint.Hint.Kind != "bookmark" || withHint.Hint.ID != "auth-fix" {
		t.Fatalf("hint = %+v, want {bookmark auth-fix}", withHint.Hint)
	}
	plain, err := ParseLink("gg://gigagit/a.go@abc1234def:7")
	if err != nil {
		t.Fatal(err)
	}
	withHint.Hint = LinkHint{}
	if withHint != plain {
		t.Errorf("the hint changed the address:\n with = %+v\n without = %+v", withHint, plain)
	}
}

func TestLinkHintRejects(t *testing.T) {
	t.Parallel()
	for _, s := range []string{
		"gg://gigagit@abc1234def?",            // empty hint
		"gg://gigagit@abc1234def?bookmark",    // no "="
		"gg://gigagit@abc1234def?bookmark=",   // no id
		"gg://gigagit@abc1234def?=auth-fix",   // no kind
		"gg://gigagit@abc1234def?branch=main", // unknown kind
		"gg://gigagit@abc1234def?shelf=a?b",   // a second "?"
		"gg://gigagit/a?.go@abc1234def",       // "?" inside a path
	} {
		t.Run(s, func(t *testing.T) {
			t.Parallel()
			if _, err := ParseLink(s); err == nil {
				t.Fatalf("ParseLink(%q) should have failed", s)
			} else if !errors.Is(err, ErrLink) {
				t.Fatalf("error should wrap ErrLink, got %v", err)
			}
		})
	}
}

// The reject sets gain "?" so no producer can emit a link that reparses as a
// different place.
func TestQuestionMarkIsNotExpressible(t *testing.T) {
	t.Parallel()
	if LinkPathOK("a?.go") {
		t.Error("LinkPathOK must reject a path containing ?")
	}
	if LinkAbsOK("/home/u/re?po") {
		t.Error("LinkAbsOK must reject a checkout path containing ?")
	}
	if LinkRefOK("feat/a?b") {
		t.Error("LinkRefOK must reject a refname containing ?")
	}
	if LinkRefOK("main..feat") {
		t.Error("LinkRefOK must reject a refname containing .. (git forbids it, and it collides with the change-set form)")
	}
}

// @ref:<name> addresses a branch or tag TIP — unbounded, a point (spec §3.2).
func TestLinkRefRoundTrip(t *testing.T) {
	t.Parallel()
	for _, s := range []string{
		"gg://gigagit@ref:main",
		"gg://gigagit@ref:feat/unified-links",
		"gg://gigagit/internal/a.go@ref:main",
		"gg://gigagit/internal/a.go@ref:main:42",
		"gg://gigagit/internal/a.go@ref:main#3",
		"gg://gigagit@ref:main?bookmark=b1",
	} {
		t.Run(s, func(t *testing.T) {
			t.Parallel()
			l, err := ParseLink(s)
			if err != nil {
				t.Fatalf("ParseLink(%q): %v", s, err)
			}
			if l.Target.State != StateCommitted || l.Target.Ref == "" {
				t.Fatalf("target = %+v, want a committed ref target", l.Target)
			}
			if l.Target.Commit != "" || l.Target.Preview != nil || l.Target.Pair != nil {
				t.Fatalf("a ref target must set ONLY Ref, got %+v", l.Target)
			}
			if got := l.String(); got != s {
				t.Errorf("String(ParseLink(%q)) = %q", s, got)
			}
		})
	}
}

// @<a>..<b> is the two-dot CHANGE-SET — bounded, a pair (spec §3.2). Each half
// is a sha or a refname; mixing is allowed, because the common producer emits
// a resolved parent sha and a user may reasonably type a branch name.
func TestLinkPairRoundTrip(t *testing.T) {
	t.Parallel()
	for _, s := range []string{
		"gg://gigagit@abc1234def..abc9999fff",
		"gg://gigagit@main..feat/x",
		"gg://gigagit@abc1234def..feat/x",
		// Both halves short and hex: at the GRAMMAR layer these are refnames,
		// not shas, and domain resolves them. A length rule here would forbid
		// a branch literally named "abc".
		"gg://gigagit@abc..def",
		"gg://gigagit/internal/a.go@abc1234def..abc9999fff",
		"gg://gigagit@abc1234def..abc9999fff?stash=0",
	} {
		t.Run(s, func(t *testing.T) {
			t.Parallel()
			l, err := ParseLink(s)
			if err != nil {
				t.Fatalf("ParseLink(%q): %v", s, err)
			}
			if l.Target.Pair == nil {
				t.Fatalf("target = %+v, want a pair", l.Target)
			}
			if got := l.String(); got != s {
				t.Errorf("String(ParseLink(%q)) = %q", s, got)
			}
		})
	}
}

// Three dots keep their SHIPPED meaning (a merge preview of two BRANCH names)
// and must not be swallowed by the new two-dot form.
func TestThreeDotStillParsesAsAPreview(t *testing.T) {
	t.Parallel()
	l, err := ParseLink("gg://gigagit@main...feat/x")
	if err != nil {
		t.Fatal(err)
	}
	if l.Target.Preview == nil || l.Target.Pair != nil {
		t.Fatalf("target = %+v, want a preview and no pair", l.Target)
	}
}

func TestLinkRefAndPairRejects(t *testing.T) {
	t.Parallel()
	for name, s := range map[string]string{
		"empty ref":            "gg://gigagit@ref:",
		"ref with @":           "gg://gigagit@ref:fe@at",
		"pair missing a half":  "gg://gigagit@abc1234def..",
		"pair missing b half":  "gg://gigagit@..abc1234def",
		"pair with three dots": "gg://gigagit@a..b..c",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := ParseLink(s); err == nil {
				t.Fatalf("ParseLink(%q) should have failed", s)
			} else if !errors.Is(err, ErrLink) {
				t.Fatalf("error should wrap ErrLink, got %v", err)
			}
		})
	}
}

// An ALL-DIGIT branch name cannot ride a ref link: the ":<line>" suffix wins,
// by the shipped splitLinkLine rule, so "@ref:123" reads as target "ref" plus
// line 123 and then fails target validation. That is a deliberate refusal, not
// a wrong answer — the line suffix is far commoner than a numeric branch — and
// this test exists so nobody "fixes" it into ambiguity. (Plan 1b ruling R5.)
func TestAllDigitBranchNameIsRefused(t *testing.T) {
	t.Parallel()
	if _, err := ParseLink("gg://gigagit@ref:123"); err == nil {
		t.Fatal("an all-digit ref name must be refused, not silently reparsed")
	}
}

// TestParseLinkAcceptsABareWindowsDriveHead pins the LENIENT local spelling:
// `gg://C:/src/repo` with TWO slashes, which is what `"gg://" + ToSlash(dir)`
// produces on Windows and what a human retypes from memory. gg's own
// producers always emit the canonical three-slash form, so this is an accepted
// alias, not a second output shape — String() canonicalises it back.
//
// Before this, the head was read as a repository NAMED "C:" with the rest as
// its path: no error, exit 0, and a resolve that reported "C: is not in this
// machine's gg history". A silent wrong answer, and the reason the whole link
// family's tests failed on Windows while passing on POSIX.
func TestParseLinkAcceptsABareWindowsDriveHead(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, in, abs, canon string
		line                 int
	}{
		{"file and line", "gg://C:/src/repo/f.go:42", "C:/src/repo/f.go", "gg:///C:/src/repo/f.go:42", 42},
		{"checkout only", "gg://D:/src/repo", "D:/src/repo", "gg:///D:/src/repo", 0},
		{"lowercase drive", "gg://c:/src/repo/f.go", "c:/src/repo/f.go", "gg:///c:/src/repo/f.go", 0},
		{"drive root", "gg://C:/", "C:/", "gg:///C:/", 0},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseLink(tc.in)
			if err != nil {
				t.Fatalf("ParseLink(%q) = %v", tc.in, err)
			}
			if !got.IsLocal() || got.Repo.Abs != tc.abs || got.Repo.Name != "" {
				t.Fatalf("repo = %+v, want local Abs %q", got.Repo, tc.abs)
			}
			if got.Line != tc.line {
				t.Errorf("Line = %d, want %d", got.Line, tc.line)
			}
			if s := got.String(); s != tc.canon {
				t.Errorf("String() = %q, want the canonical %q", s, tc.canon)
			}
			// The canonical form must parse to the very same place.
			again, err := ParseLink(tc.canon)
			if err != nil {
				t.Fatalf("ParseLink(%q) = %v", tc.canon, err)
			}
			if again.Repo.Abs != got.Repo.Abs || again.Line != got.Line {
				t.Errorf("canonical re-parse = %+v/%d, want %+v/%d", again.Repo, again.Line, got.Repo, got.Line)
			}
		})
	}
}

// TestParseLinkRefusesAColonInARepositoryName is the other half of the drive
// fix: no remote repository name can hold a ':' (RepoNameFromURL cuts at the
// last '/', ':' or '\'), so a head that has one and is NOT a drive letter is
// a malformed link, not a repository nobody has opened. Refusing it here
// turns "gg link names an unknown repository" — a claim about the machine —
// into a claim about the link.
func TestParseLinkRefusesAColonInARepositoryName(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"gg://ab:/src/repo/f.go", // two letters: not a drive
		"gg://host:22/repo/f.go",
		"gg://:/repo/f.go",
	} {
		if l, err := ParseLink(in); err == nil {
			t.Errorf("ParseLink(%q) = %+v, want a refusal", in, l)
		} else if !errors.Is(err, ErrLink) {
			t.Errorf("ParseLink(%q) error %v does not wrap ErrLink", in, err)
		}
	}
}

func TestContentLinkRoundTrips(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"gg://gigagit/a/b.go?view=content",
		"gg://gigagit/a/b.go:12?view=content", // v2 shape: parsed now, landed later
		"gg:///mnt/t/repo/a/b.go?view=content",
	} {
		l, err := ParseLink(in)
		if err != nil {
			t.Fatalf("ParseLink(%q) = %v", in, err)
		}
		if !l.IsContent() || l.Hint != ContentHint {
			t.Errorf("ParseLink(%q).Hint = %+v, want the content hint", in, l.Hint)
		}
		if l.Target.State != StateUnstaged {
			t.Errorf("ParseLink(%q) target = %v, want the working tree", in, l.Target.State)
		}
		if got := l.String(); got != in {
			t.Errorf("String() = %q, want %q", got, in)
		}
	}
}

func TestContentLinkRefusals(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, in, want string }{
		{"commit target", "gg://gigagit/a.go@" + fullSHA + "?view=content", "working tree"},
		{"staged target", "gg://gigagit/a.go@staged?view=content", "working tree"},
		{"ref target", "gg://gigagit/a.go@ref:main?view=content", "working tree"},
		{"pair target", "gg://gigagit/a.go@main..dev?view=content", "working tree"},
		{"preview target", "gg://gigagit/a.go@main...dev?view=content", "working tree"},
		{"old side", "gg://gigagit/a.go:old:3?view=content", "old"},
		{"hunk", "gg://gigagit/a.go#2?view=content", "hunk"},
		{"no path", "gg://gigagit?view=content", "file path"},
		{"other id", "gg://gigagit/a.go?view=blame", "content"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseLink(tc.in)
			if err == nil || !errors.Is(err, ErrLink) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ParseLink(%q) = %v, want an ErrLink mentioning %q", tc.in, err, tc.want)
			}
		})
	}
}
