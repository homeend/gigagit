package agentdocs

import (
	"reflect"
	"testing"
)

// An anchor an agent wrapped in backticks is a code span: every renderer
// shows it as text, so the overview opens nothing there. Only anchor
// destinations count; a code span with a web link or plain brackets, and a
// fenced block (an example on purpose), are fine.
func TestBacktickedAnchors(t *testing.T) {
	text := "Before `[maybeReStick](internal/tui/viewport.go:135-150)` summed rows.\n\n" +
		"- `[bail out](a.go:144)` and [`inner`](a.go:2) and [plain](a.go:3)\n" +
		"- `[web](https://example.com)` and `arr[i](x)` stay\n\n" +
		"```\n[example](a.go:9)\n```\n\n" +
		"| a |\n|---|\n| `[cell](b.go)` |\n"
	got := BacktickedAnchors(text)
	want := []BacktickedAnchor{
		{Link: "[maybeReStick](internal/tui/viewport.go:135-150)"},
		{Link: "[bail out](a.go:144)"},
		{Link: "[cell](b.go)"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BacktickedAnchors = %+v\nwant %+v", got, want)
	}
	if got := BacktickedAnchors("`[x](a.go:1)` then `[x](a.go:1)` again"); len(got) != 1 {
		t.Fatalf("a repeated link is reported once, got %+v", got)
	}
	if got := BacktickedAnchors("[ok](a.go:1) and `code`"); len(got) != 0 {
		t.Fatalf("clean text reported %+v", got)
	}
	if msg := got0(text).Problem(); msg != "`[maybeReStick](internal/tui/viewport.go:135-150)`: a link inside backticks is plain text, not an anchor — write [maybeReStick](internal/tui/viewport.go:135-150) (for a code look, put the backticks inside the brackets)" {
		t.Fatalf("Problem() = %q", msg)
	}
}

func got0(text string) BacktickedAnchor { return BacktickedAnchors(text)[0] }
