package git

import "testing"

func TestParseFileLog(t *testing.T) {
	// One commit per format line ("%H\x1f%P\x1f%an\x1f%at\x1f%s"), each
	// followed by its --name-status line for the followed file.
	data := "" +
		"aaa\x1fppp\x1fAda\x1f1700000000\x1fmodify auth\n" +
		"M\tsrc/auth.go\n" +
		"\n" +
		"bbb\x1fqqq\x1fBob\x1f1690000000\x1frename file\n" +
		"R100\tsrc/old.go\tsrc/auth.go\n" +
		"\n" +
		"ccc\x1f\x1fAda\x1f1680000000\x1finitial\n" +
		"A\tsrc/old.go\n"

	got := ParseFileLog([]byte(data))
	if len(got) != 3 {
		t.Fatalf("want 3 commits, got %d", len(got))
	}
	if got[0].Hash != "aaa" || got[0].Status != "M" || got[0].Path != "src/auth.go" {
		t.Errorf("commit 0 wrong: %+v", got[0])
	}
	if got[0].Author != "Ada" || got[0].UnixTime != 1700000000 || got[0].Subject != "modify auth" {
		t.Errorf("commit 0 metadata wrong: %+v", got[0])
	}
	if got[1].Status != "R" || got[1].OldPath != "src/old.go" || got[1].Path != "src/auth.go" {
		t.Errorf("rename commit wrong: %+v", got[1])
	}
	if got[2].Status != "A" || got[2].Path != "src/old.go" || len(got[2].Parents) != 0 {
		t.Errorf("root commit wrong: %+v", got[2])
	}
}
