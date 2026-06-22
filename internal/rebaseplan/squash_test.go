package rebaseplan

import (
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func squashActions(p Plan) map[string]Action {
	out := map[string]Action{}
	for _, e := range p.Entries {
		out[e.Sha] = e.Action
	}
	return out
}

func TestBuildSquashAdjacentPair(t *testing.T) {
	commits := []model.RangeCommit{rc("a", "A"), rc("b", "B"), rc("c", "C")}
	p, err := BuildSquash(commits, []string{"a", "b"})
	if err != nil {
		t.Fatalf("BuildSquash: %v", err)
	}
	if len(p.Entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(p.Entries))
	}
	got := squashActions(p)
	if got["a"] != Pick || got["b"] != Squash || got["c"] != Pick {
		t.Fatalf("actions = %v, want a:pick b:squash c:pick", got)
	}
	if p.Entries[0].Orig != "A\n" {
		t.Fatalf("Orig[0] = %q, want \"A\\n\"", p.Entries[0].Orig)
	}
}

func TestBuildSquashAdjacentTriple(t *testing.T) {
	commits := []model.RangeCommit{rc("a", "A"), rc("b", "B"), rc("c", "C"), rc("d", "D")}
	p, err := BuildSquash(commits, []string{"b", "c", "d"})
	if err != nil {
		t.Fatalf("BuildSquash: %v", err)
	}
	got := squashActions(p)
	if got["a"] != Pick || got["b"] != Pick || got["c"] != Squash || got["d"] != Squash {
		t.Fatalf("actions = %v, want a:pick b:pick c:squash d:squash", got)
	}
}

func TestBuildSquashNonAdjacent(t *testing.T) {
	commits := []model.RangeCommit{rc("a", "A"), rc("b", "B"), rc("c", "C")}
	_, err := BuildSquash(commits, []string{"a", "c"})
	if err == nil {
		t.Fatal("want error for non-adjacent targets")
	}
}

func TestBuildSquashMissingTarget(t *testing.T) {
	commits := []model.RangeCommit{rc("a", "A"), rc("b", "B")}
	_, err := BuildSquash(commits, []string{"a", "z"})
	if err == nil {
		t.Fatal("want error for target not in range")
	}
}

func TestBuildSquashTooFew(t *testing.T) {
	commits := []model.RangeCommit{rc("a", "A"), rc("b", "B")}
	_, err := BuildSquash(commits, []string{"a"})
	if err == nil {
		t.Fatal("want error for fewer than 2 targets")
	}
}

func TestBuildSquashMessageConcatenates(t *testing.T) {
	commits := []model.RangeCommit{rc("a", "A subject"), rc("b", "B subject"), rc("c", "C subject")}
	p, err := BuildSquash(commits, []string{"a", "b"})
	if err != nil {
		t.Fatalf("BuildSquash: %v", err)
	}
	msg := p.Message(0)
	if msg != "A subject\n\nB subject" {
		t.Fatalf("Message = %q, want concatenation of A and B", msg)
	}
}
