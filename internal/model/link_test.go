package model

import (
	"errors"
	"strings"
	"testing"
)

const fullSHA = "eb759989a1b2c3d4e5f60718293a4b5c6d7e8f90"

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
		{"sha too long", "gg://gigagit/a.go@" + fullSHA + "ff", "staged"},
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
