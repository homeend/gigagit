package model

import (
	"strings"
	"testing"
)

const (
	boundSha    = "0123456789abcdef0123456789abcdef01234567"
	boundParent = "89abcdef0123456789abcdef0123456789abcdef"
)

func TestBoundKind(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		text string
		want LinkBoundKind
	}{
		{"gg://r@ref:feat/x", LinkBoundRef},
		{"gg:///home/u/repo@ref:feat/x", LinkBoundRef}, // the LOCAL-form row
		{"gg://r@" + boundSha, LinkBoundCommit},
		{"gg://r@0123456", LinkBoundCommit}, // a typed, abbreviated sha is still a point
		{"gg://r/f.go@ref:feat/x", LinkBoundNone},
		{"gg://r/f.go@" + boundSha, LinkBoundNone},
		{"gg://r@" + boundParent + ".." + boundSha, LinkBoundNone},
		{"gg://r@main...feat/x", LinkBoundNone},
		{"gg://r", LinkBoundNone},
		{"gg://r@staged", LinkBoundNone},
		{"gg://r?shelf=abc", LinkBoundNone},
	} {
		l, err := ParseLink(c.text)
		if err != nil {
			t.Fatalf("%s: %v", c.text, err)
		}
		if got := l.BoundKind(); got != c.want {
			t.Errorf("%s: BoundKind = %v, want %v", c.text, got, c.want)
		}
	}
}

func TestWithBase(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, text, base, self string
		wantSuffix             string // "" = refused
	}{
		{"ref, target first", "gg://r@ref:feat/x", "main", "", "@main...feat/x"},
		{"ref, remote-tracking base", "gg://r@ref:feat/x", "origin/main", "", "@origin/main...feat/x"},
		{"ref, local form", "gg:///home/u/repo@ref:feat/x", "main", "", "gg:///home/u/repo@main...feat/x"},
		{"ref bounded by itself", "gg://r@ref:main", "main", "", ""},
		{"ref, base the grammar cannot hold", "gg://r@ref:feat/x", "bad name", "", ""},
		{"ref, empty base", "gg://r@ref:feat/x", "", "", ""},
		{"commit", "gg://r@" + boundSha, boundParent, boundSha, "@" + boundParent + ".." + boundSha},
		{"commit typed short takes the FULL self", "gg://r@0123456", boundParent, boundSha, "@" + boundParent + ".." + boundSha},
		{"commit, short base", "gg://r@" + boundSha, boundParent[:7], boundSha, ""},
		{"commit, short self", "gg://r@0123456", boundParent, "0123456", ""},
		{"a hint survives", "gg://r@" + boundSha + "?bookmark=b1", boundParent, boundSha, "@" + boundParent + ".." + boundSha + "?bookmark=b1"},
		{"already bounded", "gg://r@main...feat/x", "main", "", ""},
		{"a file link", "gg://r/f.go@ref:feat/x", "main", "", ""},
	} {
		l, err := ParseLink(c.text)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		got, ok := l.WithBase(c.base, c.self)
		if c.wantSuffix == "" {
			if ok {
				t.Errorf("%s: WithBase = %q, want a refusal", c.name, got.String())
			}
			continue
		}
		if !ok || !strings.HasSuffix(got.String(), c.wantSuffix) {
			t.Errorf("%s: WithBase = %q ok=%v, want suffix %q", c.name, got.String(), ok, c.wantSuffix)
			continue
		}
		// What it emits reparses, to the same thing, and is no longer a point.
		back, err := ParseLink(got.String())
		if err != nil || back.String() != got.String() {
			t.Errorf("%s: %q does not round-trip (%v)", c.name, got.String(), err)
		}
		if back.BoundKind() != LinkBoundNone {
			t.Errorf("%s: %q is still unbounded", c.name, got.String())
		}
	}
}
