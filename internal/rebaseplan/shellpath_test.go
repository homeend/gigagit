package rebaseplan

import "testing"

func TestShellPathWindows(t *testing.T) {
	// The reported failure: unquoted backslashes reached git's bundled sh as
	// escapes, so `t:\others\…\gg-web.exe` ran as `t:othersgigagit…gg-web.exe`
	// and sh said "command not found".
	got := ShellPath(`t:\others\gigagit.worktrees\web-dev\gg-web.exe`, "windows")
	want := `'t:/others/gigagit.worktrees/web-dev/gg-web.exe'`
	if got != want {
		t.Errorf("ShellPath = %s, want %s", got, want)
	}
	// A temp path with a space in the user's name must survive too.
	got = ShellPath(`C:\Users\Ann Lee\AppData\Local\Temp\gg-rebase-plan-1.json`, "windows")
	want = `'C:/Users/Ann Lee/AppData/Local/Temp/gg-rebase-plan-1.json'`
	if got != want {
		t.Errorf("ShellPath = %s, want %s", got, want)
	}
}

func TestShellPathPOSIX(t *testing.T) {
	if got, want := ShellPath("/home/me/gg", "linux"), `'/home/me/gg'`; got != want {
		t.Errorf("ShellPath = %s, want %s", got, want)
	}
	if got, want := ShellPath("/home/my dir/gg", "linux"), `'/home/my dir/gg'`; got != want {
		t.Errorf("ShellPath = %s, want %s", got, want)
	}
	// A backslash is a legal POSIX filename character: rewriting it would
	// break a real path, so the conversion stays Windows-only.
	if got, want := ShellPath(`/home/we\ird/gg`, "linux"), `'/home/we\ird/gg'`; got != want {
		t.Errorf("ShellPath = %s, want %s", got, want)
	}
	// A single quote ends the quoting, so it needs the '\'' dance.
	if got, want := ShellPath("/home/it's/gg", "linux"), `'/home/it'\''s/gg'`; got != want {
		t.Errorf("ShellPath = %s, want %s", got, want)
	}
}

func TestSequenceEditor(t *testing.T) {
	got := SequenceEditor(`t:\gg.exe`, `C:\Temp\plan.json`, "windows")
	want := `'t:/gg.exe' __rebase-seq 'C:/Temp/plan.json'`
	if got != want {
		t.Errorf("SequenceEditor = %s, want %s", got, want)
	}
}

// The `exec` todo lines run through the same shell as the sequence editor, so
// they carry the same quoting — the old %q form emitted Go escapes, which sh
// does not undo the same way (and \u for a non-ASCII path never survives).
func TestRewriteTodoQuotesForWindowsShell(t *testing.T) {
	p := Plan{Entries: []Entry{
		{Sha: "aaa", Action: Pick, Orig: "one\n"},
		{Sha: "bbb", Action: Squash, Orig: "two\n"},
	}}
	todo, err := p.RewriteTodo(`t:\others\gg.exe`, `C:\Temp\plan.json`, "windows")
	if err != nil {
		t.Fatal(err)
	}
	want := "pick aaa\nfixup bbb\nexec 't:/others/gg.exe' __rebase-message 'C:/Temp/plan.json' 0\n"
	if todo != want {
		t.Errorf("todo =\n%s\nwant\n%s", todo, want)
	}
}
