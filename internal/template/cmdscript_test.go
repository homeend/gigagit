package template

import (
	"strings"
	"testing"
)

// The reported failure: "Claude (yolo)" launched in normal permission mode on
// Windows. Its prompt is a double-quoted string spanning five lines with
// --dangerously-skip-permissions at the very end, so cmd.exe ran claude at the
// end of line 1 — truncated prompt, no flags at all.
func TestFlattenForCmdJoinsAQuotedPromptSpanningLines(t *testing.T) {
	in := `claude "A git %GG_OP% operation is paused with conflicts in this repository.
   Read the context file at %GG_CONTEXT_FILE% for the operation's parties and the conflicted paths.
   stop when everything is staged and summarize what you chose and why." --dangerously-skip-permissions`

	got := FlattenForCmd(in)
	if strings.Contains(got, "\r\n") || strings.Contains(got, "\n") {
		t.Fatalf("command still spans lines:\n%s", got)
	}
	if !strings.HasSuffix(got, "--dangerously-skip-permissions") {
		t.Errorf("the flag did not survive:\n%s", got)
	}
	if !strings.HasPrefix(got, `claude "A git %GG_OP%`) {
		t.Errorf("the command head changed:\n%s", got)
	}
	// The quote must still be balanced, or cmd would swallow the flag as part
	// of the argument.
	if strings.Count(got, `"`)%2 != 0 {
		t.Errorf("unbalanced quotes:\n%s", got)
	}
}

// The non-yolo template uses POSIX line continuations, which cmd.exe passes on
// as literal arguments — the flags are lost the same way.
func TestFlattenForCmdJoinsBackslashContinuations(t *testing.T) {
	in := "claude \"do the thing\" \\\n  --permission-mode acceptEdits \\\n  --allowedTools \"Read\" \"Edit\""
	got := FlattenForCmd(in)
	want := `claude "do the thing"   --permission-mode acceptEdits   --allowedTools "Read" "Edit"`
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	if strings.Contains(got, `\`) {
		t.Errorf("a continuation backslash survived as an argument: %q", got)
	}
}

// A genuine multi-line batch script (a user's post-create hook) must keep its
// lines: they are separate commands and joining them would break it.
func TestFlattenForCmdKeepsRealMultilineScript(t *testing.T) {
	in := "echo one\necho two\nnpm install"
	got := FlattenForCmd(in)
	want := "echo one\r\necho two\r\nnpm install"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestFlattenForCmdCRLFInputAndEdges(t *testing.T) {
	if got, want := FlattenForCmd("echo a\r\necho b"), "echo a\r\necho b"; got != want {
		t.Errorf("CRLF input: got %q, want %q", got, want)
	}
	if got, want := FlattenForCmd(""), ""; got != want {
		t.Errorf("empty: got %q, want %q", got, want)
	}
	if got, want := FlattenForCmd("one line"), "one line"; got != want {
		t.Errorf("single line: got %q, want %q", got, want)
	}
	// An unterminated quote on the LAST line has nothing to join to: it must
	// not swallow anything or drop the line.
	if got, want := FlattenForCmd(`echo "oops`), `echo "oops`; got != want {
		t.Errorf("dangling quote: got %q, want %q", got, want)
	}
	// A quoted string that opens and closes on the same line does not join.
	if got, want := FlattenForCmd("echo \"a b\"\necho c"), "echo \"a b\"\r\necho c"; got != want {
		t.Errorf("balanced line: got %q, want %q", got, want)
	}
}
