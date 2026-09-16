# Branch Filters Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Five configurable, named branch-filter slots (`alt+1…5`) that hide or show-only branches by tip age and name, on the TUI and web Branches/Remotes lists, defined in `.gg.toml` and remembered per repo.

**Architecture:** A new DAG-leaf package `internal/branchfilter` owns the slot type, validation, and the one matcher. `config` decodes `[[branches.filter]]` blocks and overlays them by slot number; `promptstate` remembers the active slot per repo per list; `domain` compiles the effective slots and computes exemptions. The TUI filters in `displayIndices` (memoised); the web server attaches verdicts to `/api/branches` and `/api/remotes` and the client only renders. No engine op, no CLI/MCP surface.

**Tech Stack:** Go 1.26, Bubble Tea (TUI), stdlib `net/http` + vanilla ES modules (web), go-toml, real `git` in `t.TempDir()` for tests, Playwright for the browser probe, `tui-capture.sh` for the headless TUI check.

**Spec:** `docs/superpowers/specs/2026-09-16-branch-filters-design.md` (in this worktree). Two refinements the plan makes and Task 1 writes back into the spec:
1. TOML shape is `[[branches.filter]]` under a `[branches]` section (a bare `[branch_filters]` header, which `gg config populate` would emit for a settingDoc, is a TOML error next to `[[branch_filters]]` blocks).
2. Active-slot persistence lives in the frontends via `promptstate` (both already open `prompts.toml`; domain has no state-dir handle). Domain provides only `BranchFilters` + the exemption helpers.
3. `branchfilter.Slot` keeps `OlderThan`/`YoungerThan` as the raw strings (`"90d"`) so config decodes straight into it; `Compile` parses them.

## Global Constraints

- Work in the worktree `/mnt/t/others/gigagit/.claude/worktrees/branch-filters` on branch `feat/branch-filters`. Prefix every shell command with `cd /mnt/t/others/gigagit/.claude/worktrees/branch-filters &&`; use absolute paths for Write/Edit. Never commit in the main checkout.
- `internal/branchfilter` imports stdlib only (`internal/archtest` will gate it).
- `internal/tui`, `internal/cli`, `internal/web`, `internal/mcp` never import `internal/git`.
- Every user-visible TUI string goes through `i18n.T` with a literal key present in all four bundles (`internal/i18n/lang/{ja,ko,zh,ru}.toml`); the AST-gate tests in `internal/tui` (`i18n_scan_test.go`, `options_vocab_test.go`, `menu_labels_test.go`, `engine_prose_test.go`) fail otherwise. Follow the `adding-translations` skill when adding keys.
- New tests call `t.Parallel()` unless they touch global state (env vars, `XDG_STATE_HOME`).
- Web tests that touch state set `t.Setenv("XDG_STATE_HOME", t.TempDir())` (see `uiServer` in `internal/web/uistate_test.go`).
- Keys: `alt+1`…`alt+5` (TUI, Bubble Tea key string `"alt+1"`); web `alt+DigitN` → Branches, `alt+shift+DigitN` → Remotes, matched on `e.code`.
- Radio semantics: one active slot per list; pressing the active slot's key clears it.
- Exempt rows (HEAD, worktree-checked-out branches, HEAD's upstream on Remotes) are never hidden.
- Unknown tip time (`UnixTime == 0`) fails any age clause.
- Invalid slot ⇒ inert with a reason, never an error, never an empty list.
- Commit after every task with the trailer lines given in the session (Co-Authored-By + Claude-Session). Package tests run in the foreground; the controller runs `./test.sh race` once at the end.

---

## File map

| File | Responsibility |
|---|---|
| `internal/branchfilter/branchfilter.go` (new) | `Slot`, `Mode`, `Compiled`, `ParseAge`, `Compile`, `CompileAll`, `Matches`, `Apply`, `Row`, `Verdict` |
| `internal/branchfilter/branchfilter_test.go` (new) | table tests |
| `internal/config/branches.go` (new) | `BranchesConfig`, `overlayBranchFilters`, `SetBranchFilter`, `RemoveBranchFilter` |
| `internal/config/branches_test.go` (new) | decode/overlay/writer tests |
| `internal/config/config.go` | `Config.Branches` field + overlay call in `Load` |
| `internal/config/template.go` | settingDoc row for `[branches] filter` |
| `internal/promptstate/branchfilter.go` (new) | `BranchFilterSlot`, `SetBranchFilterSlot` |
| `internal/promptstate/file_store.go`, `store.go` | record field + interface methods |
| `internal/domain/branchfilter.go` (new) | `BranchFilters`, `ExemptBranches`, `ExemptRemoteBranches` |
| `internal/tui/branch_filter.go` (new) | model state, alt+N handling, memo, header text, status messages |
| `internal/tui/branch_filter_popup.go` (new) | Settings → Branch filters… browse + edit form |
| `internal/tui/model.go`, `viewstate.go`, `footer.go`, `help.go`, `settings_popup.go` | wiring |
| `internal/web/branchfilter.go` (new) | verdict attachment + `PUT /api/branch-filter` |
| `internal/web/branches.go`, `remotes.go`, `server.go`, `settings.go` | wiring |
| `internal/web/static/branchfilter.js` (new) | chip, menu, key routing, state |
| `internal/web/static/sidebar.js`, `keys.js`, `settings.js`, `style.css`, `index.html` | wiring |
| `CHANGELOG.md`, `README.md`, `docs/CLAUDE-details.md`, `CLAUDE.md` | docs |

---

### Task 1: The `branchfilter` leaf package

**Files:**
- Create: `internal/branchfilter/branchfilter.go`
- Create: `internal/branchfilter/branchfilter_test.go`
- Modify: `internal/archtest/import_guard_test.go` (add the stdlib-only guard, copy the `changeset` block at ~line 145)
- Modify: `docs/superpowers/specs/2026-09-16-branch-filters-design.md` (the three refinements above)

**Interfaces:**
- Produces:
  ```go
  package branchfilter
  type Mode string
  const ( ModeHide Mode = "hide"; ModeShow Mode = "show" )
  const MaxSlots = 5
  type Slot struct {
      Slot        int    `toml:"slot"`
      Name        string `toml:"name"`
      Mode        Mode   `toml:"mode"`
      OlderThan   string `toml:"older_than"`
      YoungerThan string `toml:"younger_than"`
      Prefix      string `toml:"prefix"`
      Suffix      string `toml:"suffix"`
      Contains    string `toml:"contains"`
      Regex       string `toml:"regex"`
  }
  type Compiled struct { Slot; Err error; Empty bool; older, younger time.Duration; re *regexp.Regexp }
  func (c Compiled) Usable() bool           // Err == nil && !Empty
  func (c Compiled) Label() string          // Name, or "slot N"
  func (c Compiled) Summary() string        // "hide · older than 90d, prefix feat/"
  func ParseAge(s string) (time.Duration, error)
  func FormatAge(d time.Duration) string    // inverse for Summary: 90d, 12w, 6m, 1y (largest exact unit)
  func Compile(s Slot) Compiled
  func CompileAll(slots []Slot) [MaxSlots]Compiled   // index = slot-1; duplicates: later inert; missing: zero Compiled with Empty=true
  func (c Compiled) Matches(name string, unixTime int64, now time.Time) bool
  type Row struct { Name string; UnixTime int64 }
  type Verdict struct { Hidden, Exempt bool }
  func Apply(c Compiled, rows []Row, exempt []bool, now time.Time) (v []Verdict, hidden int)
  ```

- [ ] **Step 1: Write the failing tests**

`internal/branchfilter/branchfilter_test.go`:

```go
package branchfilter

import (
	"testing"
	"time"
)

var now = time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)

func ago(d time.Duration) int64 { return now.Add(-d).Unix() }

func TestParseAge(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want time.Duration
		ok   bool
	}{
		{"90d", 90 * 24 * time.Hour, true},
		{"12w", 12 * 7 * 24 * time.Hour, true},
		{"6m", 6 * 30 * 24 * time.Hour, true},
		{"1y", 365 * 24 * time.Hour, true},
		{" 3d ", 3 * 24 * time.Hour, true},
		{"", 0, false},
		{"d", 0, false},
		{"-3d", 0, false},
		{"0d", 0, false},
		{"3", 0, false},
		{"3h", 0, false},
		{"3dd", 0, false},
	}
	for _, c := range cases {
		got, err := ParseAge(c.in)
		if (err == nil) != c.ok || got != c.want {
			t.Errorf("ParseAge(%q) = %v, %v; want %v ok=%v", c.in, got, err, c.want, c.ok)
		}
	}
	for _, s := range []string{"90d", "12w", "6m", "1y", "14d"} {
		d, _ := ParseAge(s)
		if FormatAge(d) != s {
			t.Errorf("FormatAge(ParseAge(%q)) = %q", s, FormatAge(d))
		}
	}
}

func TestCompileInertReasons(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		s    Slot
		want string // substring of Err
	}{
		{"slot 0", Slot{Slot: 0, Prefix: "x"}, "slot must be 1..5"},
		{"slot 6", Slot{Slot: 6, Prefix: "x"}, "slot must be 1..5"},
		{"bad mode", Slot{Slot: 1, Mode: "drop", Prefix: "x"}, "mode must be hide or show"},
		{"bad age", Slot{Slot: 1, OlderThan: "soon"}, "older_than"},
		{"bad younger", Slot{Slot: 1, YoungerThan: "3h"}, "younger_than"},
		{"bad regex", Slot{Slot: 1, Regex: "("}, "regex"},
	}
	for _, c := range cases {
		got := Compile(c.s)
		if got.Err == nil || !contains(got.Err.Error(), c.want) {
			t.Errorf("%s: Err = %v; want containing %q", c.name, got.Err, c.want)
		}
		if got.Usable() {
			t.Errorf("%s: inert slot reported usable", c.name)
		}
	}
}

func TestCompileEmptyAndDefaults(t *testing.T) {
	t.Parallel()
	c := Compile(Slot{Slot: 3})
	if c.Err != nil || !c.Empty || c.Usable() {
		t.Fatalf("empty slot: Err=%v Empty=%v Usable=%v", c.Err, c.Empty, c.Usable())
	}
	if c.Label() != "slot 3" {
		t.Errorf("Label = %q", c.Label())
	}
	c = Compile(Slot{Slot: 1, Name: "stale", Prefix: "feat/"})
	if c.Mode != ModeHide {
		t.Errorf("default mode = %q; want hide", c.Mode)
	}
	if c.Label() != "stale" {
		t.Errorf("Label = %q", c.Label())
	}
}

func TestMatchesClauses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		s    Slot
		row  string
		age  int64
		want bool
	}{
		{"prefix hit", Slot{Slot: 1, Prefix: "feat/"}, "feat/x", ago(time.Hour), true},
		{"prefix miss", Slot{Slot: 1, Prefix: "feat/"}, "fix/x", ago(time.Hour), false},
		{"prefix case-sensitive", Slot{Slot: 1, Prefix: "Feat/"}, "feat/x", ago(time.Hour), false},
		{"suffix", Slot{Slot: 1, Suffix: "-wip"}, "a-wip", ago(time.Hour), true},
		{"contains", Slot{Slot: 1, Contains: "hot"}, "x/hotfix/y", ago(time.Hour), true},
		{"regex", Slot{Slot: 1, Regex: `^release/\d+$`}, "release/12", ago(time.Hour), true},
		{"regex unanchored", Slot{Slot: 1, Regex: `\d`}, "v1", ago(time.Hour), true},
		{"older hit", Slot{Slot: 1, OlderThan: "90d"}, "x", ago(91 * 24 * time.Hour), true},
		{"older miss", Slot{Slot: 1, OlderThan: "90d"}, "x", ago(89 * 24 * time.Hour), false},
		{"younger hit", Slot{Slot: 1, YoungerThan: "7d"}, "x", ago(24 * time.Hour), true},
		{"younger miss", Slot{Slot: 1, YoungerThan: "7d"}, "x", ago(8 * 24 * time.Hour), false},
		{"unknown time fails older", Slot{Slot: 1, OlderThan: "1d"}, "x", 0, false},
		{"unknown time fails younger", Slot{Slot: 1, YoungerThan: "1y"}, "x", 0, false},
		{"AND both hold", Slot{Slot: 1, Prefix: "feat/", OlderThan: "30d"}, "feat/x", ago(40 * 24 * time.Hour), true},
		{"AND one fails", Slot{Slot: 1, Prefix: "feat/", OlderThan: "30d"}, "feat/x", ago(10 * 24 * time.Hour), false},
		{"empty matches nothing", Slot{Slot: 1}, "anything", ago(time.Hour), false},
	}
	for _, c := range cases {
		got := Compile(c.s).Matches(c.row, c.age, now)
		if got != c.want {
			t.Errorf("%s: Matches = %v; want %v", c.name, got, c.want)
		}
	}
}

func TestApplyModesAndExempt(t *testing.T) {
	t.Parallel()
	rows := []Row{{"feat/a", ago(time.Hour)}, {"main", ago(time.Hour)}, {"feat/b", ago(time.Hour)}}
	exempt := []bool{false, true, true} // main is HEAD, feat/b checked out elsewhere

	hide := Compile(Slot{Slot: 1, Mode: ModeHide, Prefix: "feat/"})
	v, n := Apply(hide, rows, exempt, now)
	want := []Verdict{{Hidden: true}, {}, {Exempt: true}}
	if n != 1 || !equalVerdicts(v, want) {
		t.Errorf("hide: v=%v n=%d; want %v n=1", v, n, want)
	}

	show := Compile(Slot{Slot: 2, Mode: ModeShow, Prefix: "feat/"})
	v, n = Apply(show, rows, exempt, now)
	want = []Verdict{{}, {Exempt: true}, {}}
	if n != 0 || !equalVerdicts(v, want) {
		t.Errorf("show: v=%v n=%d; want %v n=0", v, n, want)
	}

	// An unusable slot hides nothing and marks nothing.
	v, n = Apply(Compile(Slot{Slot: 1, Regex: "("}), rows, exempt, now)
	if n != 0 || !equalVerdicts(v, []Verdict{{}, {}, {}}) {
		t.Errorf("inert: v=%v n=%d", v, n)
	}
	// nil exempt is allowed.
	v, n = Apply(hide, rows, nil, now)
	if n != 2 || !v[2].Hidden {
		t.Errorf("nil exempt: v=%v n=%d", v, n)
	}
}

func TestCompileAll(t *testing.T) {
	t.Parallel()
	all := CompileAll([]Slot{
		{Slot: 2, Name: "a", Prefix: "a/"},
		{Slot: 2, Name: "b", Prefix: "b/"}, // duplicate: later inert
		{Slot: 7, Prefix: "x"},            // out of range: dropped (nowhere to put it)
		{Slot: 5, Name: "e", Suffix: "e"},
	})
	if all[1].Name != "a" || !all[1].Usable() {
		t.Errorf("slot 2 = %+v; want the first block", all[1].Slot)
	}
	if all[0].Err != nil || !all[0].Empty {
		t.Errorf("slot 1 should be empty: %+v", all[0])
	}
	if !all[4].Usable() {
		t.Errorf("slot 5 unusable: %v", all[4].Err)
	}
	if s := all[1].Summary(); s != "hide · prefix a/" {
		t.Errorf("Summary = %q", s)
	}
	if s := Compile(Slot{Slot: 1, Mode: ModeShow, OlderThan: "90d", Prefix: "feat/", Regex: `x$`}).Summary(); s != "show only · older than 90d, prefix feat/, regex x$" {
		t.Errorf("Summary = %q", s)
	}
}

func contains(s, sub string) bool { return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0) }
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
func equalVerdicts(a, b []Verdict) bool {
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
```

- [ ] **Step 2: Run to verify failure**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/branch-filters && go test ./internal/branchfilter/ 2>&1 | head -5`
Expected: build failure (package has no non-test files).

- [ ] **Step 3: Implement**

`internal/branchfilter/branchfilter.go`:

```go
// Package branchfilter is the one evaluator behind gg's five branch-filter
// slots: a named rule that hides, or shows only, branches by tip age and
// name. It is a DAG leaf (stdlib only) so config can decode into Slot, domain
// can compile, and both the TUI and the web server evaluate the SAME matcher —
// a regex accepted here is accepted everywhere (Go RE2), never "works in the
// browser, inert in the terminal".
package branchfilter

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Mode says what a match does.
type Mode string

const (
	ModeHide Mode = "hide" // matching branches are hidden
	ModeShow Mode = "show" // ONLY matching branches are shown
)

// MaxSlots is how many numbered slots exist (alt+1 … alt+5).
const MaxSlots = 5

// Slot is one [[branches.filter]] block as written. Age fields keep the
// user's text ("90d") so config decodes straight into this type; Compile
// parses them. Every set clause must hold (AND) for a branch to match.
type Slot struct {
	Slot        int    `toml:"slot"`
	Name        string `toml:"name"`
	Mode        Mode   `toml:"mode"`
	OlderThan   string `toml:"older_than"`
	YoungerThan string `toml:"younger_than"`
	Prefix      string `toml:"prefix"`
	Suffix      string `toml:"suffix"`
	Contains    string `toml:"contains"`
	Regex       string `toml:"regex"`
}

// Compiled is a Slot ready to evaluate. Err != nil means the block is inert
// (never an error for the caller — the list simply stays unfiltered and the
// reason is shown). Empty means no clause is set: valid, but matches nothing.
type Compiled struct {
	Slot
	Err   error
	Empty bool

	older, younger time.Duration
	re             *regexp.Regexp
}

// Usable reports whether activating this slot filters anything.
func (c Compiled) Usable() bool { return c.Err == nil && !c.Empty }

// Label is the display name: Name, or "slot N".
func (c Compiled) Label() string {
	if strings.TrimSpace(c.Name) != "" {
		return c.Name
	}
	return "slot " + strconv.Itoa(c.Slot.Slot)
}

// Summary renders the rule in one line for Settings rows and chip menus:
// "hide · older than 90d, prefix feat/". Inert slots render their reason.
func (c Compiled) Summary() string {
	if c.Err != nil {
		return "invalid — " + c.Err.Error()
	}
	if c.Empty {
		return "empty (no clause set)"
	}
	var parts []string
	if c.OlderThan != "" {
		parts = append(parts, "older than "+strings.TrimSpace(c.OlderThan))
	}
	if c.YoungerThan != "" {
		parts = append(parts, "younger than "+strings.TrimSpace(c.YoungerThan))
	}
	if c.Prefix != "" {
		parts = append(parts, "prefix "+c.Prefix)
	}
	if c.Suffix != "" {
		parts = append(parts, "suffix "+c.Suffix)
	}
	if c.Contains != "" {
		parts = append(parts, "contains "+c.Contains)
	}
	if c.Regex != "" {
		parts = append(parts, "regex "+c.Regex)
	}
	mode := "hide"
	if c.Mode == ModeShow {
		mode = "show only"
	}
	return mode + " · " + strings.Join(parts, ", ")
}

// ageUnits is the accepted suffix set. Months and years are calendar-free
// approximations on purpose: "6m" is a filter threshold, not a date.
var ageUnits = map[byte]time.Duration{
	'd': 24 * time.Hour,
	'w': 7 * 24 * time.Hour,
	'm': 30 * 24 * time.Hour,
	'y': 365 * 24 * time.Hour,
}

// ParseAge parses "<n><unit>" with unit d|w|m|y and n a positive integer.
func ParseAge(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return 0, errors.New("age must be a number followed by d, w, m or y (e.g. 90d)")
	}
	unit, ok := ageUnits[s[len(s)-1]]
	if !ok {
		return 0, errors.New("age unit must be d, w, m or y (e.g. 90d)")
	}
	n, err := strconv.Atoi(s[:len(s)-1])
	if err != nil || n <= 0 {
		return 0, errors.New("age must be a positive whole number of d, w, m or y (e.g. 90d)")
	}
	return time.Duration(n) * unit, nil
}

// FormatAge renders d in the largest unit that divides it exactly.
func FormatAge(d time.Duration) string {
	for _, u := range []struct {
		s byte
		d time.Duration
	}{{'y', ageUnits['y']}, {'m', ageUnits['m']}, {'w', ageUnits['w']}, {'d', ageUnits['d']}} {
		if d > 0 && d%u.d == 0 {
			return strconv.Itoa(int(d/u.d)) + string(u.s)
		}
	}
	return d.String()
}

// Compile validates s. It never panics and never returns an error value —
// the failure travels inside Compiled.Err so the caller can show it.
func Compile(s Slot) Compiled {
	c := Compiled{Slot: s}
	if s.Slot < 1 || s.Slot > MaxSlots {
		c.Err = fmt.Errorf("slot must be 1..%d", MaxSlots)
		return c
	}
	switch s.Mode {
	case "":
		c.Mode = ModeHide
	case ModeHide, ModeShow:
	default:
		c.Err = errors.New("mode must be hide or show")
		return c
	}
	var err error
	if strings.TrimSpace(s.OlderThan) != "" {
		if c.older, err = ParseAge(s.OlderThan); err != nil {
			c.Err = fmt.Errorf("older_than: %w", err)
			return c
		}
	}
	if strings.TrimSpace(s.YoungerThan) != "" {
		if c.younger, err = ParseAge(s.YoungerThan); err != nil {
			c.Err = fmt.Errorf("younger_than: %w", err)
			return c
		}
	}
	if s.Regex != "" {
		if c.re, err = regexp.Compile(s.Regex); err != nil {
			c.Err = fmt.Errorf("regex: %w", err)
			return c
		}
	}
	c.Empty = c.older == 0 && c.younger == 0 && s.Prefix == "" && s.Suffix == "" && s.Contains == "" && s.Regex == ""
	return c
}

// CompileAll places blocks by slot number (index slot-1). A later block for
// an already-filled slot is inert ("duplicate of slot N") and the first
// wins, so a hand-edit never silently flips a rule; a block whose slot is out
// of range has no index to land on and is dropped. Unfilled slots are Empty.
func CompileAll(slots []Slot) [MaxSlots]Compiled {
	var out [MaxSlots]Compiled
	var filled [MaxSlots]bool
	for i := range out {
		out[i] = Compiled{Slot: Slot{Slot: i + 1, Mode: ModeHide}, Empty: true}
	}
	for _, s := range slots {
		if s.Slot < 1 || s.Slot > MaxSlots {
			continue
		}
		if filled[s.Slot-1] {
			continue // first wins; the duplicate is reported by config-level validation
		}
		out[s.Slot-1] = Compile(s)
		filled[s.Slot-1] = true
	}
	return out
}

// Matches reports whether a branch named name (the branch PART for a remote
// branch: "feat/x", not "origin/feat/x") whose tip was committed at unixTime
// (0 = unknown) satisfies every set clause. An unknown time fails any age
// clause; an empty or inert rule matches nothing.
func (c Compiled) Matches(name string, unixTime int64, now time.Time) bool {
	if !c.Usable() {
		return false
	}
	if c.older > 0 || c.younger > 0 {
		if unixTime == 0 {
			return false
		}
		age := now.Sub(time.Unix(unixTime, 0))
		if c.older > 0 && age <= c.older {
			return false
		}
		if c.younger > 0 && age >= c.younger {
			return false
		}
	}
	if c.Prefix != "" && !strings.HasPrefix(name, c.Prefix) {
		return false
	}
	if c.Suffix != "" && !strings.HasSuffix(name, c.Suffix) {
		return false
	}
	if c.Contains != "" && !strings.Contains(name, c.Contains) {
		return false
	}
	if c.re != nil && !c.re.MatchString(name) {
		return false
	}
	return true
}

// Row is what Apply evaluates: the tested name and the tip time.
type Row struct {
	Name     string
	UnixTime int64
}

// Verdict is one row's fate under an active slot. Exempt is set when the
// rule WOULD have hidden the row but it is protected (HEAD, checked out in a
// worktree, the upstream row) — the frontends dim-mark it.
type Verdict struct {
	Hidden, Exempt bool
}

// Apply evaluates every row under c. exempt may be nil or shorter than rows
// (missing entries are false). hidden counts rows with Hidden set.
func Apply(c Compiled, rows []Row, exempt []bool, now time.Time) (v []Verdict, hidden int) {
	v = make([]Verdict, len(rows))
	if !c.Usable() {
		return v, 0
	}
	for i, r := range rows {
		m := c.Matches(r.Name, r.UnixTime, now)
		wouldHide := (c.Mode == ModeHide && m) || (c.Mode == ModeShow && !m)
		if !wouldHide {
			continue
		}
		if i < len(exempt) && exempt[i] {
			v[i].Exempt = true
			continue
		}
		v[i].Hidden = true
		hidden++
	}
	return v, hidden
}
```

- [ ] **Step 4: Run the tests**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/branch-filters && go test ./internal/branchfilter/ -v 2>&1 | tail -15`
Expected: all PASS.

- [ ] **Step 5: Add the archtest guard**

In `internal/archtest/import_guard_test.go`, find the `changeset` stdlib-only block (search `internal/changeset imports`) and add a sibling test `TestBranchfilterIsStdlibOnly` with the same body, package name `branchfilter`, message `internal/branchfilter imports %s — it must stay stdlib only`. Run `go test ./internal/archtest/` → PASS.

- [ ] **Step 6: Write the three refinements into the spec**

Edit `docs/superpowers/specs/2026-09-16-branch-filters-design.md`: replace every `[[branch_filters]]` with `[[branches.filter]]` and add a sentence under "Config: the slot definition": "The blocks live under a `[branches]` section because `gg config populate` documents each section with a plain `[section]` header and a bare `[branch_filters]` table would collide with array-of-table blocks of the same name." In "Domain", delete `ActiveBranchFilter`/`SetActiveBranchFilter`/`FilterBranches`/`FilterRemoteBranches` and replace with the Task 4 signatures; in "Active slot" add "The frontends read and write this record themselves (as they do for dismissed notices); domain has no state-dir handle." In the `branchfilter` package block change `OlderThan   time.Duration` / `YoungerThan time.Duration` to `string` with the note "raw text, parsed by Compile".

- [ ] **Step 7: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/branch-filters && gofmt -l internal/branchfilter internal/archtest && go vet ./internal/branchfilter/ && git add internal/branchfilter internal/archtest docs/superpowers/specs/2026-09-16-branch-filters-design.md && git commit -q -m "feat(branchfilter): slot type, age parser, compiled matcher and Apply (stdlib leaf)" -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01BqyuQk8TCehWj3QfkHUrAM"
```

---

### Task 2: Config — `[[branches.filter]]` decode, overlay, settingDoc, block writers

**Files:**
- Create: `internal/config/branches.go`
- Create: `internal/config/branches_test.go`
- Modify: `internal/config/config.go` (Config struct ~line 204; `Load` ~line 245)
- Modify: `internal/config/template.go` (settingDocs, after the `notes` rows ~line 78)
- Read first: `internal/config/tools.go` (`overlayTools`, `AppendToolCommands`), `internal/config/write.go` (`sectionHeader` ~line 259, `atomicWriteFile` ~line 605, `RemoveThemeTable` for the header-walk shape), `internal/config/populate_test.go` (how the template tests assert a doc row).

**Interfaces:**
- Consumes: `branchfilter.Slot`, `branchfilter.MaxSlots`.
- Produces:
  ```go
  type BranchesConfig struct { Filter []branchfilter.Slot `toml:"filter"` }
  // Config.Branches BranchesConfig `toml:"branches"`
  func overlayBranchFilters(dst *BranchesConfig, src BranchesConfig)
  func SetBranchFilter(path string, s branchfilter.Slot) error      // replace the block with slot == s.Slot, else append
  func RemoveBranchFilter(path string, slot int) error              // delete that block; missing file/block = no-op
  ```

- [ ] **Step 1: Write the failing tests**

`internal/config/branches_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/branchfilter"
)

func writeCfg(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestBranchFiltersDecodeAndOverlayBySlot(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	global := writeCfg(t, dir, "global.toml", `
[[branches.filter]]
slot = 1
name = "stale"
older_than = "90d"

[[branches.filter]]
slot = 2
name = "feat"
mode = "show"
prefix = "feat/"
`)
	repo := writeCfg(t, dir, "repo.toml", `
[[branches.filter]]
slot = 2
name = "repo-feat"
prefix = "feature/"
`)
	cfg, err := Load(global, repo)
	if err != nil {
		t.Fatal(err)
	}
	all := branchfilter.CompileAll(cfg.Branches.Filter)
	if all[0].Name != "stale" || all[0].OlderThan != "90d" {
		t.Errorf("slot 1 should fall through from global: %+v", all[0].Slot)
	}
	if all[1].Name != "repo-feat" || all[1].Mode != branchfilter.ModeHide || all[1].Prefix != "feature/" {
		t.Errorf("slot 2 should be replaced WHOLE by the repo block: %+v", all[1].Slot)
	}
	if !all[2].Empty {
		t.Errorf("slot 3 should be empty")
	}
}

func TestBranchFiltersMissingSectionIsEmpty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := writeCfg(t, dir, "g.toml", "[ui]\nwheel_step = 2\n")
	cfg, err := Load(p, filepath.Join(dir, "absent.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Branches.Filter) != 0 {
		t.Errorf("Filter = %v; want none", cfg.Branches.Filter)
	}
}

func TestSetBranchFilterAppendsThenReplacesInPlace(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := writeCfg(t, dir, "repo.toml", "[ui]\nwheel_step = 2\n\n# trailing comment\n")
	if err := SetBranchFilter(p, branchfilter.Slot{Slot: 2, Name: "feat", Mode: "show", Prefix: "feat/"}); err != nil {
		t.Fatal(err)
	}
	if err := SetBranchFilter(p, branchfilter.Slot{Slot: 4, Name: "old", OlderThan: "90d", Regex: `^x"y$`}); err != nil {
		t.Fatal(err)
	}
	// Replace slot 2; slot 4 and the original lines must survive byte-for-byte.
	if err := SetBranchFilter(p, branchfilter.Slot{Slot: 2, Name: "feat2", Prefix: "feature/", Suffix: "-x"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(p)
	got := string(raw)
	if !strings.HasPrefix(got, "[ui]\nwheel_step = 2\n\n# trailing comment\n") {
		t.Errorf("original content not preserved:\n%s", got)
	}
	if strings.Count(got, "[[branches.filter]]") != 2 {
		t.Errorf("want exactly two blocks:\n%s", got)
	}
	if strings.Contains(got, `name = "feat"`) || !strings.Contains(got, `name = "feat2"`) {
		t.Errorf("slot 2 not replaced:\n%s", got)
	}
	if strings.Index(got, "slot = 2") > strings.Index(got, "slot = 4") {
		t.Errorf("replaced block moved:\n%s", got)
	}
	cfg, err := Load(p, filepath.Join(dir, "absent.toml"))
	if err != nil {
		t.Fatalf("written file must decode: %v\n%s", err, got)
	}
	all := branchfilter.CompileAll(cfg.Branches.Filter)
	if all[1].Prefix != "feature/" || all[1].Suffix != "-x" || all[1].Mode != branchfilter.ModeHide {
		t.Errorf("slot 2 = %+v", all[1].Slot)
	}
	if all[3].Regex != `^x"y$` || all[3].OlderThan != "90d" {
		t.Errorf("slot 4 = %+v (quoting broke?)", all[3].Slot)
	}
}

func TestSetBranchFilterRefusesEmptyPathAndBadSlot(t *testing.T) {
	t.Parallel()
	if err := SetBranchFilter("", branchfilter.Slot{Slot: 1, Prefix: "x"}); err == nil {
		t.Error("empty path accepted")
	}
	p := filepath.Join(t.TempDir(), "c.toml")
	if err := SetBranchFilter(p, branchfilter.Slot{Slot: 9, Prefix: "x"}); err == nil {
		t.Error("slot 9 accepted")
	}
	if _, err := os.Stat(p); err == nil {
		t.Error("a refused write created the file")
	}
}

func TestRemoveBranchFilter(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := writeCfg(t, dir, "repo.toml", "[ui]\nwheel_step = 2\n")
	_ = SetBranchFilter(p, branchfilter.Slot{Slot: 1, Prefix: "a"})
	_ = SetBranchFilter(p, branchfilter.Slot{Slot: 3, Prefix: "c"})
	if err := RemoveBranchFilter(p, 1); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(p)
	got := string(raw)
	if strings.Contains(got, "slot = 1") || !strings.Contains(got, "slot = 3") {
		t.Errorf("wrong block removed:\n%s", got)
	}
	if strings.Count(got, "[[branches.filter]]") != 1 {
		t.Errorf("want one block left:\n%s", got)
	}
	if strings.Contains(got, "\n\n\n") {
		t.Errorf("removal left a double blank line:\n%s", got)
	}
	if err := RemoveBranchFilter(p, 5); err != nil {
		t.Errorf("removing an absent slot must be a no-op, got %v", err)
	}
	if err := RemoveBranchFilter(filepath.Join(dir, "absent.toml"), 1); err != nil {
		t.Errorf("missing file must be a no-op, got %v", err)
	}
}

func TestBranchFilterSettingDocPresent(t *testing.T) {
	t.Parallel()
	tpl := Template()
	if !strings.Contains(tpl, "[branches]") || !strings.Contains(tpl, "[[branches.filter]]") {
		t.Errorf("template lacks the branch-filter doc row:\n%s", tpl)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/branch-filters && go test ./internal/config/ -run 'BranchFilter' 2>&1 | head`
Expected: compile errors (`cfg.Branches` undefined, `SetBranchFilter` undefined).

- [ ] **Step 3: Implement**

`internal/config/branches.go`:

```go
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/branchfilter"
)

// BranchesConfig is the [branches] section: the five branch-filter slots as
// [[branches.filter]] blocks. The section exists so `gg config populate` has
// a plain [branches] header to document the blocks under — a bare
// [branch_filters] table would collide with array-of-table blocks of the
// same name.
type BranchesConfig struct {
	Filter []branchfilter.Slot `toml:"filter"`
}

// overlayBranchFilters is the second deliberate exception to the field-level
// overlay rule (the first is [[tools.command]]): blocks are keyed by slot
// number, and a repo block REPLACES the global block for that slot whole —
// a filter is one rule, not a set of independently inheritable fields.
// Slots the repo file omits fall through from global. Duplicates within one
// file are left in place (CompileAll keeps the first, so hand-editing never
// silently flips a rule).
func overlayBranchFilters(dst *BranchesConfig, src BranchesConfig) {
	for _, s := range src.Filter {
		replaced := false
		for i, have := range dst.Filter {
			if have.Slot == s.Slot {
				dst.Filter[i] = s
				replaced = true
				break
			}
		}
		if !replaced {
			dst.Filter = append(dst.Filter, s)
		}
	}
}

const branchFilterHeader = "[[branches.filter]]"

// renderBranchFilter writes one block. Every field is emitted (even empty)
// so a later hand edit sees the whole vocabulary; strings use %q, which is
// valid TOML basic-string quoting for anything Go can print.
func renderBranchFilter(s branchfilter.Slot) []string {
	mode := s.Mode
	if mode == "" {
		mode = branchfilter.ModeHide
	}
	return []string{
		branchFilterHeader,
		"slot = " + strconv.Itoa(s.Slot),
		fmt.Sprintf("name = %q", s.Name),
		fmt.Sprintf("mode = %q", string(mode)),
		fmt.Sprintf("older_than = %q", strings.TrimSpace(s.OlderThan)),
		fmt.Sprintf("younger_than = %q", strings.TrimSpace(s.YoungerThan)),
		fmt.Sprintf("prefix = %q", s.Prefix),
		fmt.Sprintf("suffix = %q", s.Suffix),
		fmt.Sprintf("contains = %q", s.Contains),
		fmt.Sprintf("regex = %q", s.Regex),
	}
}

// branchFilterBlocks finds every ACTIVE [[branches.filter]] block in lines:
// [start, end) line spans (header included) and the slot number each carries
// (0 when the block has no parseable slot line). A commented header still
// ends the previous block, as everywhere else in this package.
func branchFilterBlocks(lines []string) (spans [][2]int, slots []int) {
	start := -1
	flush := func(end int) {
		if start < 0 {
			return
		}
		slot := 0
		for _, ln := range lines[start+1 : end] {
			trimmed := strings.TrimSpace(ln)
			if lineAssignsKey(trimmed, "slot") {
				_, v, _ := strings.Cut(trimmed, "=")
				slot, _ = strconv.Atoi(strings.TrimSpace(v))
			}
		}
		spans = append(spans, [2]int{start, end})
		slots = append(slots, slot)
		start = -1
	}
	for i, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if name, commented, ok := sectionHeader(trimmed); ok || strings.HasPrefix(trimmed, "[") {
			flush(i)
			if ok && !commented && name == branchFilterHeader {
				start = i
			}
		}
	}
	flush(len(lines))
	return spans, slots
}

// SetBranchFilter writes s into the config file at path: the block whose
// `slot = N` matches is replaced in place, otherwise a new block is appended.
// Everything else in the file is preserved byte-for-byte. The file is
// created when missing.
func SetBranchFilter(path string, s branchfilter.Slot) error {
	if path == "" {
		return fmt.Errorf("config: no config path; refusing to write")
	}
	if s.Slot < 1 || s.Slot > branchfilter.MaxSlots {
		return fmt.Errorf("config: branch filter slot must be 1..%d", branchfilter.MaxSlots)
	}
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var lines []string
	if len(raw) > 0 {
		lines = strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	}
	block := renderBranchFilter(s)
	spans, slots := branchFilterBlocks(lines)
	for i, span := range spans {
		if slots[i] != s.Slot {
			continue
		}
		// Keep any blank lines that trailed the old block: the span runs to
		// the next header, so trim trailing blanks off the body first.
		end := span[1]
		for end > span[0]+1 && strings.TrimSpace(lines[end-1]) == "" {
			end--
		}
		out := append([]string{}, lines[:span[0]]...)
		out = append(out, block...)
		out = append(out, lines[end:]...)
		return atomicWriteFile(path, []byte(strings.Join(out, "\n")+"\n"))
	}
	out := lines
	if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
		out = append(out, "")
	}
	out = append(out, block...)
	return atomicWriteFile(path, []byte(strings.Join(out, "\n")+"\n"))
}

// RemoveBranchFilter deletes the ACTIVE block for slot from the file at
// path, with the blank line that separated it from what precedes it. A
// missing file or absent block is a no-op.
func RemoveBranchFilter(path string, slot int) error {
	if path == "" {
		return fmt.Errorf("config: no config path; refusing to write")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	spans, slots := branchFilterBlocks(lines)
	for i, span := range spans {
		if slots[i] != slot {
			continue
		}
		start := span[0]
		for start > 0 && strings.TrimSpace(lines[start-1]) == "" {
			start--
		}
		out := append([]string{}, lines[:start]...)
		rest := lines[span[1]:]
		if len(out) > 0 && len(rest) > 0 && strings.TrimSpace(rest[0]) != "" {
			out = append(out, "")
		}
		out = append(out, rest...)
		if len(out) == 0 {
			return atomicWriteFile(path, []byte(""))
		}
		return atomicWriteFile(path, []byte(strings.Join(out, "\n")+"\n"))
	}
	return nil
}
```

In `internal/config/config.go`:
- Add `Branches BranchesConfig \`toml:"branches"\`` to `Config` after `Tools`.
- In `Load`, after `overlayTools(&cfg.Tools, layer.Tools)` add `overlayBranchFilters(&cfg.Branches, layer.Branches)`.

In `internal/config/template.go` `settingDocs`, after the `notes` rows add:

```go
	{"branches", "filter", nil, "branch filters as [[branches.filter]] blocks (alt+1…5 in the Branches/Remotes lists): slot (1..5), name, mode (hide | show = show only matching), older_than / younger_than (tip age: 90d 12w 6m 1y), prefix, suffix, contains, regex (Go RE2); set clauses AND together; a repo block REPLACES the global block for the same slot; invalid blocks are inert with a reason; edit from Settings → Branch filters… (TUI) or the web settings view"},
```

Check `sectionOrder()` in `populate.go` derives sections from `settingDocs` (it does), so `[branches]` appears in the template and in `gg config populate` output automatically. If `populate_test.go` has a golden list of sections, add `branches` after `notes`.

- [ ] **Step 4: Run the config tests**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/branch-filters && go test ./internal/config/ 2>&1 | tail -20`
Expected: all PASS, including the existing template/populate tests. If `TestTemplate…` golden tests fail on the new row, update the golden the way the failure shows (the row text is the contract).

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/branch-filters && gofmt -l internal/config && go vet ./internal/config/ && git add internal/config && git commit -q -m "feat(config): [[branches.filter]] slots — decode, overlay by slot, settingDoc, block writers" -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01BqyuQk8TCehWj3QfkHUrAM"
```

---

### Task 3: promptstate — the active slot per repo per list

**Files:**
- Create: `internal/promptstate/branchfilter.go`
- Create: `internal/promptstate/branchfilter_test.go`
- Modify: `internal/promptstate/file_store.go` (the `records` struct ~line 18, `read()` ~line 29)
- Modify: `internal/promptstate/store.go` (interface)
- Check: `grep -rn 'SuppressPrompt(' internal --include='*_test.go'` for fakes implementing `Store` and add the two methods to each (as of writing only `FileStore` implements it).

**Interfaces:**
- Produces:
  ```go
  const ( BranchFilterListBranches = "branches"; BranchFilterListRemotes = "remotes" )
  func (fs *FileStore) BranchFilterSlot(repoKey, list string) int          // 0 = none
  func (fs *FileStore) SetBranchFilterSlot(repoKey, list string, slot int) error
  // Store interface gains the same two methods.
  ```

- [ ] **Step 1: Write the failing test**

`internal/promptstate/branchfilter_test.go`:

```go
package promptstate

import (
	"path/filepath"
	"testing"
)

func TestBranchFilterSlotRoundTrip(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(filepath.Join(t.TempDir(), "prompts.toml"))
	if got := fs.BranchFilterSlot("/r/.git", BranchFilterListBranches); got != 0 {
		t.Fatalf("fresh store: %d", got)
	}
	if err := fs.SetBranchFilterSlot("/r/.git", BranchFilterListBranches, 2); err != nil {
		t.Fatal(err)
	}
	if err := fs.SetBranchFilterSlot("/r/.git", BranchFilterListRemotes, 5); err != nil {
		t.Fatal(err)
	}
	if err := fs.SetBranchFilterSlot("/other/.git", BranchFilterListBranches, 1); err != nil {
		t.Fatal(err)
	}
	if got := fs.BranchFilterSlot("/r/.git", BranchFilterListBranches); got != 2 {
		t.Errorf("branches = %d", got)
	}
	if got := fs.BranchFilterSlot("/r/.git", BranchFilterListRemotes); got != 5 {
		t.Errorf("remotes = %d", got)
	}
	if got := fs.BranchFilterSlot("/other/.git", BranchFilterListRemotes); got != 0 {
		t.Errorf("other remotes = %d", got)
	}
	// Clearing writes 0 and survives a reopen; sibling records survive too.
	if err := fs.SuppressPrompt("x"); err != nil {
		t.Fatal(err)
	}
	if err := fs.SetBranchFilterSlot("/r/.git", BranchFilterListBranches, 0); err != nil {
		t.Fatal(err)
	}
	fs2 := NewFileStore(fs.path)
	if got := fs2.BranchFilterSlot("/r/.git", BranchFilterListBranches); got != 0 {
		t.Errorf("cleared slot = %d", got)
	}
	if got := fs2.BranchFilterSlot("/r/.git", BranchFilterListRemotes); got != 5 {
		t.Errorf("remotes after clear = %d", got)
	}
	if !fs2.SuppressedPrompts()["x"] {
		t.Error("sibling record lost")
	}
	if err := fs.SetBranchFilterSlot("/r/.git", "tags", 1); err == nil {
		t.Error("unknown list accepted")
	}
	if err := fs.SetBranchFilterSlot("/r/.git", BranchFilterListBranches, 6); err == nil {
		t.Error("slot 6 accepted")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/branch-filters && go test ./internal/promptstate/ -run BranchFilter 2>&1 | head -5`
Expected: undefined `BranchFilterSlot`.

- [ ] **Step 3: Implement**

`internal/promptstate/branchfilter.go`:

```go
package promptstate

import "fmt"

// The two lists a branch filter applies to. Wire names shared with the web
// client and the TUI panels; anything else is refused at the store.
const (
	BranchFilterListBranches = "branches"
	BranchFilterListRemotes  = "remotes"
)

// branchFilterRecord is one repo's active slots: 0 = none. Per repo (the
// git common dir), per list — which branches you want out of the way is a
// property of the repository, unlike the sidebar layout in WebUI.
type branchFilterRecord struct {
	Branches int `toml:"branches"`
	Remotes  int `toml:"remotes"`
}

// BranchFilterSlot returns the active slot for repoKey's list (0 = none).
func (fs *FileStore) BranchFilterSlot(repoKey, list string) int {
	rec, ok := fs.read().BranchFilter[repoKey]
	if !ok {
		return 0
	}
	switch list {
	case BranchFilterListBranches:
		return rec.Branches
	case BranchFilterListRemotes:
		return rec.Remotes
	}
	return 0
}

// SetBranchFilterSlot persists slot (0..5) for repoKey's list.
func (fs *FileStore) SetBranchFilterSlot(repoKey, list string, slot int) error {
	if slot < 0 || slot > 5 {
		return fmt.Errorf("promptstate: branch filter slot %d out of range 0..5", slot)
	}
	r := fs.read()
	rec := r.BranchFilter[repoKey]
	switch list {
	case BranchFilterListBranches:
		rec.Branches = slot
	case BranchFilterListRemotes:
		rec.Remotes = slot
	default:
		return fmt.Errorf("promptstate: unknown branch filter list %q", list)
	}
	r.BranchFilter[repoKey] = rec
	return fs.write(r)
}
```

In `file_store.go`:
- add to `records`: `BranchFilter map[string]branchFilterRecord \`toml:"branch_filter,omitempty"\``
- in `read()`: initialise `BranchFilter: map[string]branchFilterRecord{}` in `empty`, and after the nil checks add `if r.BranchFilter == nil { r.BranchFilter = map[string]branchFilterRecord{} }`.

In `store.go` add to the interface:

```go
	// BranchFilterSlot returns repoKey's active branch-filter slot for list
	// ("branches" | "remotes"); 0 = none.
	BranchFilterSlot(repoKey, list string) int
	// SetBranchFilterSlot persists it (0 clears).
	SetBranchFilterSlot(repoKey, list string, slot int) error
```

Update the package doc comment's list of what the file holds (add "the active branch-filter slot per repo and list").

- [ ] **Step 4: Run**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/branch-filters && go test ./internal/promptstate/ ./internal/tui/ -run 'BranchFilter|Prompt' 2>&1 | tail -5 && go build ./...`
Expected: PASS and a clean build (any `Store` fake that no longer satisfies the interface fails the build — add the two methods to it).

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/branch-filters && gofmt -l internal/promptstate && git add internal/promptstate && git commit -q -m "feat(promptstate): remember the active branch-filter slot per repo and list" -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01BqyuQk8TCehWj3QfkHUrAM"
```

---

### Task 4: Domain — compiled slots and exemptions

**Files:**
- Create: `internal/domain/branchfilter.go`
- Create: `internal/domain/branchfilter_test.go`
- Read first: `internal/domain/effective_config.go`, `internal/domain/query_test.go` (how a test repo + Service are built: `newTestRepo` / `Open`).

**Interfaces:**
- Produces:
  ```go
  func (s *Service) BranchFilters(ctx context.Context) ([branchfilter.MaxSlots]branchfilter.Compiled, error) // from EffectiveConfig
  func ExemptBranches(bs []model.Branch, wts []model.Worktree) []bool        // HEAD or checked out in any worktree
  func ExemptRemoteBranches(rbs []model.RemoteBranch, bs []model.Branch) []bool // HEAD's upstream
  func BranchRows(bs []model.Branch) []branchfilter.Row                       // Name, UnixTime
  func RemoteBranchRows(rbs []model.RemoteBranch) []branchfilter.Row          // Branch (the part after the remote), UnixTime
  ```

- [ ] **Step 1: Write the failing tests**

`internal/domain/branchfilter_test.go`:

```go
package domain

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/branchfilter"
	"github.com/homeend/gigagit/internal/model"
)

func TestExemptBranches(t *testing.T) {
	t.Parallel()
	bs := []model.Branch{{Name: "main", IsHead: true}, {Name: "feat/a"}, {Name: "feat/b"}}
	wts := []model.Worktree{{Path: "/r", Branch: "main"}, {Path: "/r-b", Branch: "feat/b"}}
	got := ExemptBranches(bs, wts)
	want := []bool{true, false, true}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d (%s) exempt = %v; want %v", i, bs[i].Name, got[i], want[i])
		}
	}
}

func TestExemptRemoteBranches(t *testing.T) {
	t.Parallel()
	bs := []model.Branch{{Name: "main", IsHead: true, Upstream: "origin/main"}, {Name: "x", Upstream: "origin/x"}}
	rbs := []model.RemoteBranch{{Name: "origin/x", Branch: "x"}, {Name: "origin/main", Branch: "main"}}
	got := ExemptRemoteBranches(rbs, bs)
	if got[0] || !got[1] {
		t.Errorf("exempt = %v; want [false true]", got)
	}
	// Detached HEAD / no upstream: nothing exempt.
	if got := ExemptRemoteBranches(rbs, []model.Branch{{Name: "main", IsHead: true}}); got[0] || got[1] {
		t.Errorf("no upstream: %v", got)
	}
}

func TestRemoteBranchRowsUseBranchPart(t *testing.T) {
	t.Parallel()
	rows := RemoteBranchRows([]model.RemoteBranch{{Name: "origin/feat/x", Branch: "feat/x", UnixTime: 7}})
	if rows[0].Name != "feat/x" || rows[0].UnixTime != 7 {
		t.Errorf("rows = %+v", rows)
	}
}

func TestBranchFiltersFromEffectiveConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // keep the real global file out
	dir := newRepoDir(t)                     // whatever this package's real-git repo helper is called
	if err := os.WriteFile(filepath.Join(dir, ".gg.toml"), []byte("[[branches.filter]]\nslot = 3\nname = \"wip\"\nsuffix = \"-wip\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := Open(dir)
	all, err := svc.BranchFilters(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if all[2].Name != "wip" || !all[2].Usable() || all[2].Mode != branchfilter.ModeHide {
		t.Errorf("slot 3 = %+v err=%v", all[2].Slot, all[2].Err)
	}
	if !all[0].Empty {
		t.Errorf("slot 1 should be empty")
	}
}
```

Replace `newRepoDir(t)` with this package's actual helper (grep `func newTestRepo\|func newRepoDir\|func initRepo` in `internal/domain/*_test.go`) and `Open(dir)` with how those tests construct a `*Service`.

- [ ] **Step 2: Run to verify failure**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/branch-filters && go test ./internal/domain/ -run 'Exempt|BranchFilters|RemoteBranchRows' 2>&1 | head -5`
Expected: undefined symbols.

- [ ] **Step 3: Implement**

`internal/domain/branchfilter.go`:

```go
package domain

// Branch filters: the five [[branches.filter]] slots, compiled once from the
// effective config, plus the exemption rules both frontends share. The
// ACTIVE slot is the frontends' business (promptstate, per repo per list):
// domain has no state-dir handle and evaluation is pure over what the
// caller already holds.

import (
	"context"

	"github.com/homeend/gigagit/internal/branchfilter"
	"github.com/homeend/gigagit/internal/model"
)

// BranchFilters returns the five compiled slots (index = slot-1) for this
// repo's effective config. An invalid block is inert inside its Compiled
// (Err set), never an error here.
func (s *Service) BranchFilters(ctx context.Context) ([branchfilter.MaxSlots]branchfilter.Compiled, error) {
	cfg, err := s.EffectiveConfig(ctx)
	if err != nil {
		return [branchfilter.MaxSlots]branchfilter.Compiled{}, err
	}
	return branchfilter.CompileAll(cfg.Branches.Filter), nil
}

// ExemptBranches marks the rows a filter may never hide: HEAD, and any
// branch checked out in a worktree (switching to it would strand the user
// on an invisible row).
func ExemptBranches(bs []model.Branch, wts []model.Worktree) []bool {
	checked := make(map[string]bool, len(wts))
	for _, w := range wts {
		if w.Branch != "" {
			checked[w.Branch] = true
		}
	}
	out := make([]bool, len(bs))
	for i, b := range bs {
		out[i] = b.IsHead || checked[b.Name]
	}
	return out
}

// ExemptRemoteBranches marks the current branch's upstream row (the one the
// Remotes tab's f/find and pull land on). Detached HEAD or no upstream:
// nothing is exempt.
func ExemptRemoteBranches(rbs []model.RemoteBranch, bs []model.Branch) []bool {
	upstream := ""
	for _, b := range bs {
		if b.IsHead {
			upstream = b.Upstream
			break
		}
	}
	out := make([]bool, len(rbs))
	if upstream == "" {
		return out
	}
	for i, rb := range rbs {
		out[i] = rb.Name == upstream
	}
	return out
}

// BranchRows adapts local branches for branchfilter.Apply.
func BranchRows(bs []model.Branch) []branchfilter.Row {
	rows := make([]branchfilter.Row, len(bs))
	for i, b := range bs {
		rows[i] = branchfilter.Row{Name: b.Name, UnixTime: b.UnixTime}
	}
	return rows
}

// RemoteBranchRows adapts remote branches: name rules see the BRANCH part
// ("feat/x"), never the remote prefix.
func RemoteBranchRows(rbs []model.RemoteBranch) []branchfilter.Row {
	rows := make([]branchfilter.Row, len(rbs))
	for i, rb := range rbs {
		rows[i] = branchfilter.Row{Name: rb.Branch, UnixTime: rb.UnixTime}
	}
	return rows
}
```

- [ ] **Step 4: Run**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/branch-filters && go test ./internal/domain/ -run 'Exempt|BranchFilters|RemoteBranchRows' 2>&1 | tail -5 && go test ./internal/archtest/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/branch-filters && gofmt -l internal/domain && git add internal/domain/branchfilter.go internal/domain/branchfilter_test.go && git commit -q -m "feat(domain): compiled branch-filter slots from the effective config + shared exemption rules" -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01BqyuQk8TCehWj3QfkHUrAM"
```

---

### Task 5: TUI — alt+N, filtering in `displayIndices`, header, footer, help

**Files:**
- Create: `internal/tui/branch_filter.go`
- Create: `internal/tui/branch_filter_test.go`
- Modify: `internal/tui/model.go` (fields ~line 48–93 and ~269; constructor ~line 340–360 where `filterMemo: &commitFilterMemo{}` sits; the two `m.cfg = msg.cfg` sites ~1147 and ~1221; the reroot reset ~3955; the key switch near `case "o":` ~2318)
- Modify: `internal/tui/viewstate.go` (`displayIndices` ~line 511; `panelLabel` ~line 690)
- Modify: `internal/tui/footer.go` (add a binding next to `{"order", "o", …}` ~line 162)
- Modify: `internal/tui/help.go` (a row after `r("o", …)` ~line 42)
- Modify: `internal/i18n/lang/{ja,ko,zh,ru}.toml` (every new `i18n.T` key; follow the `adding-translations` skill)
- Read first: `internal/tui/filter_memo.go`, `internal/tui/find_current.go`, `internal/tui/source_test.go` (`newTestModel`), `internal/tui/footer_test.go` (how footer strings are asserted), `internal/tui/tool_run.go` (`toolRepoKey`).

**Interfaces:**
- Consumes: `domain.BranchFilters`, `domain.ExemptBranches`, `domain.ExemptRemoteBranches`, `domain.BranchRows`, `domain.RemoteBranchRows`, `branchfilter.Apply`, `promptstate.Store.BranchFilterSlot/SetBranchFilterSlot`.
- Produces (model state):
  ```go
  // Model fields
  branchFilters     [branchfilter.MaxSlots]branchfilter.Compiled // compiled from m.cfg; zero = all empty
  branchFilterSlot  map[panel]int                                // panelBranches / panelRemotes → active slot, 0 = none
  bfMemo            *branchFilterMemos                           // shared pointer, like filterMemo
  // helpers in branch_filter.go
  func (m Model) branchFilterHidden(p panel) (hidden []bool, exempt []bool, count int)  // memoised verdicts for p; nil when no active usable slot
  func (m Model) toggleBranchFilter(slot int) Model                                      // alt+N on the focused panel
  func (m Model) branchFilterDecoration(p panel) string                                  // " ▽2 stale · 12 hidden" or ""
  func (m Model) loadBranchFilterSlots() Model                                           // read promptstate into branchFilterSlot (startup/reroot)
  func branchFilterListName(p panel) string                                              // "branches" | "remotes" | ""
  ```

- [ ] **Step 1: Write the failing tests**

`internal/tui/branch_filter_test.go`:

```go
package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/branchfilter"
	"github.com/homeend/gigagit/internal/model"
)

func altKey(digit rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{digit}, Alt: true}
}

func bfModel(t *testing.T) Model {
	t.Helper()
	m := newTestModel(t)
	now := time.Now().Unix()
	m.branches = []model.Branch{
		{Name: "main", IsHead: true, UnixTime: now},
		{Name: "feat/a", UnixTime: now},
		{Name: "feat/old", UnixTime: now - 200*24*3600},
		{Name: "fix/b", UnixTime: now},
		{Name: "feat/wt", UnixTime: now - 200*24*3600},
	}
	m.worktrees = []model.Worktree{{Path: "/r", Branch: "main"}, {Path: "/r2", Branch: "feat/wt"}}
	m.remoteBranches = []model.RemoteBranch{
		{Name: "origin/main", Remote: "origin", Branch: "main", UnixTime: now},
		{Name: "origin/feat/a", Remote: "origin", Branch: "feat/a", UnixTime: now},
	}
	m.branches[0].Upstream = "origin/main"
	m.branchFilters = branchfilter.CompileAll([]branchfilter.Slot{
		{Slot: 1, Name: "feat", Prefix: "feat/"},
		{Slot: 2, Name: "stale", OlderThan: "90d"},
		{Slot: 3, Name: "only-fix", Mode: branchfilter.ModeShow, Prefix: "fix/"},
		{Slot: 4, Regex: "("}, // inert
	})
	m.focus = panelBranches
	m.sortModes[panelBranches] = sortDefault
	return m
}

func names(m Model, p panel) []string {
	_, idx := m.panelView(p)
	l := m.listFor(p)
	out := make([]string, len(idx))
	for i, b := range idx {
		out[i] = l.(interface{ Name(int) string }).Name(b)
	}
	return out
}

func TestAltDigitSelectsSlotAndHidesRows(t *testing.T) {
	t.Parallel()
	m := bfModel(t)
	mm, _ := m.Update(altKey('1'))
	m = mm.(Model)
	got := strings.Join(names(m, panelBranches), ",")
	// feat/a and feat/old hidden; main (HEAD) and feat/wt (worktree) exempt.
	if got != "main,fix/b,feat/wt" {
		t.Errorf("slot 1: %s", got)
	}
	if m.branchFilterSlot[panelBranches] != 1 {
		t.Errorf("active = %d", m.branchFilterSlot[panelBranches])
	}
	if d := m.branchFilterDecoration(panelBranches); d != " ▽1 feat · 2 hidden" {
		t.Errorf("decoration = %q", d)
	}
}

func TestAltSameDigitClearsAndOtherDigitSwitches(t *testing.T) {
	t.Parallel()
	m := bfModel(t)
	mm, _ := m.Update(altKey('2'))
	m = mm.(Model)
	if got := strings.Join(names(m, panelBranches), ","); got != "main,feat/a,fix/b,feat/wt" {
		t.Errorf("slot 2: %s", got)
	}
	mm, _ = m.Update(altKey('3')) // radio: switches, does not stack
	m = mm.(Model)
	if got := strings.Join(names(m, panelBranches), ","); got != "main,fix/b,feat/wt" {
		t.Errorf("slot 3 (show only fix/): %s", got)
	}
	mm, _ = m.Update(altKey('3'))
	m = mm.(Model)
	if m.branchFilterSlot[panelBranches] != 0 || len(names(m, panelBranches)) != 5 {
		t.Errorf("same key should clear: slot=%d rows=%v", m.branchFilterSlot[panelBranches], names(m, panelBranches))
	}
	if m.branchFilterDecoration(panelBranches) != "" {
		t.Errorf("decoration after clear = %q", m.branchFilterDecoration(panelBranches))
	}
}

func TestAltDigitInertSlotRefusedWithReason(t *testing.T) {
	t.Parallel()
	m := bfModel(t)
	mm, _ := m.Update(altKey('4'))
	m = mm.(Model)
	if m.branchFilterSlot[panelBranches] != 0 {
		t.Errorf("inert slot activated")
	}
	if !strings.Contains(m.statusMsg, "slot 4") || !strings.Contains(m.statusMsg, "regex") {
		t.Errorf("status = %q", m.statusMsg)
	}
	mm, _ = m.Update(altKey('5')) // empty slot
	m = mm.(Model)
	if m.branchFilterSlot[panelBranches] != 0 || !strings.Contains(m.statusMsg, "slot 5") {
		t.Errorf("empty slot: active=%d status=%q", m.branchFilterSlot[panelBranches], m.statusMsg)
	}
}

func TestAltDigitOnOtherPanelExplains(t *testing.T) {
	t.Parallel()
	m := bfModel(t)
	m.focus = panelCommits
	mm, _ := m.Update(altKey('1'))
	m = mm.(Model)
	if m.branchFilterSlot[panelBranches] != 0 || m.branchFilterSlot[panelRemotes] != 0 {
		t.Error("a filter was activated from the Commits panel")
	}
	if !strings.Contains(m.statusMsg, "Branches") {
		t.Errorf("status = %q", m.statusMsg)
	}
}

func TestRemotesSlotIsIndependentAndUpstreamExempt(t *testing.T) {
	t.Parallel()
	m := bfModel(t)
	m.focus = panelRemotes
	m.activeLeftTab = panelRemotes
	mm, _ := m.Update(altKey('1'))
	m = mm.(Model)
	if m.branchFilterSlot[panelBranches] != 0 || m.branchFilterSlot[panelRemotes] != 1 {
		t.Errorf("slots = %v", m.branchFilterSlot)
	}
	if got := strings.Join(names(m, panelRemotes), ","); got != "origin/main" {
		t.Errorf("remotes under slot 1: %s (origin/feat/a hidden, origin/main is HEAD's upstream)", got)
	}
	if !strings.Contains(m.branchFilterDecoration(panelRemotes), "1 hidden") {
		t.Errorf("decoration = %q", m.branchFilterDecoration(panelRemotes))
	}
}

func TestSlashFilterStacksOnBranchFilter(t *testing.T) {
	t.Parallel()
	m := bfModel(t)
	m.branchFilterSlot[panelBranches] = 3 // show only fix/ (+ exempt)
	m.filterPanel = panelBranches
	m.filterQuery = "wt"
	if got := strings.Join(names(m, panelBranches), ","); got != "feat/wt" {
		t.Errorf("stacked: %s", got)
	}
}

func TestBranchFilterMemoTracksListChanges(t *testing.T) {
	t.Parallel()
	m := bfModel(t)
	m.branchFilterSlot[panelBranches] = 1
	if n := len(names(m, panelBranches)); n != 3 {
		t.Fatalf("before: %d rows", n)
	}
	m.branches = append(m.branches, model.Branch{Name: "feat/new", UnixTime: time.Now().Unix()})
	if got := strings.Join(names(m, panelBranches), ","); got != "main,fix/b,feat/wt" {
		t.Errorf("after append: %s", got)
	}
	// A refresh always assigns a NEW slice; a same-length replacement with a
	// different middle row must re-evaluate (the memo keys on slice identity).
	fresh := append([]model.Branch(nil), m.branches...)
	fresh[1].Name = "zzz/renamed"
	m.branches = fresh
	if got := strings.Join(names(m, panelBranches), ","); !strings.Contains(got, "zzz/renamed") {
		t.Errorf("after replacement: %s", got)
	}
}

func TestFooterAdvertisesBranchFilterKey(t *testing.T) {
	t.Parallel()
	m := bfModel(t)
	if !strings.Contains(m.footerText(), "[alt+1-5] filter") {
		t.Errorf("footer = %q", m.footerText())
	}
	m.focus = panelCommits
	if strings.Contains(m.footerText(), "[alt+1-5]") {
		t.Errorf("footer on Commits should not advertise it: %q", m.footerText())
	}
}
```

If the footer is rendered by a differently named method, use the one `footer_test.go` uses. If `names()`'s `Name(int)` assertion fails to compile for a list type, use the exported-in-package `panelList` capability the codebase has (`branchList.Name`, `remoteBranchList.Name` both exist).

- [ ] **Step 2: Run to verify failure**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/branch-filters && go test ./internal/tui/ -run 'AltDigit|RemotesSlot|SlashFilterStacks|BranchFilterMemo|FooterAdvertisesBranchFilter' 2>&1 | head -5`
Expected: undefined fields/methods.

- [ ] **Step 3: Implement `branch_filter.go`**

```go
package tui

import (
	"strconv"
	"time"

	"github.com/homeend/gigagit/internal/branchfilter"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/promptstate"
)

// Branch filters (alt+1…5): the five [[branches.filter]] slots applied to the
// Branches and Remotes panels, one active slot per panel (radio), remembered
// per repo in prompts.toml. Evaluation runs inside displayIndices BEFORE the
// / text filter, so / stacks on top; verdicts are memoised because
// displayIndices fires many times per keystroke.

// branchFilterMemo caches the verdicts for one panel. Shared pointer across
// Model copies (the commitFilterMemo discipline). Self-validating key: the
// slot, the list length, and the IDENTITY of the backing slice (a pointer to
// its first element). Every refresh path assigns a NEW slice (m.branches =
// msg.branches; sortRemoteBranchesLocalFirst returns a fresh one), so the
// pointer changes whenever the data can; nothing mutates a row in place.
type branchFilterMemo struct {
	slot           int
	n              int
	head           any // *model.Branch or *model.RemoteBranch of element 0
	hidden, exempt []bool
	count          int
}

// branchFilterListName maps a panel to its promptstate list name.
func branchFilterListName(p panel) string {
	switch p {
	case panelBranches:
		return promptstate.BranchFilterListBranches
	case panelRemotes:
		return promptstate.BranchFilterListRemotes
	}
	return ""
}

// activeBranchFilter returns the usable Compiled slot active on p, or nil.
func (m Model) activeBranchFilter(p panel) *branchfilter.Compiled {
	slot := m.branchFilterSlot[p]
	if slot < 1 || slot > branchfilter.MaxSlots {
		return nil
	}
	c := m.branchFilters[slot-1]
	if !c.Usable() {
		return nil
	}
	return &c
}

// branchFilterHidden returns per-backing-index verdicts for p under its
// active slot (nil, nil, 0 when none). Memoised per panel.
func (m Model) branchFilterHidden(p panel) (hidden, exempt []bool, count int) {
	c := m.activeBranchFilter(p)
	if c == nil {
		return nil, nil, 0
	}
	var head any
	var n int
	switch p {
	case panelBranches:
		n = len(m.branches)
		if n > 0 {
			head = &m.branches[0]
		}
	case panelRemotes:
		n = len(m.remoteBranches)
		if n > 0 {
			head = &m.remoteBranches[0]
		}
	default:
		return nil, nil, 0
	}
	memo := m.bfMemoFor(p)
	if memo != nil && n > 0 && memo.slot == c.Slot.Slot && memo.n == n && memo.head == head {
		return memo.hidden, memo.exempt, memo.count
	}
	var rows []branchfilter.Row
	var ex []bool
	if p == panelBranches {
		rows = domain.BranchRows(m.branches)
		ex = domain.ExemptBranches(m.branches, m.worktrees)
	} else {
		rows = domain.RemoteBranchRows(m.remoteBranches)
		ex = domain.ExemptRemoteBranches(m.remoteBranches, m.branches)
	}
	v, cnt := branchfilter.Apply(*c, rows, ex, time.Now())
	hidden = make([]bool, len(v))
	exempt = make([]bool, len(v))
	for i, vv := range v {
		hidden[i], exempt[i] = vv.Hidden, vv.Exempt
	}
	if memo != nil && n > 0 {
		*memo = branchFilterMemo{slot: c.Slot.Slot, n: n, head: head, hidden: hidden, exempt: exempt, count: cnt}
	}
	return hidden, exempt, cnt
}

// bfMemoFor returns the panel's memo pointer (nil in zero-value test Models).
func (m Model) bfMemoFor(p panel) *branchFilterMemo {
	if m.bfMemo == nil {
		return nil
	}
	switch p {
	case panelBranches:
		return &m.bfMemo.branches
	case panelRemotes:
		return &m.bfMemo.remotes
	}
	return nil
}

// branchFilterMemos holds one memo per filtered panel behind one pointer.
type branchFilterMemos struct{ branches, remotes branchFilterMemo }

// toggleBranchFilter is alt+N: on Branches/Remotes it activates slot N, or
// clears it when N is already active (radio). Elsewhere it explains itself.
// An inert or empty slot is refused with its reason and the active slot is
// left alone. The choice is persisted per repo; a failed write still applies
// for the session.
func (m Model) toggleBranchFilter(slot int) Model {
	p := m.focus
	list := branchFilterListName(p)
	if list == "" {
		m.statusMsg = i18n.T("alt+1…5 filter the Branches or Remotes panel — focus one first")
		return m
	}
	next := slot
	if m.branchFilterSlot[p] == slot {
		next = 0
	} else {
		c := m.branchFilters[slot-1]
		if !c.Usable() {
			m.statusMsg = i18n.T("slot %d: %s", slot, c.Summary())
			return m
		}
	}
	if m.branchFilterSlot == nil {
		m.branchFilterSlot = map[panel]int{}
	}
	m.branchFilterSlot[p] = next
	if n := m.panelLen(p); m.sel[p] >= n && n > 0 {
		m.sel[p] = n - 1
	}
	if next == 0 {
		m.statusMsg = i18n.T("branch filter off")
	} else {
		m.statusMsg = i18n.T("branch filter %d: %s", next, m.branchFilters[next-1].Label())
	}
	if m.promptStore != nil {
		if err := m.promptStore.SetBranchFilterSlot(m.toolRepoKey(), list, next); err != nil {
			m.statusMsg += " " + i18n.T("(not remembered: %s)", err.Error())
		}
	}
	return m
}

// loadBranchFilterSlots reads the remembered slots for this repo. A slot
// that no longer exists or is unusable loads as none (nothing is rewritten).
func (m Model) loadBranchFilterSlots() Model {
	if m.branchFilterSlot == nil {
		m.branchFilterSlot = map[panel]int{}
	}
	if m.promptStore == nil {
		return m
	}
	key := m.toolRepoKey()
	for _, p := range []panel{panelBranches, panelRemotes} {
		s := m.promptStore.BranchFilterSlot(key, branchFilterListName(p))
		if s < 1 || s > branchfilter.MaxSlots || !m.branchFilters[s-1].Usable() {
			s = 0
		}
		m.branchFilterSlot[p] = s
	}
	return m
}

// branchFilterDecoration is the panel-header suffix: " ▽2 stale · 12 hidden",
// or " ▽2 stale" when nothing is hidden, or "" with no active slot.
func (m Model) branchFilterDecoration(p panel) string {
	c := m.activeBranchFilter(p)
	if c == nil {
		return ""
	}
	_, _, n := m.branchFilterHidden(p)
	s := " ▽" + strconv.Itoa(c.Slot.Slot) + " " + c.Label()
	if n > 0 {
		s += " · " + i18n.T("%d hidden", n)
	}
	return s
}

// branchFilterExemptMark is the dim marker appended to an exempt row's name
// (HEAD / checked out / upstream that the rule would otherwise hide).
const branchFilterExemptMark = " ∗"
```

Wiring:

1. `model.go` fields (next to `sortModes`): `branchFilters [branchfilter.MaxSlots]branchfilter.Compiled`, `branchFilterSlot map[panel]int`, `bfMemo *branchFilterMemos`. In the constructor (where `filterMemo: &commitFilterMemo{}` is set) add `branchFilterSlot: map[panel]int{}, bfMemo: &branchFilterMemos{}`. At the reroot reset (~3955, `m.filterMemo = &commitFilterMemo{}`) add `m.bfMemo = &branchFilterMemos{}` and `m.branchFilterSlot = map[panel]int{}`.
2. Both `m.cfg = msg.cfg` sites (~1147 startup, ~1221 reroot): immediately after, `m.branchFilters = branchfilter.CompileAll(m.cfg.Branches.Filter)` then `m = m.loadBranchFilterSlots()`. Search `grep -n 'm.cfg = ' internal/tui/*.go` and do the same at every other assignment (`settings_tools.go:90` included). Note `toolRepoKey()` needs `m.repoHealth.GitCommonDir`, which the health probe fills; if it is still empty at the startup site, `loadBranchFilterSlots` also runs where `m.repoHealth` is assigned (grep `m.repoHealth =`), guarded so it only runs when `branchFilterSlot` is empty.
3. Key dispatch: in the same `switch msg.String()` that has `case "o":`, add
   ```go
   case "alt+1", "alt+2", "alt+3", "alt+4", "alt+5":
       if !m.running && !m.loading {
           n, _ := strconv.Atoi(msg.String()[4:])
           m = m.toggleBranchFilter(n)
       }
       return m, nil
   ```
   Confirm no popup/layer intercepts alt+digit first: the key must only reach this switch when no layer is open (that is already how `o` behaves).
4. `viewstate.go` `displayIndices`: after `q := ""…` and before the loop, `hidden, _, _ := m.branchFilterHidden(p)`; inside the loop, right after the `memberOf` check: `if hidden != nil && i < len(hidden) && hidden[i] { continue }`. Keep the fast paths above it untouched (they only cover file panels).
5. `viewstate.go` `panelLabel` (~line 696): after the sort-mode suffix add `base += m.branchFilterDecoration(p)`.
6. Exempt marker: in `branchRows()` / `remoteRows()` (grep `func (m Model) branchRows`), append `branchFilterExemptMark` to a row whose exempt verdict is set: `_, exempt, _ := m.branchFilterHidden(panelBranches)` once before the loop. Render it with the dim style the rows already use for secondary text.
7. `footer.go`: next to `{"order", "o", …}` add `{"branch-filter", "alt+1", i18n.T("[alt+1-5] filter"), func(m Model) bool { return (m.focus == panelBranches || m.focus == panelRemotes) && m.opsIdle() }, scopeGlobal}`. If `TestHelpFooterCoverage` wants each footer key in help, the help row below satisfies it (key text `"alt+1…5"` → check what the drift guard compares and match it).
8. `help.go`: after the `o` row: `r("alt+1…5", i18n.T("branch filter slots on the Branches/Remotes panel: activate slot N (hide, or show only, by tip age / prefix / suffix / substring / regex — defined in Settings , → Branch filters… or [[branches.filter]] in .gg.toml); the same key again clears; one slot per panel; remembered per repo. HEAD, checked-out and upstream rows are never hidden (∗ marks one the rule would hide)")),`.
9. Every new `i18n.T` literal → all four bundles.

- [ ] **Step 4: Run the TUI tests**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/branch-filters && go test ./internal/tui/ 2>&1 | tail -20`
Expected: PASS, including `TestHelpFooterCoverage`, the i18n scan gates and `TestMenuLabels…`. Fix any bundle key the scan reports.

- [ ] **Step 5: Headless proof that alt+1 arrives**

Run (per the `driving-tui-headless` skill): `cd /mnt/t/others/gigagit/.claude/worktrees/branch-filters && go build -o /tmp/claude-1000/gg-bf ./cmd/gg && ./tui-capture.sh "M-1"` against a repo with a `[[branches.filter]]` slot 1 in its `.gg.toml`, and confirm the Branches header reads `[Branches] … ▽1 …`. If tmux delivers ESC then `1` separately, set `escape-time 0` in the capture script's tmux session and re-run; record the outcome in the task's commit message.

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/branch-filters && gofmt -l internal/tui && go vet ./internal/tui/ && git add internal/tui internal/i18n && git commit -q -m "feat(tui): alt+1…5 branch-filter slots on the Branches and Remotes panels (radio, exempt rows, header decoration, footer + help)" -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01BqyuQk8TCehWj3QfkHUrAM"
```

---

### Task 6: TUI — Settings → Branch filters… (browse + edit form + writers)

**Files:**
- Create: `internal/tui/branch_filter_popup.go`
- Create: `internal/tui/branch_filter_popup_test.go`
- Modify: `internal/tui/settings_popup.go` (const + `settingsMenu` order after `settingsMenuPrefixes`; `settingsMenuTitle` case; the enter switch ~line 486)
- Modify: `internal/tui/branch_filter.go` (reload hook after save)
- Modify: `internal/i18n/lang/*.toml`
- Read first: `internal/tui/prefix_settings.go` (browse/form pattern, `viewField`, `popupBox`, `renderWindow`), `internal/tui/prefix_settings_test.go` (how a popup is driven in tests), `internal/tui/hook_editor.go:50` (repoConfigPath guard).

**Interfaces:**
- Consumes: `config.SetBranchFilter`, `config.RemoveBranchFilter`, `config.Load`, `config.DefaultGlobalPath`, `m.repoConfigPath`, `branchfilter.Compile`.
- Produces: `func (m Model) openBranchFilterSettings() (Model, tea.Cmd)`; `type branchFilterView struct` (a layer pushed with `pushLayer`); `func (m Model) reloadConfigAfterBranchFilterWrite() Model`.

- [ ] **Step 1: Write the failing tests**

`internal/tui/branch_filter_popup_test.go`:

```go
package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/branchfilter"
)

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func typeText(v *branchFilterView, m Model, s string) Model {
	for _, r := range s {
		m, _ = v.update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func bfPopupModel(t *testing.T) (Model, *branchFilterView, string) {
	t.Helper()
	m := bfModel(t)
	dir := t.TempDir()
	m.repoConfigPath = filepath.Join(dir, ".gg.toml")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "cfg")) // global path → temp
	m, _ = m.openBranchFilterSettings()
	v, ok := m.topLayer().(*branchFilterView)
	if !ok {
		t.Fatalf("top layer = %T", m.topLayer())
	}
	return m, v, m.repoConfigPath
}

func TestBranchFilterPopupListsFiveSlots(t *testing.T) {
	m, v, _ := bfPopupModel(t)
	out := v.box(m)
	for _, want := range []string{"1  feat", "hide · prefix feat/", "2  stale", "3  only-fix", "show only", "4", "invalid", "5", "—"} {
		if !strings.Contains(out, want) {
			t.Errorf("popup lacks %q:\n%s", want, out)
		}
	}
}

func TestBranchFilterPopupEditSavesRepoBlockAndReloads(t *testing.T) {
	m, v, repoPath := bfPopupModel(t)
	// Move to slot 5 (empty) and open the form.
	for i := 0; i < 4; i++ {
		m, _ = v.update(m, key("down"))
	}
	m, _ = v.update(m, key("enter"))
	if v.mode != bfvForm {
		t.Fatalf("mode = %v", v.mode)
	}
	m = typeText(v, m, "wip")             // name
	m, _ = v.update(m, key("tab"))        // mode: leave hide
	m, _ = v.update(m, key("tab"))        // older than
	m = typeText(v, m, "30d")
	m, _ = v.update(m, key("tab"))        // younger than
	m, _ = v.update(m, key("tab"))        // prefix
	m, _ = v.update(m, key("tab"))        // suffix
	m = typeText(v, m, "-wip")
	m, _ = v.update(m, key("tab"))        // contains
	m, _ = v.update(m, key("tab"))        // regex
	m, _ = v.update(m, key("tab"))        // scope: toggle to repo
	m, _ = v.update(m, key("l"))
	m, _ = v.update(m, key("enter"))
	if v.formErr != "" {
		t.Fatalf("formErr = %q", v.formErr)
	}
	raw, err := os.ReadFile(repoPath)
	if err != nil {
		t.Fatalf("repo config not written: %v", err)
	}
	if !strings.Contains(string(raw), "slot = 5") || !strings.Contains(string(raw), `suffix = "-wip"`) || !strings.Contains(string(raw), `older_than = "30d"`) {
		t.Errorf("written:\n%s", raw)
	}
	if !m.branchFilters[4].Usable() || m.branchFilters[4].Name != "wip" {
		t.Errorf("model not reloaded: %+v", m.branchFilters[4].Slot)
	}
	if v.mode != bfvBrowse {
		t.Errorf("form should close on save")
	}
}

func TestBranchFilterPopupRefusesBadRegexInline(t *testing.T) {
	m, v, repoPath := bfPopupModel(t)
	m, _ = v.update(m, key("enter")) // edit slot 1
	for i := 0; i < 7; i++ {
		m, _ = v.update(m, key("tab"))
	}
	m = typeText(v, m, "(")
	m, _ = v.update(m, key("enter"))
	if !strings.Contains(v.formErr, "regex") {
		t.Errorf("formErr = %q", v.formErr)
	}
	if _, err := os.Stat(repoPath); err == nil {
		t.Error("a refused save wrote the repo file")
	}
	if v.mode != bfvForm {
		t.Error("form closed on a refused save")
	}
}

func TestBranchFilterPopupDeleteRemovesBlockAndClearsActive(t *testing.T) {
	m, v, repoPath := bfPopupModel(t)
	if err := os.WriteFile(repoPath, []byte("[[branches.filter]]\nslot = 1\nname = \"feat\"\nprefix = \"feat/\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = m.reloadConfigAfterBranchFilterWrite()
	m.branchFilterSlot[panelBranches] = 1
	m, _ = v.update(m, key("d"))
	raw, _ := os.ReadFile(repoPath)
	if strings.Contains(string(raw), "slot = 1") {
		t.Errorf("block not removed:\n%s", raw)
	}
	if m.branchFilterSlot[panelBranches] != 0 {
		t.Error("active slot should clear when its definition is gone")
	}
	if !m.branchFilters[0].Empty {
		t.Errorf("slot 1 still compiled: %+v", m.branchFilters[0].Slot)
	}
}
```

Adjust `m.topLayer()` to whatever accessor the layer stack exposes (grep `func (m Model) topLayer\|layers\[len` in `internal/tui`).

- [ ] **Step 2: Run to verify failure**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/branch-filters && go test ./internal/tui/ -run 'BranchFilterPopup' 2>&1 | head -5`
Expected: undefined `openBranchFilterSettings`.

- [ ] **Step 3: Implement `branch_filter_popup.go`**

```go
package tui

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/branchfilter"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/i18n"
)

// branchFilterView is Settings → Branch filters…: the five slots as rows
// (browse), and a form editing one slot (edit). Saves go through the scoped
// block writers and reload m.cfg so the open panels re-evaluate at once.
// Mirrors prefixSettingsView's browse/form split.
type branchFilterView struct {
	popupMax
	sel     int // 0..4 = slot-1
	mode    bfvMode
	fields  [bfFieldCount]textfield
	fmode   branchfilter.Mode
	scope   bfScope
	field   int    // focused form row: 0 name, 1 mode, 2 older, 3 younger, 4 prefix, 5 suffix, 6 contains, 7 regex, 8 scope
	formErr string // inline validation error; "" = none
}

type bfvMode int

const (
	bfvBrowse bfvMode = iota
	bfvForm
)

type bfScope int

const (
	bfScopeGlobal bfScope = iota
	bfScopeRepo
)

// Text fields in the form, in row order (mode and scope are toggles).
const (
	bfName = iota
	bfOlder
	bfYounger
	bfPrefix
	bfSuffix
	bfContains
	bfRegex
	bfFieldCount
)

// formRows is the form's row order: text fields interleaved with the two
// toggles. rowKind maps a focused row to what it edits.
const (
	rowName = iota
	rowMode
	rowOlder
	rowYounger
	rowPrefix
	rowSuffix
	rowContains
	rowRegex
	rowScope
	rowCount
)

func (m Model) openBranchFilterSettings() (Model, tea.Cmd) {
	return m.pushLayer(&branchFilterView{}), nil
}

// bfLabel translates a form row's aligned label at render time (a package var
// would freeze the English text before any language loads).
func bfLabel(row int) string {
	switch row {
	case rowName:
		return i18n.T("name:      ")
	case rowMode:
		return i18n.T("mode:      ")
	case rowOlder:
		return i18n.T("older than:")
	case rowYounger:
		return i18n.T("younger:   ")
	case rowPrefix:
		return i18n.T("prefix:    ")
	case rowSuffix:
		return i18n.T("suffix:    ")
	case rowContains:
		return i18n.T("contains:  ")
	case rowRegex:
		return i18n.T("regex:     ")
	case rowScope:
		return i18n.T("scope:     ")
	}
	return ""
}

func (v *branchFilterView) textField(row int) *textfield {
	switch row {
	case rowName:
		return &v.fields[bfName]
	case rowOlder:
		return &v.fields[bfOlder]
	case rowYounger:
		return &v.fields[bfYounger]
	case rowPrefix:
		return &v.fields[bfPrefix]
	case rowSuffix:
		return &v.fields[bfSuffix]
	case rowContains:
		return &v.fields[bfContains]
	case rowRegex:
		return &v.fields[bfRegex]
	}
	return nil
}

// openForm prefills the form from the compiled slot (an empty slot starts
// blank, mode hide, scope global).
func (v *branchFilterView) openForm(c branchfilter.Compiled) {
	s := c.Slot
	v.fields[bfName] = newTextField(s.Name)
	v.fields[bfOlder] = newTextField(strings.TrimSpace(s.OlderThan))
	v.fields[bfYounger] = newTextField(strings.TrimSpace(s.YoungerThan))
	v.fields[bfPrefix] = newTextField(s.Prefix)
	v.fields[bfSuffix] = newTextField(s.Suffix)
	v.fields[bfContains] = newTextField(s.Contains)
	v.fields[bfRegex] = newTextField(s.Regex)
	v.fmode = s.Mode
	if v.fmode == "" {
		v.fmode = branchfilter.ModeHide
	}
	v.scope = bfScopeGlobal
	v.field = rowName
	v.formErr = ""
	v.mode = bfvForm
}

// formSlot assembles the Slot the form describes for slot number n.
func (v *branchFilterView) formSlot(n int) branchfilter.Slot {
	return branchfilter.Slot{
		Slot:        n,
		Name:        strings.TrimSpace(v.fields[bfName].Value()),
		Mode:        v.fmode,
		OlderThan:   strings.TrimSpace(v.fields[bfOlder].Value()),
		YoungerThan: strings.TrimSpace(v.fields[bfYounger].Value()),
		Prefix:      v.fields[bfPrefix].Value(),
		Suffix:      v.fields[bfSuffix].Value(),
		Contains:    v.fields[bfContains].Value(),
		Regex:       v.fields[bfRegex].Value(),
	}
}

// writePath is the config file the form's scope targets. "" when the repo
// scope has no path (no repo config resolved yet).
func (v *branchFilterView) writePath(m Model) string {
	if v.scope == bfScopeRepo {
		return m.repoConfigPath
	}
	return config.DefaultGlobalPath()
}

func (v *branchFilterView) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if v.mode == bfvForm {
		return v.updateForm(m, msg)
	}
	return v.updateBrowse(m, msg)
}

func (v *branchFilterView) updateBrowse(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyUp:
		if v.sel > 0 {
			v.sel--
		}
		return m, nil
	case tea.KeyDown:
		if v.sel < branchfilter.MaxSlots-1 {
			v.sel++
		}
		return m, nil
	case tea.KeyEnter:
		v.openForm(m.branchFilters[v.sel])
		return m, nil
	}
	switch msg.String() {
	case "j":
		if v.sel < branchfilter.MaxSlots-1 {
			v.sel++
		}
	case "k":
		if v.sel > 0 {
			v.sel--
		}
	case "d":
		// Remove from BOTH files: a slot deleted in the repo file would
		// otherwise resurface from global, which reads as "delete did nothing".
		n := v.sel + 1
		var errs []string
		for _, p := range []string{m.repoConfigPath, config.DefaultGlobalPath()} {
			if p == "" {
				continue
			}
			if err := config.RemoveBranchFilter(p, n); err != nil {
				errs = append(errs, err.Error())
			}
		}
		m = m.reloadConfigAfterBranchFilterWrite()
		if len(errs) > 0 {
			m.statusMsg = i18n.T("branch filter %d not removed: %s", n, strings.Join(errs, "; "))
		} else {
			m.statusMsg = i18n.T("branch filter %d removed", n)
		}
	}
	return m, nil
}

func (v *branchFilterView) updateForm(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		v.mode = bfvBrowse
		return m, nil
	case tea.KeyUp:
		if v.field > 0 {
			v.field--
		}
		return m, nil
	case tea.KeyDown, tea.KeyTab:
		if v.field < rowCount-1 {
			v.field++
		}
		return m, nil
	case tea.KeyEnter:
		n := v.sel + 1
		s := v.formSlot(n)
		c := branchfilter.Compile(s)
		if c.Err != nil {
			v.formErr = c.Err.Error()
			return m, nil
		}
		if c.Empty {
			v.formErr = i18n.T("set at least one clause (age, prefix, suffix, contains or regex)")
			return m, nil
		}
		path := v.writePath(m)
		if path == "" {
			v.formErr = i18n.T("no repo config path — choose global, or open a repo first")
			return m, nil
		}
		if err := config.SetBranchFilter(path, s); err != nil {
			v.formErr = err.Error()
			return m, nil
		}
		v.formErr = ""
		v.mode = bfvBrowse
		m = m.reloadConfigAfterBranchFilterWrite()
		m.statusMsg = i18n.T("branch filter %d saved: %s", n, c.Label())
		return m, nil
	}
	switch v.field {
	case rowMode:
		switch msg.String() {
		case "left", "right", " ", "h", "l":
			if v.fmode == branchfilter.ModeHide {
				v.fmode = branchfilter.ModeShow
			} else {
				v.fmode = branchfilter.ModeHide
			}
		}
		return m, nil
	case rowScope:
		switch msg.String() {
		case "left", "right", " ", "h", "l":
			if v.scope == bfScopeGlobal {
				v.scope = bfScopeRepo
			} else {
				v.scope = bfScopeGlobal
			}
		}
		return m, nil
	}
	if f := v.textField(v.field); f != nil {
		f.HandleEditKey(msg)
	}
	return m, nil
}

func (v *branchFilterView) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), v.box(m), w, h)
}

func (v *branchFilterView) box(m Model) string {
	w, _ := m.overlayDims()
	inner := popupResolveWidth(w, v.maximized, popupWideInnerWidth(w))
	textW := popupTextWidth(inner)
	s := st()

	if v.mode == bfvForm {
		parts := []string{i18n.T("Branch filter %d", v.sel+1), ""}
		for row := 0; row < rowCount; row++ {
			cur := "  "
			if row == v.field {
				cur = "> "
			}
			switch row {
			case rowMode:
				val := i18n.T("hide matching branches")
				if v.fmode == branchfilter.ModeShow {
					val = i18n.T("show ONLY matching branches")
				}
				parts = append(parts, cur+bfLabel(row)+" "+val)
			case rowScope:
				val := i18n.T("global (every repo)")
				if v.scope == bfScopeRepo {
					val = i18n.T("this repo only")
				}
				parts = append(parts, cur+bfLabel(row)+" "+val)
			default:
				parts = append(parts, viewField(cur+bfLabel(row)+" ", *v.textField(row), row == v.field, textW))
			}
		}
		if v.formErr != "" {
			parts = append(parts, "", s.errorText.Render(v.formErr))
		}
		parts = append(parts, "",
			i18n.T("ages: 90d 12w 6m 1y · set clauses AND together · regex is Go RE2 · names are the branch part (no origin/)"),
			"",
			i18n.T("[↑/↓/tab] field  [←/→] toggle  [enter] save  [esc] back"))
		return popupBox(inner, strings.Join(parts, "\n"))
	}

	parts := []string{i18n.T("Branch filters (alt+1…5 on the Branches / Remotes panel)"), ""}
	wr := make([]winRow, branchfilter.MaxSlots)
	for i := 0; i < branchfilter.MaxSlots; i++ {
		c := m.branchFilters[i]
		prefix := "  "
		var style lipgloss.Style
		if i == v.sel {
			prefix, style = "> ", s.selectedRow
		}
		text := prefix + strconv.Itoa(i+1) + "  "
		switch {
		case c.Err != nil:
			text += i18n.T("invalid — %s", c.Err.Error())
		case c.Empty:
			text += "—"
		default:
			text += padRight(c.Label(), 14) + c.Summary()
		}
		wr[i] = winRow{text: text, style: style}
	}
	parts = append(parts, renderWindow(wr, winOpts{w: textW, h: branchfilter.MaxSlots, anchor: v.sel})...)
	parts = append(parts, "", i18n.T("[enter] edit  [d] remove (repo + global)  [esc] back"))
	return popupBox(inner, strings.Join(parts, "\n"))
}
```

Add to `branch_filter.go`:

```go
// reloadConfigAfterBranchFilterWrite re-reads the effective config after a
// Settings write and recompiles the slots; an active slot whose definition
// vanished (or turned invalid) is cleared so the panel never shows a stale
// filter, and the memos are dropped.
func (m Model) reloadConfigAfterBranchFilterWrite() Model {
	if cfg, err := config.Load(config.DefaultGlobalPath(), m.repoConfigPath); err == nil {
		m.cfg = cfg
	}
	m.branchFilters = branchfilter.CompileAll(m.cfg.Branches.Filter)
	m.bfMemo = &branchFilterMemos{}
	for _, p := range []panel{panelBranches, panelRemotes} {
		if s := m.branchFilterSlot[p]; s > 0 && !m.branchFilters[s-1].Usable() {
			m.branchFilterSlot[p] = 0
			if m.promptStore != nil {
				_ = m.promptStore.SetBranchFilterSlot(m.toolRepoKey(), branchFilterListName(p), 0)
			}
		}
	}
	return m
}
```
(add `"github.com/homeend/gigagit/internal/config"` to that file's imports).

Settings wiring in `settings_popup.go`:
- `settingsMenuBranchFilters = "Branch filters"` const; insert after `settingsMenuPrefixes` in `settingsMenu`.
- `settingsMenuTitle`: `case settingsMenuBranchFilters: return i18n.T("Branch filters…")`.
- enter switch: `case settingsMenuBranchFilters: return m.openBranchFilterSettings()`.
- `menu_labels_test.go` may enumerate settings rows — add the new key where it lists them.

- [ ] **Step 4: Run**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/branch-filters && go test ./internal/tui/ 2>&1 | tail -20`
Expected: PASS including the four i18n gates (add every literal to the four bundles).

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/branch-filters && gofmt -l internal/tui && go vet ./internal/tui/ && git add internal/tui internal/i18n && git commit -q -m "feat(tui): Settings → Branch filters… — browse the five slots, edit one (global or repo), remove" -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01BqyuQk8TCehWj3QfkHUrAM"
```

---

### Task 7: Web server — verdicts on `/api/branches` + `/api/remotes`, `PUT /api/branch-filter`

**Files:**
- Create: `internal/web/branchfilter.go`
- Create: `internal/web/branchfilter_test.go`
- Modify: `internal/web/branches.go`, `internal/web/remotes.go`, `internal/web/server.go` (routes after `/api/remotes`)
- Read first: `internal/web/uistate.go` (`webUIStore`, `writeGuard` use), `internal/web/uistate_test.go` (`uiServer`, `putJSON`, `getJSON`, `newRepoDir`), `internal/web/remotes.go` (sort → cap).

**Interfaces:**
- Consumes: `domain.BranchFilters/ExemptBranches/ExemptRemoteBranches/BranchRows/RemoteBranchRows`, `branchfilter.Apply`, `promptstate.FileStore.BranchFilterSlot/SetBranchFilterSlot`, `svc.GitCommonDir(ctx)`.
- Produces (wire):
  - `GET /api/branches` → `{"branches":[{…,"hidden":bool,"exempt":bool}], "filter": filterWire|null}`
  - `GET /api/remotes` → `{"remotes":[…visible only…], "truncated":bool, "filter": filterWire|null}`
  - `PUT /api/branch-filter` body `{"list":"branches"|"remotes","slot":0..5}` → `{"filter": filterWire|null}`; 400 on bad list/slot; 409 with `{"error":"slot N: <reason>"}` when the slot is unusable.
  - `GET /api/branch-filters` → `{"slots":[{slot,name,mode,summary,usable,error}...5]}` (for the chip menu + settings section).
  - `filterWire = {"slot":int,"name":string,"mode":"hide"|"show","hidden":int}`
  ```go
  type filterWire struct { Slot int `json:"slot"`; Name string `json:"name"`; Mode string `json:"mode"`; Hidden int `json:"hidden"` }
  func (s *Server) activeBranchFilter(ctx, svc, list string) (c *branchfilter.Compiled, slot int)  // nil when none/unusable
  func (s *Server) branchFilterRepoKey(ctx, svc) string
  ```

- [ ] **Step 1: Write the failing tests**

`internal/web/branchfilter_test.go`:

```go
package web

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// bfRepo makes a repo with main (HEAD), feat/a, feat/old (backdated 200d),
// fix/b, and 130 remote-tracking branches origin/r000…r129 of which the
// even ones are feat/* — enough to prove the remotes filter runs before the
// 100-row cap.
func bfRepo(t *testing.T) string {
	t.Helper()
	dir := newRepoDir(t, 1)
	run := func(env []string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), env...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run(nil, "branch", "feat/a")
	run(nil, "branch", "fix/b")
	old := time.Now().Add(-200 * 24 * time.Hour).Format(time.RFC3339)
	run([]string{"GIT_COMMITTER_DATE=" + old, "GIT_AUTHOR_DATE=" + old}, "commit", "--allow-empty", "-m", "old", "-q")
	run(nil, "branch", "feat/old")
	run(nil, "reset", "-q", "--hard", "HEAD~1")
	for i := 0; i < 130; i++ {
		name := "r" + fmt.Sprintf("%03d", i)
		if i%2 == 0 {
			name = "feat/" + name
		}
		run(nil, "update-ref", "refs/remotes/origin/"+name, "HEAD")
	}
	if err := os.WriteFile(filepath.Join(dir, ".gg.toml"), []byte(`
[[branches.filter]]
slot = 1
name = "feat"
prefix = "feat/"

[[branches.filter]]
slot = 2
name = "stale"
older_than = "90d"

[[branches.filter]]
slot = 4
regex = "("
`), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func bfServer(t *testing.T) *httptest.Server {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	ts := httptest.NewServer(New(domain.Open(bfRepo(t))).Handler())
	t.Cleanup(ts.Close)
	return ts
}

type bfBranchesResp struct {
	Branches []struct {
		Name   string `json:"name"`
		IsHead bool   `json:"is_head"`
		Hidden bool   `json:"hidden"`
		Exempt bool   `json:"exempt"`
	} `json:"branches"`
	Filter *struct {
		Slot   int    `json:"slot"`
		Name   string `json:"name"`
		Mode   string `json:"mode"`
		Hidden int    `json:"hidden"`
	} `json:"filter"`
}

func TestBranchesCarryVerdictsUnderActiveSlot(t *testing.T) {
	ts := bfServer(t)
	var out bfBranchesResp
	getJSON(t, ts, "/api/branches", &out)
	if out.Filter != nil {
		t.Fatalf("no slot yet: filter = %+v", out.Filter)
	}
	var put struct {
		Filter *struct{ Slot, Hidden int } `json:"filter"`
	}
	if code := putJSON(t, ts, "/api/branch-filter", `{"list":"branches","slot":1}`, "", &put); code != 200 {
		t.Fatalf("PUT: %d", code)
	}
	getJSON(t, ts, "/api/branches", &out)
	if out.Filter == nil || out.Filter.Slot != 1 || out.Filter.Name != "feat" || out.Filter.Mode != "hide" || out.Filter.Hidden != 2 {
		t.Fatalf("filter = %+v", out.Filter)
	}
	byName := map[string]bool{}
	for _, b := range out.Branches {
		byName[b.Name] = b.Hidden
	}
	if !byName["feat/a"] || !byName["feat/old"] || byName["main"] || byName["fix/b"] {
		t.Errorf("hidden map = %v", byName)
	}
	if len(out.Branches) != 4 {
		t.Errorf("branches wire must keep hidden rows (flagged): %d rows", len(out.Branches))
	}
}

func TestRemotesFilterBeforeCap(t *testing.T) {
	ts := bfServer(t)
	var out struct {
		Remotes   []struct{ Name string } `json:"remotes"`
		Truncated bool                    `json:"truncated"`
		Filter    *struct{ Slot, Hidden int } `json:"filter"`
	}
	getJSON(t, ts, "/api/remotes", &out)
	if len(out.Remotes) != 100 || !out.Truncated {
		t.Fatalf("unfiltered: %d rows truncated=%v", len(out.Remotes), out.Truncated)
	}
	putJSON(t, ts, "/api/branch-filter", `{"list":"remotes","slot":1}`, "", nil)
	getJSON(t, ts, "/api/remotes", &out)
	// 65 feat/* remote branches hidden, 65 plain ones remain: under the cap, not truncated.
	if len(out.Remotes) != 65 || out.Truncated {
		t.Errorf("filtered: %d rows truncated=%v (filter must run BEFORE the cap)", len(out.Remotes), out.Truncated)
	}
	if out.Filter == nil || out.Filter.Hidden != 65 {
		t.Errorf("filter = %+v", out.Filter)
	}
	for _, r := range out.Remotes {
		if len(r.Name) > 12 && r.Name[:12] == "origin/feat/" {
			t.Errorf("hidden remote row on the wire: %s", r.Name)
		}
	}
}

func TestBranchFilterPutValidation(t *testing.T) {
	ts := bfServer(t)
	cases := []struct {
		body string
		code int
	}{
		{`{"list":"tags","slot":1}`, 400},
		{`{"list":"branches","slot":6}`, 400},
		{`{"list":"branches","slot":-1}`, 400},
		{`{"list":"branches","slot":4}`, 409}, // inert regex
		{`{"list":"branches","slot":3}`, 409}, // empty slot
		{`not json`, 400},
		{`{"list":"branches","slot":0}`, 200},
	}
	for _, c := range cases {
		if code := putJSON(t, ts, "/api/branch-filter", c.body, "", nil); code != c.code {
			t.Errorf("%s → %d; want %d", c.body, code, c.code)
		}
	}
}

func TestBranchFilterSlotsListing(t *testing.T) {
	ts := bfServer(t)
	var out struct {
		Slots []struct {
			Slot    int    `json:"slot"`
			Name    string `json:"name"`
			Summary string `json:"summary"`
			Usable  bool   `json:"usable"`
			Error   string `json:"error"`
		} `json:"slots"`
	}
	getJSON(t, ts, "/api/branch-filters", &out)
	if len(out.Slots) != 5 || out.Slots[0].Name != "feat" || !out.Slots[0].Usable || out.Slots[3].Usable || out.Slots[3].Error == "" || out.Slots[4].Usable {
		t.Errorf("slots = %+v", out.Slots)
	}
}
```

Add the missing imports (`fmt`, `domain`) as the compiler asks; if `newRepoDir`'s signature differs, adapt to `uistate_test.go`'s usage.

- [ ] **Step 2: Run to verify failure**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/branch-filters && go test ./internal/web/ -run 'BranchesCarryVerdicts|RemotesFilterBeforeCap|BranchFilterPut|BranchFilterSlotsListing' 2>&1 | head -8`
Expected: 404s / missing fields → failures.

- [ ] **Step 3: Implement**

`internal/web/branchfilter.go`:

```go
package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/homeend/gigagit/internal/branchfilter"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/promptstate"
)

// Branch filters on the web: the server evaluates (one matcher, Go RE2) and
// the client only renders. The active slot is the same per-repo record the
// TUI keeps in prompts.toml, so alt+2 in one is what the other shows next.

type filterWire struct {
	Slot   int    `json:"slot"`
	Name   string `json:"name"`
	Mode   string `json:"mode"`
	Hidden int    `json:"hidden"`
}

type slotWire struct {
	Slot    int    `json:"slot"`
	Name    string `json:"name"`
	Mode    string `json:"mode"`
	Summary string `json:"summary"`
	Usable  bool   `json:"usable"`
	Error   string `json:"error,omitempty"`
}

// branchFilterRepoKey is the promptstate scope: the git common dir.
func (s *Server) branchFilterRepoKey(ctx context.Context, svc *domain.Service) string {
	if common, err := svc.GitCommonDir(ctx); err == nil {
		return common
	}
	return ""
}

// activeBranchFilter resolves list's remembered slot to a usable Compiled;
// nil when none is set, or the stored slot no longer exists / is inert.
func (s *Server) activeBranchFilter(ctx context.Context, svc *domain.Service, list string) *branchfilter.Compiled {
	store := s.webUIStore()
	if store == nil {
		return nil
	}
	slot := store.BranchFilterSlot(s.branchFilterRepoKey(ctx, svc), list)
	if slot < 1 || slot > branchfilter.MaxSlots {
		return nil
	}
	all, err := svc.BranchFilters(ctx)
	if err != nil || !all[slot-1].Usable() {
		return nil
	}
	c := all[slot-1]
	return &c
}

func wireFor(c *branchfilter.Compiled, hidden int) *filterWire {
	if c == nil {
		return nil
	}
	return &filterWire{Slot: c.Slot.Slot, Name: c.Label(), Mode: string(c.Mode), Hidden: hidden}
}

// applyFilter runs the active slot over rows; nil verdicts when none.
func applyFilter(c *branchfilter.Compiled, rows []branchfilter.Row, exempt []bool) ([]branchfilter.Verdict, int) {
	if c == nil {
		return nil, 0
	}
	return branchfilter.Apply(*c, rows, exempt, time.Now())
}

type branchFilterSetRequest struct {
	List string `json:"list"`
	Slot *int   `json:"slot"`
}

func (s *Server) handleBranchFilterSet(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	var req branchFilterSetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("bad request body"))
		return
	}
	if req.List != promptstate.BranchFilterListBranches && req.List != promptstate.BranchFilterListRemotes {
		writeErr(w, http.StatusBadRequest, errors.New("list must be branches or remotes"))
		return
	}
	if req.Slot == nil || *req.Slot < 0 || *req.Slot > branchfilter.MaxSlots {
		writeErr(w, http.StatusBadRequest, errors.New("slot must be 0..5"))
		return
	}
	var active *branchfilter.Compiled
	if *req.Slot > 0 {
		all, err := svc.BranchFilters(r.Context())
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		c := all[*req.Slot-1]
		if !c.Usable() {
			writeErr(w, http.StatusConflict, errors.New("slot "+itoa(*req.Slot)+": "+c.Summary()))
			return
		}
		active = &c
	}
	store := s.webUIStore()
	if store == nil {
		writeErr(w, http.StatusInternalServerError, errors.New("no state dir to remember the filter in"))
		return
	}
	if err := store.SetBranchFilterSlot(s.branchFilterRepoKey(r.Context(), svc), req.List, *req.Slot); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{"filter": wireFor(active, 0)})
}

func (s *Server) handleBranchFilters(w http.ResponseWriter, r *http.Request) {
	all, err := s.service().BranchFilters(readCtx(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	slots := make([]slotWire, 0, branchfilter.MaxSlots)
	for _, c := range all {
		sw := slotWire{Slot: c.Slot.Slot, Name: c.Label(), Mode: string(c.Mode), Summary: c.Summary(), Usable: c.Usable()}
		if c.Err != nil {
			sw.Error = c.Err.Error()
		}
		slots = append(slots, sw)
	}
	writeJSON(w, map[string]any{"slots": slots})
}

func itoa(n int) string { return strconv.Itoa(n) }
```
(add `strconv` to imports; drop `itoa` and use `strconv.Itoa` directly if preferred.)

`branches.go`: add `Hidden bool \`json:"hidden"\`` and `Exempt bool \`json:"exempt"\`` to `branchRow`; in `handleBranches`, after fetching `bs`, fetch worktrees (`svc.Worktrees(ctx)`; on error treat as none), then:

```go
	active := s.activeBranchFilter(ctx, svc, promptstate.BranchFilterListBranches)
	verdicts, hidden := applyFilter(active, domain.BranchRows(bs), domain.ExemptBranches(bs, wts))
	// … in the loop: row.Hidden/Exempt = verdicts[i].Hidden/Exempt when verdicts != nil
	writeJSON(w, map[string]any{"branches": rows, "filter": wireFor(active, hidden)})
```

`remotes.go` `handleRemotes`: after `sortedRows(...)` and BEFORE the cap, fetch `bs, _ := svc.Branches(ctx)` (for the upstream exemption), compute `verdicts, hidden := applyFilter(active, domain.RemoteBranchRows(rbs), domain.ExemptRemoteBranches(rbs, bs))`, and drop rows whose verdict is Hidden (build a new slice), then apply the cap to the survivors. Emit `"filter": wireFor(active, hidden)` alongside `remotes`/`truncated`. Update the comment above the sort to say "sort → filter → cap".

`server.go` routes, after `/api/remotes`:

```go
	mux.HandleFunc("GET /api/branch-filters", s.handleBranchFilters)
	mux.HandleFunc("PUT /api/branch-filter", writeGuard(s.handleBranchFilterSet))
```

- [ ] **Step 4: Run**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/branch-filters && go test ./internal/web/ 2>&1 | tail -15`
Expected: PASS (existing branches/remotes tests must still pass — the added fields are additive).

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/branch-filters && gofmt -l internal/web && go vet ./internal/web/ && git add internal/web/*.go && git commit -q -m "feat(web): branch-filter verdicts on /api/branches and /api/remotes (filter before the cap), PUT /api/branch-filter, GET /api/branch-filters" -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01BqyuQk8TCehWj3QfkHUrAM"
```

---

### Task 8: Web client — chip + menu, alt+digit keys, hidden rows, exempt marker

**Files:**
- Create: `internal/web/static/branchfilter.js`
- Modify: `internal/web/static/sidebar.js` (imports; `fetchBranches` ~line 24–45 and the remotes fetch ~line 718 to keep `filter`; `renderBranches` ~line 76; `renderRemotes` ~line 102; the header renderer ~line 681; the header click handler ~line 760)
- Modify: `internal/web/static/keys.js` (the document keydown ~line 75, before the form-field guard)
- Modify: `internal/web/static/style.css` (`.filterchip`, `.filterchip.on`, `li.exempt`, `.bfmenu`)
- Modify: `internal/web/static/index.html` (help row next to "list order" ~line 264; footer is unchanged)
- Read first: `internal/web/static/sortlist.js` (chip pattern), `internal/web/static/core.js` (`state`, `$`, `esc`, `getJSON`, `opLine`, layer helpers `pushLayer/closeLayer/topLayer` if a popup menu needs them), `internal/web/static/menus.js` (how a context menu is shown at x,y — reuse it for the chip menu).

**Interfaces:**
- Consumes: the wire from Task 7.
- Produces (ES module exports): `filterChipHTML(list)`, `openFilterMenu(list, x, y)`, `setBranchFilterSlot(list, slot)`, `branchFilterKey(e)` (returns true when it consumed the key), `applyFilterHeader(list, filter)`.
- State: `state.branchFilter = { branches: filterWire|null, remotes: filterWire|null }`, `state.branchFilterSlots = slotWire[]` (lazy-loaded for the menu).

- [ ] **Step 1: Implement `branchfilter.js`**

```js
// branchfilter.js — part of gg's web client. The five branch-filter slots
// (alt+1…5 in the TUI) on the branches and remotes lists: a chip in each
// section header shows the active slot and opens a menu of the five; the
// keys alt+1…5 (branches) and alt+shift+1…5 (remotes) toggle a slot the TUI
// way — the active slot's key clears it. Evaluation is SERVER-SIDE (one
// matcher, Go RE2): this module only remembers the header the server sent
// and asks it to change the slot. Matching on e.code, not e.key: with alt
// held, e.key is a dead or accented character on several layouts.
import { $, esc, getJSON, opLine, state } from "./core.js";
import { showMenuAt } from "./menus.js";

const LISTS = new Set(["branches", "remotes"]);

state.branchFilter = state.branchFilter || { branches: null, remotes: null };

// applyFilterHeader remembers the filter block a list fetch returned.
function applyFilterHeader(list, filter) {
  if (!LISTS.has(list)) return;
  state.branchFilter[list] = filter || null;
}

function filterChipHTML(list) {
  if (!LISTS.has(list)) return "";
  const f = state.branchFilter[list];
  const label = f ? `\u25bd${f.slot} ${f.name}${f.hidden ? " \u00b7 " + f.hidden + " hidden" : ""}` : "\u25bd";
  const title = f
    ? `branch filter ${f.slot} (${f.mode === "show" ? "show only" : "hide"} ${esc(f.name)}) — click to change`
    : "branch filter: none — click to pick one (alt+1…5)";
  return `<span class="filterchip${f ? " on" : ""}" data-filter="${esc(list)}" title="${esc(title)}">${esc(label)}</span>`;
}

// setBranchFilterSlot asks the server to activate slot (0 = none) for list,
// then refetches that list so the rows and the chip agree with the server.
async function setBranchFilterSlot(list, slot) {
  try {
    const resp = await fetch("/api/branch-filter", {
      method: "PUT",
      headers: { "Content-Type": "application/json", Accept: "application/json" },
      body: JSON.stringify({ list, slot }),
    });
    if (!resp.ok) {
      let msg = String(resp.status);
      try { msg = (await resp.json()).error || msg; } catch {}
      opLine(msg, true);
      return false;
    }
  } catch (e) {
    opLine("branch filter: " + e.message, true);
    return false;
  }
  if (window.__ggRefetchList) await window.__ggRefetchList(list);
  const f = state.branchFilter[list];
  opLine(f ? `branch filter ${f.slot}: ${f.name}` : "branch filter off");
  return true;
}

// toggleSlot is the radio rule shared with the TUI.
function toggleSlot(list, slot) {
  const cur = state.branchFilter[list];
  return setBranchFilterSlot(list, cur && cur.slot === slot ? 0 : slot);
}

async function openFilterMenu(list, x, y) {
  let slots = [];
  try {
    slots = (await getJSON("/api/branch-filters")).slots || [];
  } catch (e) {
    opLine("branch filters: " + e.message, true);
    return;
  }
  const cur = state.branchFilter[list];
  const rows = [
    { label: (cur ? "  " : "\u2713 ") + "none", act: () => setBranchFilterSlot(list, 0) },
    ...slots.map((s) => ({
      label: `${cur && cur.slot === s.slot ? "\u2713 " : "  "}${s.slot}  ${s.name} — ${s.summary}`,
      disabled: !s.usable,
      act: () => setBranchFilterSlot(list, s.slot),
    })),
  ];
  showMenuAt(rows, x, y);
}

// branchFilterKey routes alt+DigitN / alt+shift+DigitN. Returns true when
// consumed. Callers run it before the form-field guard is NOT wanted: a
// digit typed in a commit message must stay a digit, so keys.js calls this
// only outside inputs.
function branchFilterKey(e) {
  if (!e.altKey || e.ctrlKey || e.metaKey) return false;
  const m = /^Digit([1-5])$/.exec(e.code || "");
  if (!m) return false;
  e.preventDefault();
  toggleSlot(e.shiftKey ? "remotes" : "branches", Number(m[1]));
  return true;
}

export { applyFilterHeader, branchFilterKey, filterChipHTML, openFilterMenu, setBranchFilterSlot };
```

If `menus.js` exposes a differently named "show rows at x,y" helper, use that name (grep `export` in `menus.js`); a `disabled` row must render muted and inert.

- [ ] **Step 2: Wire `sidebar.js`**

1. `import { applyFilterHeader, filterChipHTML, openFilterMenu } from "./branchfilter.js";`
2. In `fetchBranches` (where `state.branches = b.branches || []`) add `applyFilterHeader("branches", b.filter)`; in the remotes fetch (both the initial one and the live-refresh one near line 718 that sets `state.remotesTruncated`) add `applyFilterHeader("remotes", rm.filter)`.
3. `renderBranches`: skip hidden rows and mark exempt ones:
   ```js
   $("branches-list").innerHTML = sortedBy("branches", state.branches, (b) => b.name, (b) => b.time)
     .filter((b) => !b.hidden)
     .map((b) => { … `<li class="${b.is_head ? "head" : ""}${b.exempt ? " exempt" : ""}" …` … `${esc(b.name)}${b.exempt ? `<span class="exempt-mark" title="the active filter would hide this row; kept because it is checked out">\u2217</span>` : ""}` … })
   ```
   Re-render the header too (`renderSectionHeader("branches")`, whatever the ~line 681 function is called) so the chip's hidden count updates with the rows.
4. `renderRemotes`: rows are already visible-only; append the header re-render.
5. Header renderer (~681): `… + control + filterChipHTML(name) + sortChipHTML(name);`
6. Header click handler (~760): before the `.sortchip` branch add
   ```js
   if (e.target.closest(".filterchip")) {
     openFilterMenu(n, e.clientX, e.clientY);
     return;
   }
   ```
7. Expose the refetch for `setBranchFilterSlot`: after the existing `fetchBranches`/remotes fetch definitions add `window.__ggRefetchList = (list) => (list === "branches" ? fetchBranches() : fetchRemotes());` (use the actual remotes fetch function name; if there is none standalone, extract the ~line 718 body into `fetchRemotes()`).

- [ ] **Step 3: Wire `keys.js`**

`import { branchFilterKey } from "./branchfilter.js";` and, in the document keydown handler, AFTER the layer routing and the palette shortcut but BEFORE the form-field guard `if (e.target.closest && e.target.closest("input,textarea"))`, add:

```js
  // alt+1…5 / alt+shift+1…5: branch-filter slots. Not inside inputs — a
  // digit typed into the commit box must stay a digit.
  if (!(e.target.closest && e.target.closest("input,textarea")) && branchFilterKey(e)) return;
```

- [ ] **Step 4: Styles + help**

`style.css`, next to `.sortchip`:
```css
.filterchip { font-size: 0.85em; opacity: 0.6; margin-left: 0.4em; padding: 0 0.3em; border: 1px solid transparent; border-radius: 3px; cursor: pointer; }
.filterchip.on { opacity: 1; color: var(--accent); border-color: var(--border); }
.filterchip:hover { opacity: 1; border-color: var(--border); }
li.exempt .exempt-mark { opacity: 0.5; margin-left: 0.3em; }
```
`index.html` help: after the "list order" `.hrow` add
```html
    <div class="hrow"><span class="hkey">branch filters</span><span>each of <b>branches</b> and <b>remotes</b> carries a <b>▽</b> chip: click it to pick one of five named filter slots (hide, or show only, branches by tip age / prefix / suffix / substring / regex — defined in settings… → branch filters or as <code>[[branches.filter]]</code> in .gg.toml). <b>alt+1…5</b> toggles a slot on branches, <b>alt+shift+1…5</b> on remotes; the active slot's key clears it. The choice is remembered per repo and shared with the TUI. The checked-out branch, branches checked out in a worktree and the current upstream are never hidden (∗ marks one the rule would hide). Remotes filter <b>before</b> the 100-row cap</span></div>
```

- [ ] **Step 5: Playwright probe (evidence, not a unit test)**

Follow the `playwright-web-verification` memory: build `/tmp/claude-1000/gg-bf` from the worktree, run `gg web` on an isolated `XDG_STATE_HOME`/`XDG_CONFIG_HOME` against the `bfRepo` layout (script it with the same git commands as Task 7's fixture), and assert with Playwright: (a) the branches header contains `▽`; (b) after `page.keyboard.press("Alt+Digit1")` the row `feat/a` is NOT visible and `main` is; (c) `Alt+Digit1` again restores it; (d) `Alt+Shift+Digit1` hides `origin/feat/r000` in remotes and leaves branches untouched; (e) clicking the chip lists five entries with slot 4 disabled; (f) restart `gg web` on a different port → the branches filter is still active (server-side state). Run the same script against the main-checkout binary first and confirm (b) FAILS there. Save the script to the scratchpad, not the repo; paste the pass/fail lines into the commit message.

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/branch-filters && go test ./internal/web/ 2>&1 | tail -3 && git add internal/web/static && git commit -q -m "feat(web): branch-filter chip + menu on the branches/remotes headers, alt+1…5 / alt+shift+1…5, hidden rows and ∗ exempt marker" -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01BqyuQk8TCehWj3QfkHUrAM"
```

---

### Task 9: Web settings — the five slots, edit form, scope, remove

**Files:**
- Modify: `internal/web/settings.go` (`settingsPayload` + `handleSettingsGet`; `settingsWriteRequest` + `handleSettingsSet`)
- Modify: `internal/web/settings_test.go` (or create `internal/web/settings_branchfilter_test.go`)
- Modify: `internal/web/static/settings.js` (`renderSettings` new section; `pendingEdits`/`savePatch` untouched — the form saves on its own button)
- Read first: `internal/web/settings.go` (the "validate everything first" rule), `internal/web/static/settings.js` (`setOpt`, `toggleBtn`, `.sact` click wiring).

**Interfaces:**
- GET adds `"branch_filters": slotWire[]` (Task 7's `slotWire` plus the raw fields so the form can prefill: add `OlderThan, YoungerThan, Prefix, Suffix, Contains, Regex string` with `json:"older_than"` etc. to `slotWire`).
- POST accepts `"branch_filters": [{ "slot": 1..5, "scope": "global"|"repo", "remove": bool, "name","mode","older_than","younger_than","prefix","suffix","contains","regex" }]`. Every item is validated with `branchfilter.Compile` (and refused when `Empty` unless `remove`) BEFORE any write; `remove` deletes from the named scope's file.

- [ ] **Step 1: Write the failing tests**

```go
func TestSettingsBranchFilterWriteAndRemove(t *testing.T) {
	ts := bfServer(t) // Task 7 helper: repo has slots 1,2,4
	var got struct {
		BranchFilters []struct {
			Slot   int    `json:"slot"`
			Name   string `json:"name"`
			Usable bool   `json:"usable"`
			Prefix string `json:"prefix"`
		} `json:"branch_filters"`
	}
	getJSON(t, ts, "/api/settings", &got)
	if len(got.BranchFilters) != 5 || got.BranchFilters[0].Prefix != "feat/" {
		t.Fatalf("GET: %+v", got.BranchFilters)
	}
	body := `{"branch_filters":[{"slot":5,"scope":"repo","name":"wip","mode":"show","suffix":"-wip"}]}`
	if code := postJSON(t, ts, "/api/settings", body, nil); code != 200 {
		t.Fatalf("POST: %d", code)
	}
	getJSON(t, ts, "/api/settings", &got)
	if got.BranchFilters[4].Name != "wip" || !got.BranchFilters[4].Usable {
		t.Errorf("slot 5 after write: %+v", got.BranchFilters[4])
	}
	// Bad regex refuses the WHOLE request and writes nothing.
	bad := `{"branch_filters":[{"slot":3,"scope":"repo","prefix":"x"},{"slot":5,"scope":"repo","regex":"("}]}`
	if code := postJSON(t, ts, "/api/settings", bad, nil); code != 400 {
		t.Errorf("bad regex → %d; want 400", code)
	}
	getJSON(t, ts, "/api/settings", &got)
	if got.BranchFilters[2].Usable || got.BranchFilters[4].Name != "wip" {
		t.Errorf("a refused batch wrote something: %+v", got.BranchFilters)
	}
	// Empty rule refused; remove works.
	if code := postJSON(t, ts, "/api/settings", `{"branch_filters":[{"slot":3,"scope":"repo"}]}`, nil); code != 400 {
		t.Errorf("empty rule → %d; want 400", code)
	}
	if code := postJSON(t, ts, "/api/settings", `{"branch_filters":[{"slot":5,"scope":"repo","remove":true}]}`, nil); code != 200 {
		t.Errorf("remove → %d", code)
	}
	getJSON(t, ts, "/api/settings", &got)
	if got.BranchFilters[4].Usable {
		t.Errorf("slot 5 still usable after remove")
	}
	if code := postJSON(t, ts, "/api/settings", `{"branch_filters":[{"slot":1,"scope":"elsewhere","prefix":"x"}]}`, nil); code != 400 {
		t.Errorf("bad scope → %d", code)
	}
}
```

Use this package's existing `postJSON` helper (grep `func postJSON` in `internal/web/*_test.go`).

- [ ] **Step 2: Run to verify failure**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/branch-filters && go test ./internal/web/ -run SettingsBranchFilter 2>&1 | head -5`
Expected: FAIL (no `branch_filters` in GET).

- [ ] **Step 3: Implement server**

In `settings.go`:
- `settingsPayload` gains `BranchFilters []slotWire \`json:"branch_filters"\``; in `handleSettingsGet` fill it from `branchfilter.CompileAll(cfg.Branches.Filter)` (map each Compiled to `slotWire` — factor Task 7's loop into `func slotWires(all [branchfilter.MaxSlots]branchfilter.Compiled) []slotWire` in `branchfilter.go` and use it in both handlers; extend `slotWire` with the raw fields).
- `settingsWriteRequest` gains:
  ```go
  BranchFilters []branchFilterEdit `json:"branch_filters"`
  ```
  ```go
  type branchFilterEdit struct {
      Slot   int    `json:"slot"`
      Scope  string `json:"scope"` // "global" | "repo"
      Remove bool   `json:"remove"`
      branchfilter.Slot // embedded: name, mode, older_than, …; its Slot field is shadowed by the outer one — copy outer Slot into it before Compile
  }
  ```
  If embedding confuses the JSON decoder (two `slot` keys), declare the eight fields explicitly instead and build a `branchfilter.Slot` in code.
- Validation block (before the "nothing to set" check): for each edit, scope must be `global`/`repo`, slot 1..5; unless `Remove`, `c := branchfilter.Compile(s)`; `c.Err != nil` → 400 `slot N: <err>`; `c.Empty` → 400 `slot N: set at least one clause`. Include `len(req.BranchFilters) > 0` in the "nothing to set" condition and in `needRepo` when any edit has scope repo.
- Write block (after the hook write, before the global section): for each edit pick `path := repoPath` or `config.DefaultGlobalPath()` by scope; `Remove` → `config.RemoveBranchFilter(path, slot)` else `config.SetBranchFilter(path, s)`; any error → `fail(err)`.

- [ ] **Step 4: Implement client**

In `settings.js` `renderSettings`, add before `<h3>repo</h3>`:

```js
    <h3>branch filters</h3>
    <div class="srow"><span class="snote">five slots for the branches / remotes lists (alt+1…5 · alt+shift+1…5, or the ▽ chip). a repo block replaces the global block for the same slot</span></div>
    ${(d.branch_filters || []).map((s) => `
    <div class="srow sbf" data-slot="${s.slot}"><span class="slbl">slot ${s.slot}</span><span class="sval">${s.usable ? esc(s.name) + " — " + esc(s.summary) : s.error ? "invalid — " + esc(s.error) : "—"}</span><button class="sact" data-act="bf-edit" data-slot="${s.slot}">edit…</button>${s.usable || s.error ? `<button class="sact" data-act="bf-remove" data-slot="${s.slot}">remove</button>` : ""}</div>`).join("")}
    <div id="bf-form" class="hidden"></div>
```

Add two handlers next to the existing `.sact` click wiring:
- `bf-edit`: render into `#bf-form` a small form prefilled from `d.branch_filters[slot-1]`: inputs `name`, select `mode` (hide/show), inputs `older_than`, `younger_than`, `prefix`, `suffix`, `contains`, `regex`, select `scope` (global/repo, default repo when `d.repo_config_path` is set), buttons **save** / **cancel**. Save → `postJSON("/api/settings", { branch_filters: [{ slot, scope, name, mode, older_than, younger_than, prefix, suffix, contains, regex }] })`; on error show it in the form's `.serr`; on success `state.settings = await getJSON("/api/settings")` and `renderSettings({ fresh: true })`.
- `bf-remove`: `postJSON` with `[{ slot, scope: "repo", remove: true }, { slot, scope: "global", remove: true }]` (both scopes, like the TUI's `d`), then refresh.

Also after any successful save/remove, refetch the sidebar lists (`window.__ggRefetchList("branches")` and `("remotes")`) so an active slot whose rule changed re-evaluates.

- [ ] **Step 5: Run + probe**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/branch-filters && go test ./internal/web/ 2>&1 | tail -5`
Expected: PASS. Extend the Task 8 Playwright script: open settings, edit slot 5 (suffix `-wip`, scope repo), save, confirm the branches chip menu now lists "5 wip".

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/branch-filters && gofmt -l internal/web && git add internal/web && git commit -q -m "feat(web): settings → branch filters — list the five slots, edit one (global or repo), remove; POST /api/settings validates every block before writing" -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01BqyuQk8TCehWj3QfkHUrAM"
```

---

### Task 10: Docs, full gate, verify binary

**Files:**
- Modify: `CHANGELOG.md` (top of Unreleased), `README.md` (branches section / key table), `docs/CLAUDE-details.md` (new `### Branch filters` section: decision rulings, memo key, exemptions, overlay exception, the ctrl+digit fact), `CLAUDE.md` (package map: add the `branchfilter` row; one line), `docs/web-tui-parity.md` if it lists per-list controls.

- [ ] **Step 1: CHANGELOG entry**

```markdown
- **Added: branch filters — five configurable slots on the Branches and Remotes
  lists (`alt+1…5`).** A slot is a named rule in `.gg.toml` (`[[branches.filter]]`,
  global or repo — a repo block replaces the global one for the same slot) that
  hides, or shows only, branches by tip age (`older_than`/`younger_than`: 90d 12w
  6m 1y) and name (prefix, suffix, contains, Go RE2 regex); set clauses AND
  together. One slot is active per list (the same key clears it), remembered per
  repo in `prompts.toml` and shared by the TUI and `gg web`. HEAD, branches
  checked out in a worktree and the current upstream are never hidden (a dim `∗`
  marks one the rule would hide). The panel header reads `▽2 stale · 12 hidden`;
  `/` stacks on top. Edit the slots from Settings `,` → Branch filters… or the
  web settings view; an invalid block is inert with its reason, never an error.
  Web: a `▽` chip on each list header, `alt+1…5` (branches) / `alt+shift+1…5`
  (remotes); remotes filter before the 100-row cap. Not `ctrl+1…5`: terminals
  deliver ctrl+3 as ESC and browsers reserve ctrl+digit for tab switching.
```

- [ ] **Step 2: CLAUDE.md package-map row** (one line, after `changeset`):

`| \`branchfilter\` | Pure five-slot branch-filter model: \`Slot\` (TOML shape), \`Compile\` (RE2 + age parse, inert-with-reason), \`Apply\` (hide / show-only + exempt rows) — the ONE matcher the TUI and web share. DAG leaf. |`

- [ ] **Step 3: CLAUDE-details section** — record: `[[branches.filter]]` lives under `[branches]` because a bare array-of-tables name collides with populate's `[section]` header; overlay by slot (second exception after tools); memo key = (slot, len, first/last name+time); exemptions; remotes sort→filter→cap; active slot per repo per list in promptstate keyed by git common dir; web keys on `e.code`; the ctrl+digit terminal/browser facts; rulings from the spec's last section.

- [ ] **Step 4: Full gate**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/branch-filters && ./test.sh race 2>&1 | tail -15`
Expected: vet+gofmt clean, unit + e2e PASS. (The controller runs this; implementers run package tests only.)

- [ ] **Step 5: Verify binary**

`cd /mnt/t/others/gigagit/.claude/worktrees/branch-filters && ./build.sh linux` (and `./build.sh web` for the Windows web exe), then hand the user the absolute path of the built `gg` (SendUserFile + path), per the standing rule.

- [ ] **Step 6: Commit docs**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/branch-filters && git add CHANGELOG.md README.md docs/CLAUDE-details.md CLAUDE.md docs/web-tui-parity.md && git commit -q -m "docs: branch filters — changelog, README, CLAUDE-details rulings, package map" -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01BqyuQk8TCehWj3QfkHUrAM"
```

Then hand off: the user merges (`--no-ff`, the merge-message convention), runs `./build.sh install`.
