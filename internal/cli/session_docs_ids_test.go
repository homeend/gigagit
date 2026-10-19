package cli

import (
	"bytes"
	"strings"
	"testing"
)

// A document id — a session document's file name — passes the verbs' id
// check (the usage gate); a path-like word still does not. No session is
// live here: an accepted id fails later, not as a misuse.
func TestSessionVerbsAcceptDocumentIDs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, c := range []struct {
		args []string
		ok   bool
	}{
		{[]string{"overview", "show", "20260101T220000-ab12"}, true},
		{[]string{"overview", "rm", "tour-brief-123-4"}, true},
		{[]string{"overview", "show", "f1"}, true},
		{[]string{"overview", "show", "a/b"}, false},
		{[]string{"overview", "show", "a.md"}, false},
		{[]string{"note", "show", "20260101T220100-cd34"}, true},
		{[]string{"note", "rm", "t1"}, true},
		{[]string{"note", "rm", "../x"}, false},
	} {
		var out, errb bytes.Buffer
		code := runSessionIn(dir, nil, c.args, nil, &out, &errb)
		usage := code == 2 && strings.Contains(errb.String(), "usage: gg session")
		if c.ok && usage {
			t.Errorf("%v: refused as misuse: %s", c.args, errb.String())
		}
		if !c.ok && !usage {
			t.Errorf("%v: not a misuse (code %d): %s", c.args, code, errb.String())
		}
	}
}
