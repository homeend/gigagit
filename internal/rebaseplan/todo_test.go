package rebaseplan

import "testing"

func TestRewriteTodo(t *testing.T) {
	p := Plan{Entries: []Entry{
		{Sha: "aaaaaaa", Action: Pick, Orig: "A"},
		{Sha: "bbbbbbb", Action: Reword, Orig: "B", NewMsg: "B2"},
		{Sha: "ccccccc", Action: Squash, Orig: "C"}, // melds into B
		{Sha: "ddddddd", Action: Drop, Orig: "D"},   // explicit drop line
		{Sha: "eeeeeee", Action: Pick, Orig: "E"},   // plain pick, no exec
	}}
	got, err := p.RewriteTodo("/usr/bin/gg", "/tmp/plan.json", "linux")
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	want := "pick aaaaaaa\n" +
		"pick bbbbbbb\n" +
		"fixup ccccccc\n" +
		`exec '/usr/bin/gg' __rebase-message '/tmp/plan.json' 1` + "\n" +
		"drop ddddddd\n" +
		"pick eeeeeee\n"
	if got != want {
		t.Fatalf("todo =\n%q\nwant\n%q", got, want)
	}
}

// TestRewriteTodoAllDrops pins the drop-the-whole-range case (e.g. dropping the
// branch tip, where the range holds only that commit): the todo must carry
// explicit `drop` lines — an EMPTY todo makes git abort with "nothing to do".
func TestRewriteTodoAllDrops(t *testing.T) {
	p := Plan{Entries: []Entry{
		{Sha: "aaaaaaa", Action: Drop, Orig: "A"},
		{Sha: "bbbbbbb", Action: Drop, Orig: "B"},
	}}
	got, err := p.RewriteTodo("gg", "/tmp/p", "linux")
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if want := "drop aaaaaaa\ndrop bbbbbbb\n"; got != want {
		t.Fatalf("todo = %q, want %q", got, want)
	}
}

func TestRewriteTodoSquashFirstErrors(t *testing.T) {
	p := Plan{Entries: []Entry{{Sha: "a", Action: Squash, Orig: "A"}}}
	if _, err := p.RewriteTodo("gg", "/tmp/p", "linux"); err == nil {
		t.Fatal("leading squash must error")
	}
}
