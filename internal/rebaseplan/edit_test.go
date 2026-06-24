package rebaseplan

import (
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func rc(h, s string) model.RangeCommit {
	return model.RangeCommit{Hash: h, Subject: s, Message: s + "\n"}
}

func TestOntoFor(t *testing.T) {
	if OntoFor("abc", EditDrop) != "abc~1" || OntoFor("abc", EditMoveUp) != "abc~1" {
		t.Fatal("drop/up should base onto ~1")
	}
	if OntoFor("abc", EditMoveDown) != "abc~2" {
		t.Fatal("down should base onto ~2")
	}
}

func planShas(p Plan) []string {
	var out []string
	for _, e := range p.Entries {
		s := e.Sha
		if e.Action == Drop {
			s += "(drop)"
		}
		out = append(out, s)
	}
	return out
}

func eqStrs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestBuildSingleEdit(t *testing.T) {
	commits := []model.RangeCommit{rc("a", "a"), rc("b", "b"), rc("c", "c"), rc("d", "d")}

	drop, err := BuildSingleEdit(commits, "b", EditDrop)
	if err != nil {
		t.Fatal(err)
	}
	if got := planShas(drop); !eqStrs(got, []string{"a", "b(drop)", "c", "d"}) {
		t.Fatalf("drop = %v", got)
	}
	for _, e := range drop.Entries {
		if e.Orig == "" {
			t.Fatalf("entry %s missing Orig", e.Sha)
		}
		if e.Sha != "b" && e.Action != Pick {
			t.Fatalf("entry %s should be Pick, got %s", e.Sha, e.Action)
		}
	}

	up, err := BuildSingleEdit(commits, "b", EditMoveUp) // swap b,c
	if err != nil {
		t.Fatal(err)
	}
	if got := planShas(up); !eqStrs(got, []string{"a", "c", "b", "d"}) {
		t.Fatalf("moveUp = %v", got)
	}

	down, err := BuildSingleEdit(commits, "c", EditMoveDown) // swap b,c
	if err != nil {
		t.Fatal(err)
	}
	if got := planShas(down); !eqStrs(got, []string{"a", "c", "b", "d"}) {
		t.Fatalf("moveDown = %v", got)
	}

	if _, err := BuildSingleEdit(commits, "zzz", EditDrop); err == nil {
		t.Fatal("missing target should error")
	}
	if _, err := BuildSingleEdit(commits, "d", EditMoveUp); err == nil {
		t.Fatal("move up the tip should error")
	}
	if _, err := BuildSingleEdit(commits, "a", EditMoveDown); err == nil {
		t.Fatal("move down the oldest should error")
	}
}
