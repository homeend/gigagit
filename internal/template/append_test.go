package template

import "testing"

// AppendBlocker names what would keep an argument appended to a command from
// reaching the command's first program: a list or pipeline operator, a line
// that starts a new command, or a comment.
func TestAppendBlocker(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ cmd, goos, want string }{
		{`claude -p "x" --output-format json`, "linux", ""},
		{"claude -p \"x\" \\\n  --output-format json \\\n  --allowedTools \"Bash(git diff *)\"", "linux", ""},
		{`claude -p "a; b | c && d # e"`, "linux", ""},
		{`claude -p 'a; b # c'`, "linux", ""},
		{`claude -p x > "$GG_MESSAGE_FILE"`, "linux", ""},
		{`claude -p a\;b`, "linux", ""},
		{`claude -p x#y`, "linux", ""},
		{`claude -p x > out 2>&1`, "linux", ""},
		{`claude -p x &> out`, "linux", ""},
		{`claude -p x 2>&1 | tee log`, "linux", "|"},
		{`claude -p x > out 2>&1`, "windows", ""},
		{`claude -p x; echo done`, "linux", ";"},
		{`claude -p x | tee log`, "linux", "|"},
		{`claude -p x && echo ok`, "linux", "&&"},
		{`claude -p x || true`, "linux", "||"},
		{`claude -p x &`, "linux", "&"},
		{`claude -p x # review`, "linux", "#"},
		{"claude -p x\necho done", "linux", "a line break"},
		{`claude -p "it's" & echo`, "windows", "&"},
		{`claude -p x # not a comment in cmd`, "windows", ""},
		{`claude -p x ^& y`, "windows", ""},
		// cmd.exe runs the command flattened (FlattenForCmd): a trailing \
		// and a quote spanning lines join; a plain line break does not.
		{"claude -p x \\\n  --output-format json", "windows", ""},
		{"claude -p \"a\nb\" --output-format json", "windows", ""},
		{"claude -p x\necho done", "windows", "a line break"},
	} {
		if got := AppendBlockerFor(c.cmd, c.goos); got != c.want {
			t.Errorf("%s %q: got %q, want %q", c.goos, c.cmd, got, c.want)
		}
	}
}
