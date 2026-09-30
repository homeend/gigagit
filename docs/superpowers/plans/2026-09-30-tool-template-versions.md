# Versioned external-tool templates — Implementation Plan

> **For agentic workers:** Executed by THIS session only (project rule: never
> subagents) via superpowers:executing-plans. Steps use checkbox (`- [ ]`)
> syntax for tracking. Work in `.claude/worktrees/tool-template-versions`
> (branch `feat/tool-template-versions`); every command uses absolute paths.

**Goal:** Config blocks written from the external-tool catalog carry a
template stamp; gg detects blocks that fall behind the catalog or the
installed agent's version and offers the new template — never writing
without consent.

**Architecture:** `exttool` gains template versions, agent-version ranges on
variants, and a version parser. `config` gains the stamp keys, a normalised
fingerprint, and a one-block in-place writer. `domain` probes agent
versions, computes per-block status, and applies an accepted update. The
TUI (Settings → External tools + a notice + a review popup) and the web
tools overlay render the status and collect the answer.

**Tech Stack:** Go 1.26, pelletier/go-toml/v2, Bubble Tea, vanilla JS (web).

**Spec:** `docs/superpowers/specs/2026-09-30-tool-template-versions-design.md`

## Global Constraints

- No block is ever written without the user's consent — not even a stamp on an unstamped block (spec rulings 1, 3).
- Overlapping variant ranges within one family are forbidden, enforced by a catalog test (ruling 2).
- The review popup shows the offer reason + the new template's full text; no diff, no old template texts (ruling 4).
- Fingerprint = normalised parsed fields (`mode`, `per_file`, `when_op`, sorted `frontends`, `command`); whitespace/CRLF/blank lines/comments/key order never count.
- Unknown agent version ⇒ never an offer based on version; probe timeout 3 s, stdin closed.
- Every user-visible TUI string goes through `i18n.T` with a literal key present in `internal/i18n/lang/{ja,ko,ru,zh}.toml`.
- `internal/tui` never imports `internal/git`; config writes live in `domain` (as the review-command upgrade and `Ensure*Commands` already do).
- Tests call `t.Parallel()` where they don't `t.Setenv`; any test reaching config/preflight pins `XDG_CONFIG_HOME` to a temp dir.

## Deviations from the spec (decided while planning — flag in review)

1. **Flat variant encoding.** Instead of a nested `Variants []Variant`, a
   variant is a `CommandTemplate` row with a `Range`; rows sharing
   `(Category, Name)` form one family. Same semantics, and every existing
   consumer of `CommandTemplate` fields keeps compiling; consumers switch
   from `det.Tool.Commands` to `exttool.Pick(...)`.
2. **No engine op for the write.** Config writes already live in `domain`
   (`EnsureSessionCommands`, the review upgrade); `UpdateToolCommand` becomes
   `domain.ApplyToolUpdate`, not an engine `Operation`.
3. **The e2e scenario becomes a TUI test.** The e2e harness drives the CLI
   only, and this feature has no CLI surface; the "take new → conflict picker
   offers it" check moves to a TUI `Update`-driven test (Task 9).
4. **Web answers:** Take new / Keep mine only (the web has no `$EDITOR`).

## Review Focus

1. **Block in the REPO config file** (`.gg.toml` / private repo file) — the update must rewrite that file, not append to the global one. Pinned in Task 4 (`ReplaceToolCommand` on the given path) and Task 6 (`path` in status).
2. **Same key in global AND repo config** — only the effective (repo) block gets a status; the shadowed global one never produces a second offer. Pinned in Task 6.
3. **Multi-variant family, version unknown, wizard install** — the wizard must still offer ONE row (the newest variant), not one per variant or none. Pinned in Task 2 (`Pick` with `known=false`).
4. **CRLF config files on Windows** — `ReplaceToolCommand` keeps the file's line endings, and a CRLF block fingerprints equal to its LF self. Pinned in Tasks 3 and 4.
5. **Command holding `'''`** — the replace writer refuses (TOML literal delimiter) instead of corrupting the file. Pinned in Task 4.

---

## File structure

| File | Responsibility |
|---|---|
| `internal/exttool/agentver.go` (new) | `Version`, `ParseVersion`, `Range`, `ParseRange`, `Range.Contains`, `ExtractVersion` |
| `internal/exttool/family.go` (new) | `Pick`, `Families`, `FamilyVersion`, catalog defaults (`Version` 0→1) |
| `internal/exttool/exttool.go` | `CommandTemplate.Version/Range`, `Tool.VersionArgs/VersionRe` |
| `internal/exttool/testdata/catalog_versions.golden` (new) | version-bump guard |
| `internal/config/tools.go` | stamp fields, `ToolFingerprint`, `renderToolCommand`, `ReplaceToolCommand` |
| `internal/domain/agentver.go` (new) | cached `AgentVersion` probe |
| `internal/domain/tooltemplates.go` (new) | `NewToolBlock`, `ToolTemplateStatuses`, `ApplyToolUpdate` |
| `internal/promptstate/*` | `DeclinedToolUpdates` / `DeclineToolUpdate` |
| `internal/tui/settings_tools.go`, `settings_popup.go` | stamped writes, status suffix, `u` key |
| `internal/tui/tool_update_popup.go` (new) | review popup |
| `internal/tui/notify.go` | tool-template notice |
| `internal/web/exttools.go`, `static/exttools.js`, `server.go` | status + two POST routes + dialog |

---

### Task 1: Agent version + range primitives (`exttool`)

**Files:**
- Create: `internal/exttool/agentver.go`
- Test: `internal/exttool/agentver_test.go`

**Interfaces:**
- Produces: `type Version [3]int`; `ParseVersion(s string) (Version, bool)`;
  `type Range struct{ Min, Max Version; HasMin, HasMax bool }`;
  `ParseRange(s string) (Range, error)`; `(Range) Contains(v Version) bool`;
  `(Range) Overlaps(o Range) bool`; `(Range) String() string`;
  `ExtractVersion(out []byte, re *regexp.Regexp) (Version, bool)`.

- [ ] **Step 1: Write the failing test**

```go
package exttool

import (
	"regexp"
	"testing"
)

func TestParseVersion(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]Version{"2.1": {2, 1, 0}, "2.1.7": {2, 1, 7}, "10.0.1": {10, 0, 1}} {
		got, ok := ParseVersion(in)
		if !ok || got != want {
			t.Errorf("ParseVersion(%q) = %v,%v want %v", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "2", "x.y", "2..1"} {
		if _, ok := ParseVersion(bad); ok {
			t.Errorf("ParseVersion(%q) ok, want failure", bad)
		}
	}
}

func TestRangeContains(t *testing.T) {
	t.Parallel()
	r, err := ParseRange(">=1.8 <2.1")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[Version]bool{{1, 7, 9}: false, {1, 8, 0}: true, {2, 0, 99}: true, {2, 1, 0}: false}
	for v, want := range cases {
		if got := r.Contains(v); got != want {
			t.Errorf("%v in %q = %v, want %v", v, ">=1.8 <2.1", got, want)
		}
	}
	any, _ := ParseRange("")
	if !any.Contains(Version{0, 0, 1}) {
		t.Error(`"" must contain every version`)
	}
	for _, bad := range []string{">2", "<=3", ">=a", ">=2 >=3"} {
		if _, err := ParseRange(bad); err == nil {
			t.Errorf("ParseRange(%q) accepted", bad)
		}
	}
}

func TestRangeOverlaps(t *testing.T) {
	t.Parallel()
	mk := func(s string) Range { r, _ := ParseRange(s); return r }
	if mk(">=1.8 <2.1").Overlaps(mk(">=2.1")) {
		t.Error("adjacent half-open ranges must not overlap")
	}
	if !mk(">=1.8 <2.2").Overlaps(mk(">=2.1")) {
		t.Error("[1.8,2.2) and [2.1,∞) overlap")
	}
	if !mk("").Overlaps(mk(">=9")) {
		t.Error(`"" overlaps everything`)
	}
}

func TestExtractVersion(t *testing.T) {
	t.Parallel()
	v, ok := ExtractVersion([]byte("2.1.7 (Claude Code)\n"), nil)
	if !ok || v != (Version{2, 1, 7}) {
		t.Fatalf("default re: %v %v", v, ok)
	}
	re := regexp.MustCompile(`junie ([0-9.]+)`)
	v, ok = ExtractVersion([]byte("build 99.1\njunie 4.2\n"), re)
	if !ok || v != (Version{4, 2, 0}) {
		t.Fatalf("custom re: %v %v", v, ok)
	}
	if _, ok := ExtractVersion([]byte("no digits"), nil); ok {
		t.Fatal("garbage parsed")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/exttool/ -run 'TestParseVersion|TestRange|TestExtractVersion'` (from the worktree)
Expected: FAIL — `undefined: Version`.

- [ ] **Step 3: Implement**

```go
package exttool

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Version is an agent's own version, major.minor.patch (a missing patch is 0).
type Version [3]int

// ParseVersion reads "X.Y" or "X.Y.Z" (decimal components only).
func ParseVersion(s string) (Version, bool) {
	parts := strings.Split(s, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return Version{}, false
	}
	var v Version
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return Version{}, false
		}
		v[i] = n
	}
	return v, true
}

func (v Version) String() string { return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2]) }

func (v Version) less(o Version) bool {
	for i := range v {
		if v[i] != o[i] {
			return v[i] < o[i]
		}
	}
	return false
}

// Range is a half-open agent-version range: [Min, Max). The zero Range
// ("" in the catalog) holds every version.
type Range struct {
	Min, Max       Version
	HasMin, HasMax bool
	src            string
}

// ParseRange reads a space-separated AND of at most one ">=X.Y[.Z]" and at
// most one "<X.Y[.Z]"; "" is every version.
func ParseRange(s string) (Range, error) {
	r := Range{src: strings.TrimSpace(s)}
	for _, f := range strings.Fields(s) {
		var op, rest string
		switch {
		case strings.HasPrefix(f, ">="):
			op, rest = ">=", f[2:]
		case strings.HasPrefix(f, "<") && !strings.HasPrefix(f, "<="):
			op, rest = "<", f[1:]
		default:
			return Range{}, fmt.Errorf("exttool: range %q: want >=X.Y or <X.Y, got %q", s, f)
		}
		v, ok := ParseVersion(rest)
		if !ok {
			return Range{}, fmt.Errorf("exttool: range %q: bad version %q", s, rest)
		}
		if op == ">=" {
			if r.HasMin {
				return Range{}, fmt.Errorf("exttool: range %q: two lower bounds", s)
			}
			r.Min, r.HasMin = v, true
		} else {
			if r.HasMax {
				return Range{}, fmt.Errorf("exttool: range %q: two upper bounds", s)
			}
			r.Max, r.HasMax = v, true
		}
	}
	return r, nil
}

// String is the range as written in the catalog ("" = any).
func (r Range) String() string { return r.src }

// Contains reports Min <= v < Max.
func (r Range) Contains(v Version) bool {
	if r.HasMin && v.less(r.Min) {
		return false
	}
	if r.HasMax && !v.less(r.Max) {
		return false
	}
	return true
}

// Overlaps reports whether some version lies in both half-open ranges.
func (r Range) Overlaps(o Range) bool {
	// r lies wholly below o, or o wholly below r.
	if r.HasMax && o.HasMin && !o.Min.less(r.Max) {
		return false
	}
	if o.HasMax && r.HasMin && !r.Min.less(o.Max) {
		return false
	}
	return true
}

var defaultVersionRe = regexp.MustCompile(`(\d+\.\d+(?:\.\d+)?)`)

// ExtractVersion finds the first version in an agent's --version output; re's
// first capture group names it (nil = the first X.Y[.Z] anywhere).
func ExtractVersion(out []byte, re *regexp.Regexp) (Version, bool) {
	if re == nil {
		re = defaultVersionRe
	}
	m := re.FindSubmatch(out)
	if len(m) < 2 {
		return Version{}, false
	}
	return ParseVersion(string(m[1]))
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/exttool/ -run 'TestParseVersion|TestRange|TestExtractVersion'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git -C /mnt/t/others/gigagit/.claude/worktrees/tool-template-versions add internal/exttool/agentver.go internal/exttool/agentver_test.go
git -C /mnt/t/others/gigagit/.claude/worktrees/tool-template-versions commit -m "feat(exttool): agent version + half-open version range primitives"
```

---

### Task 2: Catalog families — `Version`, `Range`, `Pick`, invariants

**Files:**
- Modify: `internal/exttool/exttool.go` (`CommandTemplate`, `Tool`, `Builtins` return)
- Create: `internal/exttool/family.go`
- Test: `internal/exttool/family_test.go`
- Modify (consumers, `det.Tool.Commands` → `exttool.Pick(det.Tool, exttool.Version{}, false)`):
  `internal/tui/settings_tools.go:36`, `internal/web/exttools.go:93`,
  `internal/domain/sessions.go:107`, `internal/domain/task_modes.go:112`

**Interfaces:**
- Consumes: Task 1 `Version`, `Range`, `ParseRange`.
- Produces: `CommandTemplate.Version int`, `CommandTemplate.Range string`;
  `Tool.VersionArgs []string`, `Tool.VersionRe *regexp.Regexp`;
  `Pick(tl Tool, v Version, known bool) []CommandTemplate` (one row per
  family, catalog order); `Variant(tl Tool, category Category, name string, v Version, known bool, stampRange string) (CommandTemplate, bool)`;
  `FamilyVersion(tl Tool, category Category, name string) int`;
  `HasRanged(tl Tool) bool`; `func (t CommandTemplate) Key() string` (category + "\x00" + name, same shape as `config.ToolCommand.Key`).

- [ ] **Step 1: Write the failing test**

```go
package exttool

import "testing"

func testTool() Tool {
	return Tool{ID: "t", Commands: []CommandTemplate{
		{Category: CatReview, Name: "A", Version: 3, Range: ">=2.1", Command: "<bin> new"},
		{Category: CatReview, Name: "A", Version: 3, Range: ">=1.8 <2.1", Command: "<bin> old"},
		{Category: CatConflict, Name: "B", Version: 1, Command: "<bin> b"},
	}}
}

func TestPickKnownVersion(t *testing.T) {
	t.Parallel()
	got := Pick(testTool(), Version{2, 0, 0}, true)
	if len(got) != 2 || got[0].Command != "<bin> old" || got[1].Name != "B" {
		t.Fatalf("Pick(2.0) = %+v", got)
	}
}

func TestPickUnknownVersionTakesNewestVariant(t *testing.T) {
	t.Parallel()
	got := Pick(testTool(), Version{}, false)
	if len(got) != 2 || got[0].Command != "<bin> new" {
		t.Fatalf("Pick(unknown) = %+v", got)
	}
}

func TestPickOutOfRangeDropsFamily(t *testing.T) {
	t.Parallel()
	got := Pick(testTool(), Version{1, 0, 0}, true)
	if len(got) != 1 || got[0].Name != "B" {
		t.Fatalf("Pick(1.0) = %+v", got)
	}
}

func TestVariantUnknownUsesStampRange(t *testing.T) {
	t.Parallel()
	ct, ok := Variant(testTool(), CatReview, "A", Version{}, false, ">=1.8 <2.1")
	if !ok || ct.Command != "<bin> old" {
		t.Fatalf("stamp range: %+v %v", ct, ok)
	}
	if _, ok := Variant(testTool(), CatReview, "A", Version{}, false, ""); ok {
		t.Fatal("multi-variant family, unknown version, no stamp: must be no variant")
	}
}

// The catalog invariants (spec: overlapping ranges forbidden; "" alone).
func TestBuiltinsFamilyInvariants(t *testing.T) {
	t.Parallel()
	for _, tl := range Builtins() {
		fams := map[string][]CommandTemplate{}
		for _, ct := range tl.Commands {
			fams[ct.Key()] = append(fams[ct.Key()], ct)
		}
		for key, vs := range fams {
			for i, a := range vs {
				ra, err := ParseRange(a.Range)
				if err != nil {
					t.Fatalf("%s %q: %v", tl.ID, key, err)
				}
				if a.Version < 1 {
					t.Errorf("%s %q: Version %d < 1", tl.ID, key, a.Version)
				}
				if len(vs) > 1 && a.Range == "" {
					t.Errorf("%s %q: an any-version variant must be the family's only one", tl.ID, key)
				}
				for _, b := range vs[i+1:] {
					rb, _ := ParseRange(b.Range)
					if ra.Overlaps(rb) {
						t.Errorf("%s %q: ranges %q and %q overlap", tl.ID, key, a.Range, b.Range)
					}
					if a.Version != b.Version {
						t.Errorf("%s %q: variants disagree on Version", tl.ID, key)
					}
				}
			}
		}
		if HasRanged(tl) && tl.VersionArgs == nil {
			t.Errorf("%s has ranged variants but no VersionArgs", tl.ID)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/exttool/ -run 'TestPick|TestVariant|TestBuiltinsFamily'`
Expected: FAIL — unknown fields `Version`, `Range`; undefined `Pick`.

- [ ] **Step 3: Implement**

In `exttool.go`, add to `CommandTemplate` (after `Frontends`):

```go
	// Version is the family's template revision — the family is every row
	// sharing (Category, Name). Bump it on ANY change to the family; the
	// golden guard (catalog_versions.golden) fails the build otherwise.
	// 0 in a literal = 1 (Builtins fills it).
	Version int
	// Range is the agent-version range this variant serves ("" = any; see
	// ParseRange). Ranges within a family never overlap.
	Range string
```

and to `Tool` (after `ExtraProbes`):

```go
	// VersionArgs reads the agent's own version (e.g. {"--version"}); nil =
	// never probed, which only a tool without ranged variants may have.
	VersionArgs []string
	// VersionRe's first group is the version in that output (nil = the first X.Y[.Z]).
	VersionRe *regexp.Regexp
```

Rename the body of `Builtins()` to `builtins()` and add:

```go
func Builtins() []Tool { return withDefaults(builtins()) }
```

Create `family.go`:

```go
package exttool

// Key identifies a template's family — the config block key shape.
func (t CommandTemplate) Key() string { return string(t.Category) + "\x00" + t.Name }

// withDefaults fills Version 0 → 1 so only a bumped family spells its version.
func withDefaults(tools []Tool) []Tool {
	for i := range tools {
		for j := range tools[i].Commands {
			if tools[i].Commands[j].Version == 0 {
				tools[i].Commands[j].Version = 1
			}
		}
	}
	return tools
}

// HasRanged reports whether any variant of tl names a version range.
func HasRanged(tl Tool) bool {
	for _, ct := range tl.Commands {
		if ct.Range != "" {
			return true
		}
	}
	return false
}

// FamilyVersion is the family's template version (0 = no such family).
func FamilyVersion(tl Tool, category Category, name string) int {
	for _, ct := range tl.Commands {
		if ct.Category == category && ct.Name == name {
			return ct.Version
		}
	}
	return 0
}

// Variant picks the family's variant for an agent version. A known version
// picks the range holding it (none = unsupported). An unknown one picks the
// variant whose Range equals stampRange, else the family's only variant.
func Variant(tl Tool, category Category, name string, v Version, known bool, stampRange string) (CommandTemplate, bool) {
	var fam []CommandTemplate
	for _, ct := range tl.Commands {
		if ct.Category == category && ct.Name == name {
			fam = append(fam, ct)
		}
	}
	if known {
		for _, ct := range fam {
			if r, err := ParseRange(ct.Range); err == nil && r.Contains(v) {
				return ct, true
			}
		}
		return CommandTemplate{}, false
	}
	for _, ct := range fam {
		if ct.Range == stampRange && (stampRange != "" || len(fam) == 1) {
			return ct, true
		}
	}
	return CommandTemplate{}, false
}

// Pick is the catalog as offered for one agent version: one row per family,
// in catalog order. Unknown version = each family's newest variant (the one
// with the highest lower bound), so the wizard still offers every family.
func Pick(tl Tool, v Version, known bool) []CommandTemplate {
	var out []CommandTemplate
	seen := map[string]int{}
	for _, ct := range tl.Commands {
		r, err := ParseRange(ct.Range)
		if err != nil {
			continue
		}
		if known && !r.Contains(v) {
			continue
		}
		i, dup := seen[ct.Key()]
		if !dup {
			seen[ct.Key()] = len(out)
			out = append(out, ct)
			continue
		}
		have, _ := ParseRange(out[i].Range)
		if have.Min.less(r.Min) {
			out[i] = ct
		}
	}
	return out
}
```

Switch the four consumers: `for _, ct := range det.Tool.Commands {` →
`for _, ct := range exttool.Pick(det.Tool, exttool.Version{}, false) {`
(Task 7 replaces the TUI/web ones with a version-aware call; with every
catalog row at `Range: ""` today this is behaviour-neutral).

Also give every AI tool in `builtins()` `VersionArgs: []string{"--version"}`
(claude, junie, codex, agy, kimi) — mergetools (meld, …) keep nil.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/exttool/ ./internal/domain/ ./internal/tui/ ./internal/web/ -run 'TestPick|TestVariant|TestBuiltinsFamily|Tool|Session|Interactive'`
Expected: PASS; `go build ./...` clean.

- [ ] **Step 5: Commit**

```bash
git -C /mnt/t/others/gigagit/.claude/worktrees/tool-template-versions add -A internal/exttool internal/tui/settings_tools.go internal/web/exttools.go internal/domain/sessions.go internal/domain/task_modes.go
git -C /mnt/t/others/gigagit/.claude/worktrees/tool-template-versions commit -m "feat(exttool): template families with versions and agent-version variants"
```

---

### Task 3: Config stamp + normalised fingerprint (`config`)

**Files:**
- Modify: `internal/config/tools.go`
- Test: `internal/config/tools_stamp_test.go`

**Interfaces:**
- Produces: `ToolCommand.TemplateVersion int` (`toml:"template_version"`),
  `ToolCommand.AgentRange string` (`toml:"agent_range"`),
  `ToolCommand.Fingerprint string` (`toml:"fingerprint"`);
  `ToolFingerprint(tc ToolCommand) string` ("sha256:" + hex);
  `(ToolCommand) Stamped() bool` (TemplateVersion > 0);
  `(ToolCommand) Edited() bool` (!Stamped() || ToolFingerprint(tc) != tc.Fingerprint);
  `AppendToolCommands` writes the three stamp keys for a stamped block and
  computes `fingerprint` itself (callers set only TemplateVersion/AgentRange).

- [ ] **Step 1: Write the failing test**

```go
package config

import (
	"path/filepath"
	"testing"
)

func TestToolFingerprintIgnoresFormatting(t *testing.T) {
	t.Parallel()
	a := ToolCommand{Mode: "capture", Frontends: []string{"tui", "web"}, Command: "claude -p \\\n  \"x  y\"\n"}
	b := ToolCommand{Mode: "capture", Frontends: []string{"web", "tui"}, Command: "claude   -p \\\r\n\r\n\t\"x y\"  "}
	if ToolFingerprint(a) != ToolFingerprint(b) {
		t.Fatal("formatting-only differences must fingerprint equal")
	}
	c := a
	c.Command = "claude -p --dangerously-skip-permissions"
	if ToolFingerprint(a) == ToolFingerprint(c) {
		t.Fatal("a flag change must change the fingerprint")
	}
	d := a
	d.Frontends = []string{"web"}
	if ToolFingerprint(a) == ToolFingerprint(d) {
		t.Fatal("a frontends change must change the fingerprint")
	}
}

func TestAppendWritesStampThatRoundTrips(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	in := ToolCommand{Category: "review", Name: "R", Mode: "capture", Command: "x --y", TemplateVersion: 3, AgentRange: ">=2.1"}
	if err := AppendToolCommands(path, []ToolCommand{in}); err != nil {
		t.Fatal(err)
	}
	got, err := ToolCommandsIn(path)
	if err != nil || len(got) != 1 {
		t.Fatalf("read back: %v %+v", err, got)
	}
	tc := got[0]
	if tc.TemplateVersion != 3 || tc.AgentRange != ">=2.1" || tc.Fingerprint == "" {
		t.Fatalf("stamp lost: %+v", tc)
	}
	if tc.Edited() {
		t.Fatal("a freshly written block must read as not edited")
	}
}

func TestUnstampedBlockHasNoStampKeys(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := AppendToolCommands(path, []ToolCommand{{Category: "review", Name: "U", Mode: "capture", Command: "x"}}); err != nil {
		t.Fatal(err)
	}
	got, _ := ToolCommandsIn(path)
	if got[0].Stamped() || !got[0].Edited() {
		t.Fatalf("hand-authored block: %+v", got[0])
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/config/ -run 'TestToolFingerprint|TestAppendWritesStamp|TestUnstamped'`
Expected: FAIL — unknown field `TemplateVersion`.

- [ ] **Step 3: Implement**

Add fields to `ToolCommand` (after `Command`):

```go
	// The template stamp (written only on blocks gg generated from the
	// catalog): the family's template version, the variant's agent range,
	// and ToolFingerprint of the block as written — a later mismatch means
	// the user edited it. Older gg binaries ignore these keys.
	TemplateVersion int    `toml:"template_version"`
	AgentRange      string `toml:"agent_range"`
	Fingerprint     string `toml:"fingerprint"`
```

Add below `Key()` (imports: `crypto/sha256`, `encoding/hex`, `sort`, `strconv`):

```go
// Stamped reports a block gg generated from the catalog.
func (tc ToolCommand) Stamped() bool { return tc.TemplateVersion > 0 }

// Edited reports a block whose content no longer matches its stamp; an
// unstamped block always counts as edited.
func (tc ToolCommand) Edited() bool { return !tc.Stamped() || ToolFingerprint(tc) != tc.Fingerprint }

// ToolFingerprint hashes a block's MEANING, not its text: mode, per_file,
// when_op, sorted frontends and the command with line ends normalised,
// every run of spaces/tabs collapsed, lines trimmed and blank lines dropped.
func ToolFingerprint(tc ToolCommand) string {
	fr := append([]string(nil), tc.Frontends...)
	sort.Strings(fr)
	var lines []string
	for _, ln := range strings.Split(strings.ReplaceAll(tc.Command, "\r\n", "\n"), "\n") {
		if ln = strings.Join(strings.Fields(ln), " "); ln != "" {
			lines = append(lines, ln)
		}
	}
	h := sha256.New()
	for _, part := range []string{tc.Mode, strconv.FormatBool(tc.PerFile), tc.WhenOp, strings.Join(fr, ","), strings.Join(lines, "\n")} {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
```

Extract the block writer out of `AppendToolCommands` so Task 4 reuses it:

```go
// renderToolCommand writes one [[tools.command]] block (no leading blank
// line). A stamped block gets its three stamp keys, the fingerprint
// computed here from the block being written.
func renderToolCommand(b *strings.Builder, tc ToolCommand) {
	fmt.Fprintf(b, "[[tools.command]]\n")
	fmt.Fprintf(b, "category = %q\n", tc.Category)
	fmt.Fprintf(b, "name = %q\n", tc.Name)
	fmt.Fprintf(b, "mode = %q\n", tc.Mode)
	fmt.Fprintf(b, "per_file = %t\n", tc.PerFile)
	fmt.Fprintf(b, "when_op = %q\n", tc.WhenOp)
	if len(tc.Frontends) > 0 {
		quoted := make([]string, len(tc.Frontends))
		for i, f := range tc.Frontends {
			quoted[i] = fmt.Sprintf("%q", f)
		}
		fmt.Fprintf(b, "frontends = [%s]\n", strings.Join(quoted, ", "))
	}
	if tc.Stamped() {
		fmt.Fprintf(b, "template_version = %d\n", tc.TemplateVersion)
		fmt.Fprintf(b, "agent_range = %q\n", tc.AgentRange)
		fmt.Fprintf(b, "fingerprint = %q\n", ToolFingerprint(tc))
	}
	b.WriteString("command = '''\n")
	b.WriteString(strings.TrimRight(tc.Command, "\n"))
	b.WriteString("\n'''\n")
}
```

and replace the per-block `Fprintf` run inside `AppendToolCommands`' loop
with `renderToolCommand(&b, tc)`.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/config/`
Expected: PASS (existing `AppendToolCommands` tests unchanged — unstamped output is byte-identical).

- [ ] **Step 5: Commit**

```bash
git -C /mnt/t/others/gigagit/.claude/worktrees/tool-template-versions add internal/config/tools.go internal/config/tools_stamp_test.go
git -C /mnt/t/others/gigagit/.claude/worktrees/tool-template-versions commit -m "feat(config): template stamp keys and a normalised block fingerprint"
```

---

### Task 4: One-block in-place writer `ReplaceToolCommand`

**Files:**
- Modify: `internal/config/tools.go`
- Test: `internal/config/tools_replace_test.go`

**Interfaces:**
- Consumes: Task 3 `renderToolCommand`.
- Produces: `ReplaceToolCommand(path, key string, tc ToolCommand) (bool, error)`
  — replaces the whole `[[tools.command]]` block whose (category, name)
  equals `key` (the `ToolCommand.Key()` shape), keeps every other byte and
  the file's line ending; `false, nil` when no such block.

- [ ] **Step 1: Write the failing test**

```go
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const replaceFixture = `# my tools
[ui]
theme = "dark"

[[tools.command]]
category = "review"
name = "A"
mode = "capture"
frontends = ["web"]
command = '''
old a
'''

# keep me
[[tools.command]]
category = "review"
name = "B"
mode = "capture"
command = '''
b
'''
`

func TestReplaceToolCommandRewritesOnlyThatBlock(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "c.toml")
	os.WriteFile(path, []byte(replaceFixture), 0o644)
	nb := ToolCommand{Category: "review", Name: "A", Mode: "capture", Frontends: []string{"tui", "web"}, Command: "new a", TemplateVersion: 2}
	ok, err := ReplaceToolCommand(path, nb.Key(), nb)
	if err != nil || !ok {
		t.Fatalf("replace: %v %v", ok, err)
	}
	raw, _ := os.ReadFile(path)
	s := string(raw)
	for _, want := range []string{"# my tools", "theme = \"dark\"", "# keep me", "name = \"B\"", "new a", "template_version = 2"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
	if strings.Contains(s, "old a") {
		t.Errorf("old body survived:\n%s", s)
	}
	got, _ := ToolCommandsIn(path)
	if len(got) != 2 || got[0].Name != "A" || got[0].Edited() {
		t.Fatalf("decoded: %+v", got)
	}
}

func TestReplaceToolCommandKeepsCRLF(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "c.toml")
	os.WriteFile(path, []byte(strings.ReplaceAll(replaceFixture, "\n", "\r\n")), 0o644)
	nb := ToolCommand{Category: "review", Name: "A", Mode: "capture", Command: "new a", TemplateVersion: 2}
	if _, err := ReplaceToolCommand(path, nb.Key(), nb); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(strings.ReplaceAll(string(raw), "\r\n", ""), "\n") {
		t.Fatal("a bare LF crept into a CRLF file")
	}
}

func TestReplaceToolCommandRefusesDelimiterAndMissing(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "c.toml")
	os.WriteFile(path, []byte(replaceFixture), 0o644)
	bad := ToolCommand{Category: "review", Name: "A", Command: "x ''' y"}
	if _, err := ReplaceToolCommand(path, bad.Key(), bad); err == nil {
		t.Fatal("a ''' body must be refused")
	}
	miss := ToolCommand{Category: "review", Name: "Z", Command: "z"}
	if ok, err := ReplaceToolCommand(path, miss.Key(), miss); ok || err != nil {
		t.Fatalf("missing key: %v %v", ok, err)
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != replaceFixture {
		t.Fatal("a refused/missed replace must not touch the file")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/config/ -run TestReplaceToolCommand`
Expected: FAIL — `undefined: ReplaceToolCommand`.

- [ ] **Step 3: Implement**

```go
// ReplaceToolCommand rewrites, in place, the one [[tools.command]] block
// whose (category, name) is key with tc, leaving every other byte of the
// file alone and keeping its line ending. A block spans from its header to
// the line before the next table header, minus trailing blank/comment lines
// (they belong to what follows). false, nil when no block has that key.
func ReplaceToolCommand(path, key string, tc ToolCommand) (bool, error) {
	if strings.Contains(tc.Command, "'''") {
		return false, fmt.Errorf("config: %s: command must not contain ''' (TOML literal delimiter)", tc.Name)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	nl := "\n"
	if strings.Contains(string(raw), "\r\n") {
		nl = "\r\n"
	}
	lines := strings.SplitAfter(string(raw), "\n")
	inLiteral := false
	isHeader := func(l string) bool {
		t := strings.TrimSpace(l)
		return !inLiteral && strings.HasPrefix(t, "[")
	}
	var starts []int
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if isHeader(l) {
			starts = append(starts, i)
		}
		if isCommandLiteralOpen(t) {
			inLiteral = true
		} else if inLiteral && strings.TrimRight(l, "\r\n") == "'''" {
			inLiteral = false
		}
	}
	for si, s := range starts {
		if strings.TrimSpace(lines[s]) != "[[tools.command]]" {
			continue
		}
		end := len(lines)
		if si+1 < len(starts) {
			end = starts[si+1]
		}
		for end > s+1 {
			t := strings.TrimSpace(lines[end-1])
			if t != "" && !strings.HasPrefix(t, "#") {
				break
			}
			end--
		}
		var one struct {
			Tools ToolsConfig `toml:"tools"`
		}
		if err := toml.Unmarshal([]byte(strings.Join(lines[s:end], "")), &one); err != nil || len(one.Tools.Command) != 1 {
			continue
		}
		if one.Tools.Command[0].Key() != key {
			continue
		}
		var b strings.Builder
		renderToolCommand(&b, tc)
		block := strings.ReplaceAll(b.String(), "\n", nl)
		out := strings.Join(lines[:s], "") + block + strings.Join(lines[end:], "")
		return true, atomicWriteFile(path, []byte(out))
	}
	return false, nil
}
```

(Add the `toml "github.com/pelletier/go-toml/v2"` import to `tools.go` if not present.)

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/config/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git -C /mnt/t/others/gigagit/.claude/worktrees/tool-template-versions add internal/config/tools.go internal/config/tools_replace_test.go
git -C /mnt/t/others/gigagit/.claude/worktrees/tool-template-versions commit -m "feat(config): replace one [[tools.command]] block in place"
```

---

### Task 5: Version-bump guard (golden)

**Files:**
- Create: `internal/exttool/version_guard_test.go`
- Create: `internal/exttool/testdata/catalog_versions.golden` (generated)

**Interfaces:**
- Consumes: Task 2 `Builtins`, `Key`; Task 3 `config.ToolFingerprint`
  (test-only import of `internal/config` — `config` does not import
  `exttool`, so no cycle).

- [ ] **Step 1: Write the test (fails: golden missing)**

```go
package exttool

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/config"
)

var updateGolden = flag.Bool("update", false, "rewrite catalog_versions.golden (refuses un-bumped changes)")

// familyPrints is "<tool>\t<category>\t<name>" → (version, fingerprint of all
// variants in catalog order, Range included).
func familyPrints() map[string][2]string {
	out := map[string][2]string{}
	for _, tl := range Builtins() {
		parts := map[string][]string{}
		vers := map[string]int{}
		for _, ct := range tl.Commands {
			k := tl.ID + "\t" + string(ct.Category) + "\t" + ct.Name
			fp := config.ToolFingerprint(config.ToolCommand{Mode: string(ct.Mode), PerFile: ct.PerFile, WhenOp: ct.WhenOp, Frontends: ct.Frontends, Command: ct.Command})
			parts[k] = append(parts[k], ct.Range+"="+fp+fmt.Sprintf(";optin=%t", ct.OptIn))
			vers[k] = ct.Version
		}
		for k, p := range parts {
			out[k] = [2]string{strconv.Itoa(vers[k]), strings.Join(p, "|")}
		}
	}
	return out
}

func TestCatalogVersionBumpGuard(t *testing.T) {
	path := filepath.Join("testdata", "catalog_versions.golden")
	prev := map[string][2]string{}
	if raw, err := os.ReadFile(path); err == nil {
		for _, ln := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			f := strings.Split(ln, "\t")
			if len(f) == 5 {
				prev[f[0]+"\t"+f[1]+"\t"+f[2]] = [2]string{f[3], f[4]}
			}
		}
	} else if !*updateGolden {
		t.Fatalf("%s missing: run go test ./internal/exttool -run TestCatalogVersionBumpGuard -update", path)
	}
	cur := familyPrints()
	for k, c := range cur {
		p, ok := prev[k]
		if !ok || p[1] == c[1] {
			if ok && p[0] != c[0] && p[1] == c[1] && !*updateGolden {
				t.Errorf("%s: version changed %s→%s with no template change", k, p[0], c[0])
			}
			continue
		}
		pv, _ := strconv.Atoi(p[0])
		cv, _ := strconv.Atoi(c[0])
		if cv <= pv {
			t.Errorf("%s: template changed but Version stayed %d — bump it", strings.ReplaceAll(k, "\t", " / "), cv)
		}
	}
	if *updateGolden && !t.Failed() {
		keys := make([]string, 0, len(cur))
		for k := range cur {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		for _, k := range keys {
			fmt.Fprintf(&b, "%s\t%s\t%s\n", k, cur[k][0], cur[k][1])
		}
		if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/exttool/ -run TestCatalogVersionBumpGuard`
Expected: FAIL — golden missing.

- [ ] **Step 3: Generate the golden**

Run: `go test ./internal/exttool/ -run TestCatalogVersionBumpGuard -update`
Expected: PASS; `testdata/catalog_versions.golden` written.

- [ ] **Step 4: Prove the guard bites** (the guard, not the feature)

Temporarily change `claudeCompleteHeadlessCommand` (append ` --x`), run
`go test ./internal/exttool/ -run TestCatalogVersionBumpGuard` → Expected:
FAIL naming `claude / conflict_complete / Claude — resolve & complete (yolo, headless)`
"bump it". Revert the change; rerun → PASS.

- [ ] **Step 5: Commit**

```bash
git -C /mnt/t/others/gigagit/.claude/worktrees/tool-template-versions add internal/exttool/version_guard_test.go internal/exttool/testdata/catalog_versions.golden
git -C /mnt/t/others/gigagit/.claude/worktrees/tool-template-versions commit -m "test(exttool): golden guard — a changed template must bump its Version"
```

---

### Task 6: Domain — version probe, stamped blocks, statuses, apply

**Files:**
- Create: `internal/domain/agentver.go`, `internal/domain/tooltemplates.go`
- Modify: `internal/domain/sessions.go`, `internal/domain/task_modes.go` (use `NewToolBlock`)
- Test: `internal/domain/agentver_test.go`, `internal/domain/tooltemplates_test.go`

**Interfaces:**
- Consumes: Tasks 1–4.
- Produces:
  - `var agentVersionRun = func(ctx context.Context, bin string, args []string) ([]byte, error)` (test seam; real: `exec.CommandContext`, 3 s timeout, `Stdin = nil`);
  - `AgentVersion(ctx context.Context, tl exttool.Tool, bin string) (exttool.Version, bool)` — cached per (LookPath-resolved path, mtime);
  - `NewToolBlock(det exttool.Detection, ct exttool.CommandTemplate) config.ToolCommand` — generated + stamped;
  - `type ToolStatusKind int` with `ToolCurrent`, `ToolCustomised`, `ToolUpdateAvailable`, `ToolUnsupported`;
  - `type ToolTemplateStatus struct { Path string; Block config.ToolCommand; Kind ToolStatusKind; New config.ToolCommand; FromVersion, ToVersion int; AgentVersion string; FromRange, ToRange string; Edited bool; ToolLabel string }` with `(ToolTemplateStatus) OfferKey() string` (= `Block.Key() + "\x00" + config.ToolFingerprint(New)`);
  - `ToolTemplateStatuses(ctx context.Context, paths []string, dets []exttool.Detection) []ToolTemplateStatus` (paths in overlay order: global, then repo; only effective blocks);
  - `(s *Service) ToolTemplateStatuses(ctx context.Context) []ToolTemplateStatus` (global + active repo path, real detection);
  - `ApplyToolUpdate(st ToolTemplateStatus) error` (re-reads the file and refuses if the block changed since `st` was computed).

- [ ] **Step 1: Write the failing tests**

`agentver_test.go`:

```go
package domain

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/exttool"
)

func TestAgentVersionParsesAndCaches(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "fakeagent")
	os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755)
	calls := 0
	old := agentVersionRun
	agentVersionRun = func(ctx context.Context, b string, args []string) ([]byte, error) {
		calls++
		return []byte("fakeagent 2.3.1\n"), nil
	}
	t.Cleanup(func() { agentVersionRun = old; resetAgentVersionCache() })
	tl := exttool.Tool{ID: "fake", VersionArgs: []string{"--version"}}
	for i := 0; i < 2; i++ {
		v, ok := AgentVersion(context.Background(), tl, bin)
		if !ok || v != (exttool.Version{2, 3, 1}) {
			t.Fatalf("got %v %v", v, ok)
		}
	}
	if calls != 1 {
		t.Fatalf("probe ran %d times, want 1 (cached)", calls)
	}
}

func TestAgentVersionUnknownWithoutArgsOrOnError(t *testing.T) {
	old := agentVersionRun
	agentVersionRun = func(context.Context, string, []string) ([]byte, error) { return nil, context.DeadlineExceeded }
	t.Cleanup(func() { agentVersionRun = old; resetAgentVersionCache() })
	if _, ok := AgentVersion(context.Background(), exttool.Tool{ID: "m"}, "meld"); ok {
		t.Fatal("no VersionArgs must be unknown")
	}
	if _, ok := AgentVersion(context.Background(), exttool.Tool{ID: "x", VersionArgs: []string{"--version"}}, "x"); ok {
		t.Fatal("a failing probe must be unknown")
	}
}
```

`tooltemplates_test.go` (table-driven over the spec's status table; uses a
fake catalog tool so tests never depend on the real catalog):

```go
package domain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/exttool"
)

func fakeDet(version int, variants ...exttool.CommandTemplate) exttool.Detection {
	for i := range variants {
		variants[i].Category, variants[i].Name, variants[i].Mode, variants[i].Version = exttool.CatReview, "Fake", exttool.ModeCapture, version
	}
	return exttool.Detection{Tool: exttool.Tool{ID: "fake", Label: "Fake", VersionArgs: []string{"--version"}, Commands: variants}, Bin: "fake"}
}

func stubVersion(t *testing.T, out string) {
	old := agentVersionRun
	agentVersionRun = func(context.Context, string, []string) ([]byte, error) {
		if out == "" {
			return nil, context.DeadlineExceeded
		}
		return []byte(out), nil
	}
	t.Cleanup(func() { agentVersionRun = old; resetAgentVersionCache() })
}

func writeBlocks(t *testing.T, blocks ...config.ToolCommand) string {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := config.AppendToolCommands(path, blocks); err != nil {
		t.Fatal(err)
	}
	return path
}

// userEdit simulates the user editing the file AFTER gg wrote it (writing a
// stamped block always computes a fresh fingerprint, so an "edited" block
// can only be made this way).
func userEdit(t *testing.T, path, from, to string) {
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), from) {
		t.Fatalf("userEdit: %q not in file", from)
	}
	os.WriteFile(path, []byte(strings.Replace(string(raw), from, to, 1)), 0o644)
}

func TestToolTemplateStatusTable(t *testing.T) {
	v1 := fakeDet(1, exttool.CommandTemplate{Command: "<bin> one"})
	v2 := fakeDet(2, exttool.CommandTemplate{Command: "<bin> two"})
	stamped := NewToolBlock(v1, v1.Tool.Commands[0])
	unstampedEqual := NewToolBlock(v2, v2.Tool.Commands[0])
	unstampedEqual.TemplateVersion, unstampedEqual.AgentRange = 0, ""
	unstampedDiff := unstampedEqual
	unstampedDiff.Command = "fake two --mine"

	cases := []struct {
		name   string
		block  config.ToolCommand
		edited bool // apply a user edit to the written file
		det    exttool.Detection
		want   ToolStatusKind
		edit   bool
	}{
		{"stamped current", stamped, false, v1, ToolCurrent, false},
		{"stamped unedited behind", stamped, false, v2, ToolUpdateAvailable, false},
		{"stamped edited current", stamped, true, v1, ToolCustomised, true},
		{"stamped edited behind", stamped, true, v2, ToolUpdateAvailable, true},
		{"unstamped equal to target", unstampedEqual, false, v2, ToolCurrent, true},
		{"unstamped differing", unstampedDiff, false, v2, ToolUpdateAvailable, true},
	}
	stubVersion(t, "")
	for _, c := range cases {
		path := writeBlocks(t, c.block)
		if c.edited {
			userEdit(t, path, "fake one", "fake one --mine")
		}
		sts := ToolTemplateStatuses(context.Background(), []string{path}, []exttool.Detection{c.det})
		if len(sts) != 1 || sts[0].Kind != c.want || sts[0].Edited != c.edit {
			t.Errorf("%s: got %+v", c.name, sts)
		}
	}
}

func TestToolTemplateStatusAgentRanges(t *testing.T) {
	det := fakeDet(1,
		exttool.CommandTemplate{Range: ">=2.1", Command: "<bin> new"},
		exttool.CommandTemplate{Range: ">=1.8 <2.1", Command: "<bin> old"})
	oldBlock := NewToolBlock(det, det.Tool.Commands[1]) // written for <2.1
	path := writeBlocks(t, oldBlock)

	stubVersion(t, "fake 2.3.0")
	st := ToolTemplateStatuses(context.Background(), []string{path}, []exttool.Detection{det})
	if len(st) != 1 || st[0].Kind != ToolUpdateAvailable || st[0].ToRange != ">=2.1" || st[0].AgentVersion != "2.3.0" {
		t.Fatalf("agent upgrade: %+v", st)
	}

	stubVersion(t, "fake 1.0.0")
	st = ToolTemplateStatuses(context.Background(), []string{path}, []exttool.Detection{det})
	if len(st) != 1 || st[0].Kind != ToolUnsupported {
		t.Fatalf("out of range: %+v", st)
	}

	stubVersion(t, "") // unknown: keep the stamped variant, no offer
	st = ToolTemplateStatuses(context.Background(), []string{path}, []exttool.Detection{det})
	if len(st) != 1 || st[0].Kind != ToolCurrent {
		t.Fatalf("unknown version: %+v", st)
	}
}

func TestToolTemplateStatusRepoShadowsGlobal(t *testing.T) {
	v2 := fakeDet(2, exttool.CommandTemplate{Command: "<bin> two"})
	v1 := fakeDet(1, exttool.CommandTemplate{Command: "<bin> one"})
	stubVersion(t, "")
	global := writeBlocks(t, NewToolBlock(v1, v1.Tool.Commands[0]))
	repo := writeBlocks(t, NewToolBlock(v2, v2.Tool.Commands[0]))
	sts := ToolTemplateStatuses(context.Background(), []string{global, repo}, []exttool.Detection{v2})
	if len(sts) != 1 || sts[0].Path != repo || sts[0].Kind != ToolCurrent {
		t.Fatalf("only the effective (repo) block may get a status: %+v", sts)
	}
}

func TestToolTemplateStatusIgnoresUserAuthored(t *testing.T) {
	stubVersion(t, "")
	path := writeBlocks(t, config.ToolCommand{Category: "review", Name: "Mine", Mode: "capture", Command: "x"})
	if sts := ToolTemplateStatuses(context.Background(), []string{path}, []exttool.Detection{fakeDet(1, exttool.CommandTemplate{Command: "<bin>"})}); len(sts) != 0 {
		t.Fatalf("a non-catalog block got a status: %+v", sts)
	}
}

func TestApplyToolUpdateWritesAndRefusesStale(t *testing.T) {
	stubVersion(t, "")
	v1 := fakeDet(1, exttool.CommandTemplate{Command: "<bin> one"})
	v2 := fakeDet(2, exttool.CommandTemplate{Command: "<bin> two"})
	path := writeBlocks(t, NewToolBlock(v1, v1.Tool.Commands[0]))
	st := ToolTemplateStatuses(context.Background(), []string{path}, []exttool.Detection{v2})[0]

	// The user edits the block after the status was computed: refuse.
	raw, _ := os.ReadFile(path)
	os.WriteFile(path, []byte(string(raw)+"\n# touched\n"), 0o644) // formatting only — still allowed
	if err := ApplyToolUpdate(st); err != nil {
		t.Fatalf("formatting-only change must not block: %v", err)
	}
	got, _ := config.ToolCommandsIn(path)
	if got[0].Command != "fake two" || got[0].TemplateVersion != 2 || got[0].Edited() {
		t.Fatalf("after apply: %+v", got[0])
	}
	stale := st
	stale.Block.Command = "something the file no longer holds"
	if err := ApplyToolUpdate(stale); err == nil {
		t.Fatal("a block that changed since the status was computed must be refused")
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/domain/ -run 'TestAgentVersion|TestToolTemplate|TestApplyToolUpdate'`
Expected: FAIL — undefined symbols.

- [ ] **Step 3: Implement `agentver.go`**

```go
package domain

import (
	"context"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/homeend/gigagit/internal/exttool"
)

// agentVersionRun runs `<bin> <args…>` with stdin closed (test seam).
var agentVersionRun = func(ctx context.Context, bin string, args []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdin = nil
	return cmd.Output()
}

type agentVerKey struct {
	path  string
	mtime time.Time
}

type agentVerVal struct {
	v  exttool.Version
	ok bool
}

var (
	agentVerMu    sync.Mutex
	agentVerCache = map[agentVerKey]agentVerVal{}
)

func resetAgentVersionCache() {
	agentVerMu.Lock()
	agentVerCache = map[agentVerKey]agentVerVal{}
	agentVerMu.Unlock()
}

// AgentVersion is the installed agent's own version, probed once per binary
// (resolved path + mtime, so an upgrade mid-session re-probes). Unknown when
// the tool declares no VersionArgs, the probe fails or times out, or the
// output holds no version.
func AgentVersion(ctx context.Context, tl exttool.Tool, bin string) (exttool.Version, bool) {
	if len(tl.VersionArgs) == 0 {
		return exttool.Version{}, false
	}
	path := bin
	if p, err := exec.LookPath(bin); err == nil {
		path = p
	}
	var key agentVerKey
	if fi, err := os.Stat(path); err == nil {
		key = agentVerKey{path, fi.ModTime()}
	} else {
		key = agentVerKey{path: path}
	}
	agentVerMu.Lock()
	if v, hit := agentVerCache[key]; hit {
		agentVerMu.Unlock()
		return v.v, v.ok
	}
	agentVerMu.Unlock()
	out, err := agentVersionRun(ctx, bin, tl.VersionArgs)
	var val agentVerVal
	if err == nil {
		val.v, val.ok = exttool.ExtractVersion(out, tl.VersionRe)
	}
	agentVerMu.Lock()
	agentVerCache[key] = val
	agentVerMu.Unlock()
	return val.v, val.ok
}
```

- [ ] **Step 4: Implement `tooltemplates.go`**

```go
package domain

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/exttool"
)

// NewToolBlock is the config block gg writes for a catalog template: the
// rendered command plus the template stamp (AppendToolCommands adds the
// fingerprint). Every writer of catalog blocks goes through it.
func NewToolBlock(det exttool.Detection, ct exttool.CommandTemplate) config.ToolCommand {
	return config.ToolCommand{
		Category: string(ct.Category), Name: ct.Name, Mode: string(ct.Mode),
		PerFile: ct.PerFile, WhenOp: ct.WhenOp, Frontends: ct.Frontends,
		Command:         exttool.GenerateCommand(ct, det.Bin),
		TemplateVersion: ct.Version, AgentRange: ct.Range,
	}
}

// ToolStatusKind is a catalog block's standing against the catalog.
type ToolStatusKind int

const (
	ToolCurrent         ToolStatusKind = iota // matches its template
	ToolCustomised                            // user-edited, template unchanged: left alone
	ToolUpdateAvailable                       // a newer template or a better-fitting variant
	ToolUnsupported                           // the installed agent is outside every range
)

// ToolTemplateStatus is one effective catalog block's standing.
type ToolTemplateStatus struct {
	Path         string             // the config file holding the block
	Block        config.ToolCommand // as read
	Kind         ToolStatusKind
	New          config.ToolCommand // the offered block (ToolUpdateAvailable only)
	FromVersion  int                // the block's template_version (0 = unstamped)
	ToVersion    int                // the family's current Version
	AgentVersion string             // "" = unknown
	FromRange    string
	ToRange      string
	Edited       bool
	ToolLabel    string
}

// OfferKey identifies this exact offer — the "Keep mine" memory key, so a
// later template change offers again.
func (st ToolTemplateStatus) OfferKey() string {
	return st.Block.Key() + "\x00" + config.ToolFingerprint(st.New)
}

// ToolTemplateStatuses computes the standing of every EFFECTIVE block (a
// later path's same-key block shadows an earlier one — the overlay rule)
// that belongs to a detected catalog family. Blocks of undetected tools and
// user-authored blocks get no status.
func ToolTemplateStatuses(ctx context.Context, paths []string, dets []exttool.Detection) []ToolTemplateStatus {
	type located struct {
		path string
		tc   config.ToolCommand
	}
	var order []string
	eff := map[string]located{}
	for _, p := range paths {
		blocks, err := config.ToolCommandsIn(p)
		if err != nil {
			continue
		}
		for _, tc := range blocks {
			if _, seen := eff[tc.Key()]; !seen {
				order = append(order, tc.Key())
			}
			eff[tc.Key()] = located{p, tc}
		}
	}
	var out []ToolTemplateStatus
	for _, key := range order {
		loc := eff[key]
		for _, det := range dets {
			fv := exttool.FamilyVersion(det.Tool, exttool.Category(loc.tc.Category), loc.tc.Name)
			if fv == 0 {
				continue
			}
			if st, ok := blockStatus(ctx, loc.path, loc.tc, det, fv); ok {
				out = append(out, st)
			}
			break
		}
	}
	return out
}

func blockStatus(ctx context.Context, path string, tc config.ToolCommand, det exttool.Detection, fv int) (ToolTemplateStatus, bool) {
	st := ToolTemplateStatus{Path: path, Block: tc, FromVersion: tc.TemplateVersion, ToVersion: fv,
		FromRange: tc.AgentRange, Edited: tc.Edited(), ToolLabel: det.Tool.Label}
	v, known := AgentVersion(ctx, det.Tool, det.Bin)
	if known {
		st.AgentVersion = fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2])
	}
	ct, ok := exttool.Variant(det.Tool, exttool.Category(tc.Category), tc.Name, v, known, tc.AgentRange)
	if !ok {
		if known {
			st.Kind = ToolUnsupported
			return st, true
		}
		return st, false // unknown version, no variant to name: no status
	}
	st.ToRange = ct.Range
	st.New = NewToolBlock(det, ct)
	target := config.ToolFingerprint(st.New)
	switch {
	case !tc.Stamped():
		if config.ToolFingerprint(tc) == target {
			st.Kind = ToolCurrent
		} else {
			st.Kind = ToolUpdateAvailable
		}
	case tc.TemplateVersion < fv || tc.AgentRange != ct.Range:
		st.Kind = ToolUpdateAvailable
	case st.Edited:
		st.Kind = ToolCustomised
	default:
		st.Kind = ToolCurrent
	}
	return st, true
}

// ErrToolBlockChanged: the block changed on disk since its status was computed.
var ErrToolBlockChanged = errors.New("the block changed since the offer was made — reopen to see the current offer")

// ApplyToolUpdate writes st.New over st.Block in st.Path, after checking the
// file still holds that block (same meaning — a formatting-only edit is fine).
func ApplyToolUpdate(st ToolTemplateStatus) error {
	blocks, err := config.ToolCommandsIn(st.Path)
	if err != nil {
		return err
	}
	for _, tc := range blocks {
		if tc.Key() != st.Block.Key() {
			continue
		}
		if config.ToolFingerprint(tc) != config.ToolFingerprint(st.Block) {
			return ErrToolBlockChanged
		}
		_, err := config.ReplaceToolCommand(st.Path, tc.Key(), st.New)
		return err
	}
	return ErrToolBlockChanged
}

// ToolTemplateStatuses is the frontends' entry: the global file then the
// active repo file, against the tools detected on this machine.
func (s *Service) ToolTemplateStatuses(ctx context.Context) []ToolTemplateStatus {
	home, _ := os.UserHomeDir()
	return ToolTemplateStatuses(ctx, s.reviewCommandConfigPaths(ctx), exttool.Detect(exec.LookPath, os.Stat, home))
}
```

Rename `reviewCommandConfigPaths` → `toolConfigPaths` (update its two
callers in `review_upgrade.go`) since it now serves both.

In `sessions.go` and `task_modes.go`, replace the inline
`config.ToolCommand{…GenerateCommand…}` literal with
`NewToolBlock(det, ct)` so first-run auto-configured blocks are stamped too.

- [ ] **Step 5: Run to verify it passes**

Run: `go test ./internal/domain/ -run 'TestAgentVersion|TestToolTemplate|TestApplyToolUpdate|Session|Interactive|Review'`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git -C /mnt/t/others/gigagit/.claude/worktrees/tool-template-versions add internal/domain
git -C /mnt/t/others/gigagit/.claude/worktrees/tool-template-versions commit -m "feat(domain): agent version probe, tool-template statuses, apply update"
```

---

### Task 7: "Keep mine" memory (`promptstate`)

**Files:**
- Modify: `internal/promptstate/store.go`, `internal/promptstate/file_store.go`
- Test: `internal/promptstate/tool_updates_test.go`

**Interfaces:**
- Produces: `Store.DeclinedToolUpdates() map[string]bool`;
  `Store.DeclineToolUpdate(offerKey string) error` (machine-global — the
  config files are machine-global too). Record field
  `DeclinedToolUpdates []string \`toml:"declined_tool_updates,omitempty"\``.
  Key stored as hex sha256 of the offer key (keeps the file printable).

- [ ] **Step 1: Write the failing test**

```go
package promptstate

import (
	"path/filepath"
	"testing"
)

func TestDeclineToolUpdatePersists(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "prompts.toml")
	fs := NewFileStore(path)
	if err := fs.DeclineToolUpdate("review\x00A\x00sha256:1"); err != nil {
		t.Fatal(err)
	}
	fs.DeclineToolUpdate("review\x00A\x00sha256:1") // idempotent
	got := NewFileStore(path).DeclinedToolUpdates()
	if len(got) != 1 || !got[ToolUpdateID("review\x00A\x00sha256:1")] {
		t.Fatalf("got %v", got)
	}
	if got[ToolUpdateID("review\x00A\x00sha256:2")] {
		t.Fatal("a different offer must not read as declined")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/promptstate/ -run TestDeclineToolUpdate`
Expected: FAIL — undefined.

- [ ] **Step 3: Implement**

`store.go`, in the interface:

```go
	// DeclinedToolUpdates is the set of ToolUpdateID(offerKey) answered
	// "Keep mine" in the tool-template review.
	DeclinedToolUpdates() map[string]bool
	// DeclineToolUpdate records one "Keep mine" (idempotent).
	DeclineToolUpdate(offerKey string) error
```

`file_store.go`: add the `records` field above; then

```go
// ToolUpdateID is the stored form of a tool-update offer key.
func ToolUpdateID(offerKey string) string {
	sum := sha256.Sum256([]byte(offerKey))
	return hex.EncodeToString(sum[:])
}

func (fs *FileStore) DeclinedToolUpdates() map[string]bool {
	return toSet(fs.read().DeclinedToolUpdates)
}

func (fs *FileStore) DeclineToolUpdate(offerKey string) error {
	r := fs.read()
	id := ToolUpdateID(offerKey)
	if toSet(r.DeclinedToolUpdates)[id] {
		return nil
	}
	r.DeclinedToolUpdates = append(r.DeclinedToolUpdates, id)
	return fs.write(r)
}
```

(imports `crypto/sha256`, `encoding/hex`). Grep for any other `Store`
implementation (`grep -rn "SetTaskLaunchChoice(" internal`) and add the two
methods there too.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/promptstate/ && go build ./...`
Expected: PASS, clean build.

- [ ] **Step 5: Commit**

```bash
git -C /mnt/t/others/gigagit/.claude/worktrees/tool-template-versions add internal/promptstate
git -C /mnt/t/others/gigagit/.claude/worktrees/tool-template-versions commit -m "feat(promptstate): remember declined tool-template updates"
```

---

### Task 8: TUI — stamped wizard writes, status suffix, review popup

**Files:**
- Modify: `internal/tui/settings_tools.go`, `internal/tui/settings_popup.go`
- Create: `internal/tui/tool_update_popup.go`
- Modify: `internal/i18n/lang/{ja,ko,ru,zh}.toml` (every new key)
- Test: `internal/tui/tool_update_popup_test.go`

**Interfaces:**
- Consumes: Task 6 (`NewToolBlock`, `ToolTemplateStatuses`, `ApplyToolUpdate`, `ToolTemplateStatus`, kinds), Task 7.
- Produces: `toolWizardRow.status *domain.ToolTemplateStatus`;
  `toolUpdatePopup{popupMax; st domain.ToolTemplateStatus}`;
  `(m Model) toolUpdateReason(st domain.ToolTemplateStatus) string` (shared with Task 9's notice);
  `var toolStatusesFn = func(m Model) []domain.ToolTemplateStatus { return m.svc.ToolTemplateStatuses(context.Background()) }` (test seam);
  `var toolDetectFn = func() []exttool.Detection` (test seam, default real `exttool.Detect`).

- [ ] **Step 1: Write the failing tests**

```go
package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/promptstate"
)

func sampleToolStatus(t *testing.T) domain.ToolTemplateStatus {
	path := filepath.Join(t.TempDir(), "config.toml")
	old := config.ToolCommand{Category: "conflict_complete", Name: "Claude — resolve & complete (yolo, headless)", Mode: "capture", Frontends: []string{"web"}, Command: "claude -p x"}
	if err := config.AppendToolCommands(path, []config.ToolCommand{old}); err != nil {
		t.Fatal(err)
	}
	nw := old
	nw.Frontends, nw.TemplateVersion = []string{"tui", "web"}, 2
	return domain.ToolTemplateStatus{Path: path, Block: old, New: nw, Kind: domain.ToolUpdateAvailable, FromVersion: 0, ToVersion: 2, Edited: true, ToolLabel: "Claude Code"}
}

func TestToolUpdatePopupShowsReasonAndNewText(t *testing.T) {
	m := newTestModel(t)
	st := sampleToolStatus(t)
	m = m.pushLayer(&toolUpdatePopup{st: st})
	out := m.View()
	for _, want := range []string{"frontends = [\"tui\", \"web\"]", "claude -p x", "[t]", "[k]"} {
		if !strings.Contains(out, want) {
			t.Errorf("popup lacks %q:\n%s", want, out)
		}
	}
}

func TestToolUpdatePopupTakeNewWrites(t *testing.T) {
	m := newTestModel(t)
	st := sampleToolStatus(t)
	m = m.pushLayer(&toolUpdatePopup{st: st})
	m, _ = updateModel(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	got, _ := config.ToolCommandsIn(st.Path)
	if len(got) != 1 || len(got[0].Frontends) != 2 || got[0].TemplateVersion != 2 {
		t.Fatalf("take new did not write: %+v", got)
	}
	if layerOf[*toolUpdatePopup](m) != nil {
		t.Fatal("popup must close after take new")
	}
}

func TestToolUpdatePopupKeepMineRemembers(t *testing.T) {
	m := newTestModel(t)
	store := promptstate.NewFileStore(filepath.Join(t.TempDir(), "prompts.toml"))
	m.promptStore = store
	st := sampleToolStatus(t)
	before, _ := os.ReadFile(st.Path)
	m = m.pushLayer(&toolUpdatePopup{st: st})
	m, _ = updateModel(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	after, _ := os.ReadFile(st.Path)
	if string(before) != string(after) {
		t.Fatal("keep mine must not write the config")
	}
	if !store.DeclinedToolUpdates()[promptstate.ToolUpdateID(st.OfferKey())] {
		t.Fatal("keep mine must remember the offer")
	}
}
```

(Use the existing TUI test helpers — check `grep -n "^func newTestModel\|^func updateModel" internal/tui/*_test.go`; if they are named differently, use those names — do not add duplicates.)

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/tui/ -run TestToolUpdatePopup`
Expected: FAIL — undefined `toolUpdatePopup`.

- [ ] **Step 3: Implement the popup (`tool_update_popup.go`)**

```go
package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// toolUpdatePopup reviews one tool-template offer: the reason and the new
// block's full text (user ruling: no diff), answered take / keep / edit.
type toolUpdatePopup struct {
	popupMax
	st domain.ToolTemplateStatus
}

// toolUpdateReason is the one-line offer reason shared by this popup, the
// Settings row and the notice.
func (m Model) toolUpdateReason(st domain.ToolTemplateStatus) string {
	var parts []string
	switch {
	case st.Kind == domain.ToolUnsupported:
		return i18n.T("%s %s is outside every version range this template supports", st.ToolLabel, st.AgentVersion)
	case st.FromVersion == 0:
		parts = append(parts, i18n.T("written before template versions — current template v%d", st.ToVersion))
	case st.FromVersion < st.ToVersion:
		parts = append(parts, i18n.T("template updated (v%d → v%d)", st.FromVersion, st.ToVersion))
	}
	if st.FromRange != st.ToRange && st.AgentVersion != "" {
		parts = append(parts, i18n.T("%s %s detected — this block was written for %s", st.ToolLabel, st.AgentVersion, st.FromRange))
	}
	if st.Edited && st.FromVersion > 0 {
		parts = append(parts, i18n.T("you changed this block"))
	}
	return strings.Join(parts, " · ")
}

func (p *toolUpdatePopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyEsc:
		return m.popLayer(), nil
	case msg.Type == tea.KeyRunes && string(msg.Runes) == "t":
		m = m.popLayer()
		if err := domain.ApplyToolUpdate(p.st); err != nil {
			m.statusMsg = i18n.T("external tools: %s", err.Error())
			return m, nil
		}
		m.statusMsg = i18n.T("external tools: updated %s in %s", p.st.Block.Name, p.st.Path)
		return m.reloadToolConfig(), m.refreshToolStatusesCmd()
	case msg.Type == tea.KeyRunes && string(msg.Runes) == "k":
		m = m.popLayer()
		if m.promptStore != nil {
			_ = m.promptStore.DeclineToolUpdate(p.st.OfferKey())
		}
		m.statusMsg = i18n.T("external tools: kept your %s — not offered again until the template changes", p.st.Block.Name)
		return m, m.refreshToolStatusesCmd()
	case msg.Type == tea.KeyRunes && string(msg.Runes) == "e":
		return m.popLayer(), handover(editorCommandAt(resolveEditor(), p.st.Path, 0), func(err error) tea.Msg {
			return editorFinishedMsg{path: p.st.Path, err: err}
		})
	}
	return m, nil
}

func (p *toolUpdatePopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

func (p *toolUpdatePopup) box(m Model) string {
	w, _ := m.overlayDims()
	inner := popupResolveWidth(w, p.maximized, popupInnerWidth(w))
	tw := popupTextWidth(inner)
	var b strings.Builder
	b.WriteString(i18n.T("Update %s?", p.st.Block.Name) + "\n\n")
	for _, ln := range wrapWords(m.toolUpdateReason(p.st), tw) {
		b.WriteString(st().dim.Render(ln) + "\n")
	}
	b.WriteString("\n" + i18n.T("New template (written to %s):", elidePath(p.st.Path, tw)) + "\n\n")
	b.WriteString(renderToolBlockText(p.st.New, tw))
	b.WriteString("\n\n" + i18n.T("[t] take new  [k] keep mine  [e] edit config  [esc] later"))
	return st().modalStyle.Width(inner).Render(b.String()) + "\n"
}
```

Add helpers in the same file:

```go
// renderToolBlockText is the block exactly as the config writer will lay
// it out (stamp keys included), each line word-wrapped at w.
func renderToolBlockText(tc config.ToolCommand, w int) string {
	text := config.RenderToolCommand(tc)
	var out []string
	for _, ln := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		out = append(out, wrapWords(ln, w)...)
	}
	return strings.Join(out, "\n")
}
```

and export the writer in `config/tools.go`:
`func RenderToolCommand(tc ToolCommand) string { var b strings.Builder; renderToolCommand(&b, tc); return b.String() }`.

(If `elidePath` has a different signature, use the existing one — memory rule: paths are always middle-elided with `elidePath`.)

`reloadToolConfig` / `refreshToolStatusesCmd` on `Model` (put in `settings_tools.go`):

```go
// reloadToolConfig re-reads the effective config after a tools write (the
// applyToolsWizard tail, shared).
func (m Model) reloadToolConfig() Model {
	if cfg, err := config.Load(config.DefaultGlobalPath(), m.repoConfigPath); err == nil {
		m.cfg = cfg
		m = m.applyBranchFilterConfig()
	}
	return m
}

// toolStatusesMsg carries a background tool-template status read.
type toolStatusesMsg struct {
	gen int
	sts []domain.ToolTemplateStatus
}

var toolStatusesFn = func(m Model) []domain.ToolTemplateStatus {
	return m.svc.ToolTemplateStatuses(context.Background())
}

// refreshToolStatusesCmd re-reads statuses off the UI thread (agent version
// probes spawn processes).
func (m Model) refreshToolStatusesCmd() tea.Cmd {
	gen := m.noticeGen
	return func() tea.Msg { return toolStatusesMsg{gen: gen, sts: toolStatusesFn(m)} }
}
```

Handle `toolStatusesMsg` in `Model.Update` (next to `repoHealthMsg`):
drop on `msg.gen != m.noticeGen`; set `m.toolStatuses = msg.sts` (new
`[]domain.ToolTemplateStatus` field on `Model`); if the tools wizard is
open, re-attach statuses to its rows (`p.toolRows[i].status`); then
`m = m.rebuildNotices()` (Task 9 consumes it). Replace the tail of
`applyToolsWizard` with `m = m.reloadToolConfig()`.

- [ ] **Step 4: Wizard rows: stamped writes + status suffix + `u`**

In `applyToolsWizard`, replace the `config.ToolCommand{…}` literal with
`domain.NewToolBlock(row.det, row.tmpl)`.

In `openToolsWizard`, after building rows, attach statuses:

```go
	byKey := map[string]*domain.ToolTemplateStatus{}
	for i := range m.toolStatuses {
		byKey[m.toolStatuses[i].Block.Key()] = &m.toolStatuses[i]
	}
	for i := range p.toolRows {
		p.toolRows[i].status = byKey[p.toolRows[i].tmpl.Key()]
	}
```

and return `m, m.refreshToolStatusesCmd()` from the caller that opens the
wizard (find it: `grep -n "openToolsWizard()" internal/tui/*.go`).

In the row render (`settings_popup.go` ~line 1041, the `row.existing`
branch), choose the suffix from the status:

```go
				if row.existing {
					suffix = " " + i18n.T("(configured)")
					if s := row.status; s != nil && !m.toolOfferDeclined(*s) {
						switch s.Kind {
						case domain.ToolUpdateAvailable:
							suffix = " " + i18n.T("(update available — u)")
						case domain.ToolUnsupported:
							suffix = " " + i18n.T("(agent version unsupported)")
						}
					}
					text = base + suffix
					deco = toolConfiguredSuffixDecorator(lipgloss.Width(base), lipgloss.Width(suffix))
				}
```

with, in `settings_tools.go`:

```go
func (m Model) toolOfferDeclined(st domain.ToolTemplateStatus) bool {
	return m.promptStore != nil && m.promptStore.DeclinedToolUpdates()[promptstate.ToolUpdateID(st.OfferKey())]
}
```

In the toolsView key switch (`settings_popup.go` ~line 749) add before
`case tea.KeyEnter`:

```go
		case tea.KeyRunes:
			if string(msg.Runes) == "u" && p.sel >= 0 && p.sel < len(p.toolRows) {
				if s := p.toolRows[p.sel].status; s != nil && s.Kind == domain.ToolUpdateAvailable {
					return m.pushLayer(&toolUpdatePopup{st: *s}), nil
				}
			}
```

Add `i18n.T("[u] review update")` to `hintParts`. A declined offer stays
reachable with `u` (the suffix hides; the key still opens it) — the user
can change their mind.

- [ ] **Step 5: Translations**

Add every new key (all `i18n.T` literals introduced in this task) to
`internal/i18n/lang/{ja,ko,ru,zh}.toml`, following `adding-translations`.
Run: `go test ./internal/tui/ -run 'I18n|Vocab|MenuLabels|EngineProse'`
Expected: PASS.

- [ ] **Step 6: Run to verify**

Run: `go test ./internal/tui/ -run 'TestToolUpdatePopup|Tools|Settings'`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git -C /mnt/t/others/gigagit/.claude/worktrees/tool-template-versions add internal/tui internal/i18n internal/config/tools.go
git -C /mnt/t/others/gigagit/.claude/worktrees/tool-template-versions commit -m "feat(tui): tool-template update review in Settings → External tools"
```

---

### Task 9: TUI notice + end-to-end fix of the trigger case

**Files:**
- Modify: `internal/tui/notify.go`, the repo-health load path (where `repoHealthCmd` is batched)
- Modify: `internal/i18n/lang/{ja,ko,ru,zh}.toml`
- Test: `internal/tui/tool_notice_test.go`

**Interfaces:**
- Consumes: Task 8 (`m.toolStatuses`, `toolStatusesMsg`, `refreshToolStatusesCmd`, `toolUpdateReason`, `toolOfferDeclined`).
- Produces: `toolTemplateNotices(m Model) []notice` (ids `tool_template_update`, `tool_agent_unsupported_<toolID-less key hash>`).

- [ ] **Step 1: Write the failing tests**

```go
package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/promptstate"
)

func TestToolTemplateNoticeCountsOffers(t *testing.T) {
	m := newTestModel(t)
	m.promptStore = promptstate.NewFileStore(filepath.Join(t.TempDir(), "prompts.toml"))
	a, b := sampleToolStatus(t), sampleToolStatus(t)
	b.Block.Name, b.New.Name = "Other", "Other"
	m, _ = updateModel(m, toolStatusesMsg{gen: m.noticeGen, sts: []domain.ToolTemplateStatus{a, b}})
	var got *notice
	for i := range m.notices {
		if m.notices[i].id == noticeToolTemplateUpdate {
			got = &m.notices[i]
		}
	}
	if got == nil || !strings.Contains(got.title, "2") {
		t.Fatalf("want a 2-offer notice, got %+v", m.notices)
	}
	m.promptStore.DeclineToolUpdate(a.OfferKey())
	m.promptStore.DeclineToolUpdate(b.OfferKey())
	m = m.rebuildNotices()
	for _, n := range m.notices {
		if n.id == noticeToolTemplateUpdate {
			t.Fatal("declined offers must not be counted")
		}
	}
}

// The trigger case: a web-only Claude headless complete block → take new →
// the TUI conflict picker's tool list includes it.
func TestTakeNewMakesHeadlessCompleteVisibleInTUI(t *testing.T) {
	m := newTestModel(t)
	st := sampleToolStatus(t)
	m.repoConfigPath = st.Path // the block lives in this file for the test
	m = m.reloadToolConfig()
	if len(m.toolCommands("conflict_complete")) != 0 {
		t.Fatal("precondition: web-only block must be hidden in the TUI")
	}
	m = m.pushLayer(&toolUpdatePopup{st: st})
	m, _ = updateModel(m, keyRunes("t"))
	if len(m.toolCommands("conflict_complete")) != 1 {
		t.Fatal("after take new the TUI must offer the headless complete tool")
	}
}
```

(`keyRunes` — use the existing helper name; `grep -n "func keyRunes\|func runeKey" internal/tui/*_test.go`.)

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/tui/ -run 'TestToolTemplateNotice|TestTakeNewMakesHeadless'`
Expected: FAIL — undefined `noticeToolTemplateUpdate`.

- [ ] **Step 3: Implement**

In `notify.go`:

```go
// noticeToolTemplateUpdate is the tool-template offers notice's stable id.
const noticeToolTemplateUpdate = "tool_template_update"

// toolTemplateNotices: one notice counting open update offers (never
// declined ones) and one per unsupported agent. Both open Settings →
// External tools; nothing is written from the notice itself.
func toolTemplateNotices(m Model) []notice {
	var offers int
	var out []notice
	for _, st := range m.toolStatuses {
		switch {
		case st.Kind == domain.ToolUpdateAvailable && !m.toolOfferDeclined(st):
			offers++
		case st.Kind == domain.ToolUnsupported:
			out = append(out, notice{
				id:      "tool_agent_unsupported_" + promptstate.ToolUpdateID(st.Block.Key())[:12],
				repoKey: m.repoHealth.GitCommonDir,
				title:   m.toolUpdateReason(st),
				detail:  []string{i18n.T("The block keeps running as it is. Update the agent, or edit the block in Settings → External tools.")},
				actions: []noticeAction{
					{label: i18n.T("Open external tools"), run: openToolsFromNotice},
					{label: i18n.T("Not now (ask again next load)")},
					{label: i18n.T("Never for this repo"), never: true},
				},
			})
		}
	}
	if offers > 0 {
		out = append([]notice{{
			id:      noticeToolTemplateUpdate,
			repoKey: m.repoHealth.GitCommonDir,
			title:   i18n.T("%d external-tool templates can be updated", offers),
			detail:  []string{i18n.T("Newer templates exist for commands gg once wrote into your config. Nothing changes until you review each one.")},
			actions: []noticeAction{
				{label: i18n.T("Open external tools"), run: openToolsFromNotice},
				{label: i18n.T("Not now (ask again next load)")},
			},
		}}, out...)
	}
	return out
}

func openToolsFromNotice(m Model) (Model, tea.Cmd) {
	m, cmd := m.openSettingsPopup() // existing opener — grep for the Settings open path and reuse it
	m = m.openToolsWizard()
	return m, tea.Batch(cmd, m.refreshToolStatusesCmd())
}
```

(Before writing `openToolsFromNotice`, find the actual Settings opener:
`grep -n "settingsPopup{" internal/tui/*.go` — use it, don't invent one.
Offers are counted, so the update notice gets no "Never" — a new offer
must be able to surface again; per-offer silence is "Keep mine".)

In `rebuildHealthNotices`, append:

```go
	for _, n := range toolTemplateNotices(m) {
		if !dismissed[n.id] && !m.noticeSessionDismissed[n.id] {
			next = append(next, n)
		}
	}
```

Where the repo-health read is started (every `repoHealthCmd(` call site
batched on startup/reRoot), batch `m.refreshToolStatusesCmd()` next to it.
In the `toolStatusesMsg` handler, arm the blink exactly like
`applyRepoHealth` does for new ids (copy its `prev`/`noticesUnread` block —
extract it into `func (m Model) armBlinkForNew(prev map[string]bool) (Model, tea.Cmd)` and call it from both).

Translations for every new key into the four bundles.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/tui/`
Expected: PASS (incl. the i18n gates).

- [ ] **Step 5: Headless look** (driving-tui-headless skill)

Build `go build -o /mnt/t/others/gigagit/.claude/worktrees/tool-template-versions/bin/gg ./cmd/gg`,
then with a temp `XDG_CONFIG_HOME` holding the web-only Claude headless
block, run `./tui-capture.sh` with the keyscript that opens `!`, then the
notice action, then `u`. Check the snapshots show the notice title, the
`(update available — u)` suffix, and the popup with the reason + new text.

- [ ] **Step 6: Commit**

```bash
git -C /mnt/t/others/gigagit/.claude/worktrees/tool-template-versions add internal/tui internal/i18n
git -C /mnt/t/others/gigagit/.claude/worktrees/tool-template-versions commit -m "feat(tui): notice for tool-template updates and unsupported agents"
```

---

### Task 10: Web — status, take new / keep mine

**Files:**
- Modify: `internal/web/exttools.go`, `internal/web/server.go`, `internal/web/static/exttools.js` (+ its CSS if a new class is needed)
- Test: `internal/web/exttools_update_test.go`

**Interfaces:**
- Consumes: Task 6, Task 7.
- Produces: `extToolCmdRow` gains `Status string` (`"current"|"customised"|"update"|"unsupported"|""`), `Reason string`, `NewText string`, `OfferID string` (= `promptstate.ToolUpdateID(OfferKey)`), `Declined bool`;
  `POST /api/exttools/update {offer_id}` and `POST /api/exttools/keep {offer_id}` — the server recomputes statuses and resolves `offer_id` against them (allowlist: the wire never carries a path or a command).

- [ ] **Step 1: Write the failing test**

```go
package web

import (
	"net/http"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/promptstate"
)

func TestExtToolsUpdateByOfferID(t *testing.T) {
	srv, path := newToolStatusTestServer(t) // builds a Server with toolStatuses seam returning one ToolUpdateAvailable status for a block in path
	st := srv.toolStatuses(nil)[0]
	id := promptstate.ToolUpdateID(st.OfferKey())

	rec := postJSON(t, srv, "/api/exttools/update", `{"offer_id":"`+id+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body)
	}
	got, _ := config.ToolCommandsIn(path)
	if got[0].TemplateVersion != st.New.TemplateVersion {
		t.Fatalf("not written: %+v", got[0])
	}
	rec = postJSON(t, srv, "/api/exttools/update", `{"offer_id":"deadbeef"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown offer: %d", rec.Code)
	}
	_ = domain.ToolUpdateAvailable
	_ = strings.Contains
}
```

Add to `exttools.go` the seam `toolStatuses func(ctx context.Context) []domain.ToolTemplateStatus` on `Server` (nil = `s.service().ToolTemplateStatuses`), and write `newToolStatusTestServer` in the test file using the existing web test-server helper (`grep -n "^func newTestServer" internal/web/*_test.go`) plus `postJSON` if it exists (else a 6-line `httptest` helper). Add a keep test mirroring the update one: after `/keep`, `DeclinedToolUpdates()` holds `id` and the config file is byte-identical.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/web/ -run TestExtToolsUpdate`
Expected: FAIL.

- [ ] **Step 3: Implement**

In `handleExtTools`, build `byKey := map[string]domain.ToolTemplateStatus{}`
from `s.toolStatusesFor(r.Context())`; per configured row set
`Status`, `Reason` (English, via a `toolReason(st)` in `exttools.go` that
mirrors the TUI wording — the web is not i18n'd), `NewText =
config.RenderToolCommand(st.New)`, `OfferID`, `Declined`.

```go
func (s *Server) handleExtToolsUpdate(w http.ResponseWriter, r *http.Request) {
	st, ok := s.offerByID(w, r)
	if !ok {
		return
	}
	if err := domain.ApplyToolUpdate(st); err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleExtToolsKeep(w http.ResponseWriter, r *http.Request) {
	st, ok := s.offerByID(w, r)
	if !ok {
		return
	}
	store := s.promptStore()
	if store == nil {
		writeErr(w, http.StatusServiceUnavailable, errors.New("no state dir: cannot remember"))
		return
	}
	if err := store.DeclineToolUpdate(st.OfferKey()); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// offerByID resolves a wire offer id against a FRESH status read — the wire
// never names a path or a command.
func (s *Server) offerByID(w http.ResponseWriter, r *http.Request) (domain.ToolTemplateStatus, bool) {
	var req struct {
		OfferID string `json:"offer_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return domain.ToolTemplateStatus{}, false
	}
	for _, st := range s.toolStatusesFor(r.Context()) {
		if st.Kind == domain.ToolUpdateAvailable && promptstate.ToolUpdateID(st.OfferKey()) == req.OfferID {
			return st, true
		}
	}
	writeErr(w, http.StatusNotFound, errors.New("no such update offer"))
	return domain.ToolTemplateStatus{}, false
}
```

Routes in `server.go` beside `GET /api/exttools`:

```go
	mux.HandleFunc("POST /api/exttools/update", writeGuard(s.handleExtToolsUpdate))
	mux.HandleFunc("POST /api/exttools/keep", writeGuard(s.handleExtToolsKeep))
```

`exttools.js`: in `cmdRowHTML`, when `c.status === "update" && !c.declined`
add `<span class="xwarn">update available</span>` and a
`<button data-offer="${esc(c.offer_id)}">review</button>`; `"unsupported"`
adds `<span class="xproblem">${esc(c.reason)}</span>`. A click on
`[data-offer]` opens an inline panel under the row showing `c.reason` and
`<pre class="xcmdline">${esc(c.new_text)}</pre>` with **Take new** / **Keep
mine** buttons that `postJSON("/api/exttools/update"|"/keep", {offer_id})`
then re-`openExtToolsView()`'s data fetch and re-render. Update the header
comment and footer: the overlay is no longer strictly read-only (template
updates only). Hide the panel by id (memory: gg web hides by ID, not a
global `.hidden`).

- [ ] **Step 4: Run to verify**

Run: `go test ./internal/web/`
Expected: PASS.

- [ ] **Step 5: Browser check** (playwright-web-verification memory)

Rebuild `bin/gg`, run `gg web` against a temp `XDG_CONFIG_HOME` holding the
web-only Claude block, hard-reload, open the external-tools overlay: assert
the "update available" badge is VISIBLE, open review, assert the new text is
visible, click Take new, assert the badge disappears and the file changed.

- [ ] **Step 6: Commit**

```bash
git -C /mnt/t/others/gigagit/.claude/worktrees/tool-template-versions add internal/web
git -C /mnt/t/others/gigagit/.claude/worktrees/tool-template-versions commit -m "feat(web): review and take tool-template updates in the external-tools overlay"
```

---

### Task 11: Docs, skill, full gate

**Files:**
- Modify: `CHANGELOG.md`, `README.md` (External tools section), `docs/CLAUDE-details.md` (exttool/config/domain entries), `.claude/skills/adding-external-tools/SKILL.md`

- [ ] **Step 1: Skill rule** — add to `adding-external-tools`:
  "Changing a template (command, mode, frontends, when_op, per_file, OptIn,
  or its Range): raise the family's `Version` (`Version: N` on EVERY row of
  the family) and run `go test ./internal/exttool -run TestCatalogVersionBumpGuard -update`.
  A new agent-CLI behaviour gets a new variant row with a disjoint `Range`
  instead of editing the old one; a new AI tool sets `VersionArgs`."
- [ ] **Step 2: CHANGELOG** — one Unreleased entry: stamped tool blocks,
  update offers in Settings/notice/web, agent-version variants, the fix for
  blocks stuck on old `frontends`.
- [ ] **Step 3: README** — External tools: the stamp keys, "update available"
  / `u`, Keep mine semantics.
- [ ] **Step 4: CLAUDE-details** — `exttool` families/Pick/guard; `config`
  stamp + `ReplaceToolCommand`; `domain` statuses. (No `CLAUDE.md` map change
  beyond appending "template families + agent-version variants" to the
  `exttool` row and "stamped blocks, one-block replace" to `config`.)
- [ ] **Step 5: Full gate**

Run: `./test.sh` then `./test.sh race` (from the worktree).
Expected: all stages green. Report the actual output if anything fails.

- [ ] **Step 6: Commit**

```bash
git -C /mnt/t/others/gigagit/.claude/worktrees/tool-template-versions add CHANGELOG.md README.md docs/CLAUDE-details.md CLAUDE.md .claude/skills/adding-external-tools/SKILL.md
git -C /mnt/t/others/gigagit/.claude/worktrees/tool-template-versions commit -m "docs: versioned external-tool templates"
```

Then stop and ask before merging (merge discipline: `gg merge -F <msgfile> --into main feat/tool-template-versions`).
