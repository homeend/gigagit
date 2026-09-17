package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/timespan"
)

// The blame overlay parses the age filter in the BROWSER, so filehist.js
// carries a port of internal/timespan (parseSpan/formatSpan for one span,
// parseFilter/formatFilter for the signed "+1d -7d" grammar).
// Nothing but this test keeps the two grammars in step, and the drift it
// prevents is the quiet kind — "1mo" refused in the terminal, silently read
// as a month (or a minute) in the browser. It drives the JS over the Go
// package's own conformance table, so a new rule added to Table on the Go
// side fails here until the port learns it too.
//
// Only the PURE section of filehist.js is evaluated: the rest of the module
// touches the DOM at import time and cannot run under node.
const spanPureStart = "// --- time span (pure; guarded against Go) ---"
const spanPureEnd = "// --- end time span ---"

func TestTimeSpanJSMatchesGo(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS port guard needs it")
	}
	src, err := os.ReadFile(filepath.Join("static", "filehist.js"))
	if err != nil {
		t.Fatal(err)
	}
	i := strings.Index(string(src), spanPureStart)
	j := strings.Index(string(src), spanPureEnd)
	if i < 0 || j < i {
		t.Fatalf("filehist.js: the guarded section markers are gone (%q / %q)", spanPureStart, spanPureEnd)
	}
	pure := string(src)[i:j]

	if len(timespan.Table) == 0 {
		t.Fatal("timespan.Table is empty; the parity check would pass vacuously")
	}
	// The Go side is the oracle; the table's Want/Filter/Formatted are
	// re-derived through Parse/Format (span rows) or ParseFilter/String
	// (filter rows) so a stale table row cannot hide a real drift.
	type input struct {
		In     string `json:"in"`
		Filter bool   `json:"filter"`
	}
	inputs := make([]input, len(timespan.Table))
	hasFilter := false
	for n, c := range timespan.Table {
		inputs[n] = input{In: c.In, Filter: c.IsFilter}
		if c.IsFilter {
			hasFilter = true
			f, perr := timespan.ParseFilter(c.In)
			if c.Err != (perr != nil) {
				t.Fatalf("timespan.Table[%d] %q: Err=%v but ParseFilter returned %v", n, c.In, c.Err, perr)
			}
			if !c.Err && (f != c.Filter || f.String() != c.Formatted) {
				t.Fatalf("timespan.Table[%d] %q: table says %+v/%q, ParseFilter/String say %+v/%q", n, c.In, c.Filter, c.Formatted, f, f.String())
			}
			continue
		}
		d, perr := timespan.Parse(c.In)
		if c.Err != (perr != nil) {
			t.Fatalf("timespan.Table[%d] %q: Err=%v but Parse returned %v", n, c.In, c.Err, perr)
		}
		if !c.Err && (d != c.Want || timespan.Format(d) != c.Formatted) {
			t.Fatalf("timespan.Table[%d] %q: table says %v/%q, Parse/Format say %v/%q", n, c.In, c.Want, c.Formatted, d, timespan.Format(d))
		}
	}
	if !hasFilter {
		t.Fatal("timespan.Table has no IsFilter row; parseFilter/formatFilter would go unguarded")
	}

	dir := t.TempDir()
	casesPath := filepath.Join(dir, "cases.json")
	blob, _ := json.Marshal(inputs)
	if err := os.WriteFile(casesPath, blob, 0o644); err != nil {
		t.Fatal(err)
	}
	// A span answer is {mins, fmt}: mins null when the port refuses the
	// input, fmt the badge for the parsed minutes (so formatSpan is exercised
	// on every accepted row, the same way the badge is built). A filter
	// answer is {f, fmt}: f null when refused, else {older, younger} in
	// minutes (null for an absent half), fmt the signed badge.
	script := `
import { readFileSync } from "node:fs";
const pure = readFileSync(process.argv[2], "utf8");
const inputs = JSON.parse(readFileSync(process.argv[3], "utf8"));
const fns = new Function(pure + "; return { parseSpan, formatSpan, parseFilter, formatFilter };")();
console.log(JSON.stringify(inputs.map((c) => {
  if (c.filter) {
    const f = fns.parseFilter(c.in);
    return { f, fmt: f === null ? "" : fns.formatFilter(f) };
  }
  const mins = fns.parseSpan(c.in);
  return { mins, fmt: mins === null ? "" : fns.formatSpan(mins) };
})));
`
	purePath := filepath.Join(dir, "pure.js")
	scriptPath := filepath.Join(dir, "check.mjs")
	if err := os.WriteFile(purePath, []byte(pure), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scriptPath, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, scriptPath, purePath, casesPath).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	type half struct {
		Older   *int64 `json:"older"`
		Younger *int64 `json:"younger"`
	}
	var got []struct {
		Mins *int64 `json:"mins"`
		F    *half  `json:"f"`
		Fmt  string `json:"fmt"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("node output %q: %v", out, err)
	}
	if len(got) != len(timespan.Table) {
		t.Fatalf("node returned %d answers, want %d", len(got), len(timespan.Table))
	}
	// sameHalf compares one signed half: absent on both sides, or the same
	// whole minutes on both.
	sameHalf := func(has bool, want time.Duration, got *int64) bool {
		if !has || got == nil {
			return !has && got == nil
		}
		return *got == int64(want/time.Minute)
	}
	for n, c := range timespan.Table {
		g := got[n]
		if c.IsFilter {
			if c.Err {
				if g.F != nil {
					t.Errorf("filter %q: Go refuses it, filehist.js parses it as %q", c.In, g.Fmt)
				}
				continue
			}
			if g.F == nil {
				t.Errorf("filter %q: Go parses it as %q, filehist.js refuses it", c.In, c.Formatted)
				continue
			}
			if !sameHalf(c.Filter.HasOlder, c.Filter.Older, g.F.Older) || !sameHalf(c.Filter.HasYounger, c.Filter.Younger, g.F.Younger) {
				t.Errorf("filter %q: Go parses %+v, filehist.js older=%v younger=%v", c.In, c.Filter, ptr(g.F.Older), ptr(g.F.Younger))
			}
			if g.Fmt != c.Formatted {
				t.Errorf("filter %q: Go formats %q, filehist.js %q", c.In, c.Formatted, g.Fmt)
			}
			continue
		}
		if c.Err {
			if g.Mins != nil {
				t.Errorf("%q: Go refuses it, filehist.js parses it as %d minutes", c.In, *g.Mins)
			}
			continue
		}
		if g.Mins == nil {
			t.Errorf("%q: Go parses it as %v, filehist.js refuses it", c.In, c.Want)
			continue
		}
		if want := int64(c.Want / time.Minute); *g.Mins != want {
			t.Errorf("%q: Go parses %d minutes, filehist.js %d", c.In, want, *g.Mins)
		}
		if g.Fmt != c.Formatted {
			t.Errorf("%q: Go formats %q, filehist.js %q", c.In, c.Formatted, g.Fmt)
		}
	}
}

// ptr prints an optional minute count as "nil" or the number, for messages.
func ptr(p *int64) any {
	if p == nil {
		return "nil"
	}
	return *p
}
