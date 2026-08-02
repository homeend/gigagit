package template

import "testing"

func TestCQuotePath(t *testing.T) {
	if got := CQuotePath("plain/path.txt"); got != "plain/path.txt" {
		t.Errorf("clean path must be byte-exact, got %q", got)
	}
	if got := CQuotePath("a\nb"); got != `"a\nb"` {
		t.Errorf("newline path: got %q", got)
	}
	if got := CQuotePath("a\x01b"); got != `"a\001b"` {
		t.Errorf("control byte: got %q", got)
	}
	if got := CQuotePath(`a"b\c`); got != `a"b\c` {
		t.Errorf("quote/backslash WITHOUT control bytes stays unquoted, got %q", got)
	}
}

func TestConflictContextDoc(t *testing.T) {
	got := ConflictContextDoc("merge", "feat/x", "main", []string{"a.txt", "b\nc.txt"})
	want := "op: merge\nsource: feat/x\ntarget: main\nconflicted:\na.txt\n\"b\\nc.txt\"\n"
	if got != want {
		t.Errorf("doc mismatch:\n got %q\nwant %q", got, want)
	}
}
