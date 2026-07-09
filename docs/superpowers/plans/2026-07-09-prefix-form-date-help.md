# Prefix Form Validation + Bare `<date>` Default + Format Help Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** An invalid branch prefix keeps the add-prefix form open with an inline error instead of closing it; a bare `<date>` token resolves to `yyyy-MM-dd`; `ctrl+d` in the form opens a token/date-format cheat sheet.

**Architecture:** Three thin slices along existing seams: (1) `internal/template.resolveToken` gains a default layout for `<date>`/`<date:>`; (2) `prefixSettingsView.updateForm` validates synchronously via the pure `domain.ValidatePrefixValue` before dispatching, storing the error in a new `formErr` field rendered inside the popup; (3) a new `prefixTokensHelp` cheat sheet in `popup_help.go` pushed as a `contentPopup` layer on `ctrl+d`.

**Tech Stack:** Go 1.26, Bubble Tea/lipgloss TUI, plain `go test` (all changes are pure — no real-git tests needed).

**Spec:** `docs/superpowers/specs/2026-07-09-prefix-form-validation-date-default-design.md`

## Global Constraints

- Work in the worktree at `/mnt/t/others/gigagit/.claude/worktrees/prefix-form-date-help`, branch `feat/prefix-form-date-help`. **Subagents start in the main checkout — `cd` into the worktree first and verify with `git status -sb` (must say `feat/prefix-form-date-help`).** All Write/Edit paths must use the worktree's absolute path.
- Archtest layering: `internal/tui` may import `internal/domain` (never `internal/git`); `internal/template` stays pure (no I/O).
- TUI `Model` is a value receiver; `prefixSettingsView` is a pointer layer — mutate its fields directly.
- Canonical date-format spelling in every doc/hint: `yyyy-MM-dd` (lowercase `mm` = minutes). Go layout: `2006-01-02`.
- Run `gofmt -w` on touched files before each commit; `go vet ./...` must stay clean.

---

### Task 1: Bare `<date>` defaults to `yyyy-MM-dd` (template + domain validation)

**Files:**
- Modify: `internal/template/template.go:61-68` (the `"date"` case + the `Resolve` doc comment at lines 27-29)
- Test: `internal/template/template_test.go` (table in `TestResolveSubstitutionTokens`, and `TestResolveNilCtxDependenciesError`)
- Test: `internal/domain/prefixstore_test.go:52-78` (`TestValidatePrefixValue` — `"<date>"` moves from `bad` to `ok`)

**Interfaces:**
- Consumes: `template.Resolve(tmpl, inputs, ctx)` — unchanged signature.
- Produces: `<date>` and `<date:>` resolve to `ctx.Now().Format("2006-01-02")`. Task 2's TUI test uses `"feat/<date>"` as a now-valid value; Task 4's docs describe this default.

- [ ] **Step 1: Write the failing tests**

In `internal/template/template_test.go`, add two rows to the `TestResolveSubstitutionTokens` table (after the existing `{"date", ...}` row; the shared `fixedCtx()` pins `Now` to 2026-06-11 14:05:09 UTC):

```go
		{"date bare defaults to yyyy-MM-dd", "d-<date>", nil, fixedCtx(), "d-2026-06-11"},
		{"date empty format defaults too", "d-<date:>", nil, fixedCtx(), "d-2026-06-11"},
```

In `TestResolveNilCtxDependenciesError`, add before the `<random-alpha:4>` check:

```go
	if _, err := Resolve("<date>", nil, Ctx{}); err == nil {
		t.Error("bare <date> with nil Ctx.Now should error, not panic")
	}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/template/ -run 'TestResolveSubstitutionTokens|TestResolveNilCtxDependenciesError' -v`
Expected: FAIL — the two new table rows error with `template: <date> requires a format`.

- [ ] **Step 3: Implement the default**

In `internal/template/template.go`, replace the `"date"` case (currently lines 61-68):

```go
	case "date":
		if ctx.Now == nil {
			return "", fmt.Errorf("template: <date> requires Ctx.Now to be set")
		}
		if !hasColon || rest == "" {
			// Bare <date> (and an empty <date:> format) default to yyyy-MM-dd.
			return ctx.Now().Format("2006-01-02"), nil
		}
		return ctx.Now().Format(goLayout(rest)), nil
```

(Note the `Ctx.Now == nil` guard moves first so the bare form is covered too.)

Update the `Resolve` doc comment: change the sentence beginning "A `<date:...>` token requires Ctx.Now" to:

```go
// silently passed through). A <date> token without a format (or with an empty
// one) defaults to yyyy-MM-dd; any <date...> token requires Ctx.Now and a
// <random-*> token requires Ctx.Rand; if the corresponding field is nil,
// Resolve returns an error rather than panicking.
```

- [ ] **Step 4: Update the domain validation test (it now fails otherwise)**

In `internal/domain/prefixstore_test.go` `TestValidatePrefixValue`: add `"wt/<date>/"` to the `ok` slice and delete the `"<date>",    // missing format → engine errors` line from `bad`.

- [ ] **Step 5: Run both packages**

Run: `go test ./internal/template/ ./internal/domain/`
Expected: PASS (both packages).

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/template/template.go internal/template/template_test.go internal/domain/prefixstore_test.go
git add internal/template/ internal/domain/prefixstore_test.go
git commit -m "feat(template): bare <date> defaults to yyyy-MM-dd"
```

---

### Task 2: Invalid prefix keeps the form open with an inline error

**Files:**
- Modify: `internal/tui/prefix_settings.go` (imports; `prefixSettingsView` struct; `updateBrowse` `n`/`a` case; `updateForm` Enter case; `box` form branch)
- Test: `internal/tui/prefix_settings_test.go`

**Interfaces:**
- Consumes: `domain.ValidatePrefixValue(value string) error` (pure, exported); `errorStyle` (`internal/tui/view.go:87`, plain red fg for inline popup error lines); Task 1's bare-`<date>` support (one test uses `"feat/<date>"`).
- Produces: `prefixSettingsView.formErr string` — set on invalid Enter, cleared on form open and on valid Enter. Task 3 renders around it (footer hint line changes again there).

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/prefix_settings_test.go` (add `"strings"` to imports):

```go
// An invalid value must NOT close the form or dispatch the add — the error
// shows inline and the typed value survives so the user can fix it.
func TestPrefixSettingsInvalidValueKeepsFormOpen(t *testing.T) {
	v := &prefixSettingsView{mode: pfForm}
	v.fValue = newTextField("x-<bogus:1>-y")
	_, cmd := v.update(Model{}, tea.KeyMsg{Type: tea.KeyEnter})
	if v.mode != pfForm {
		t.Fatal("invalid value must keep the form open")
	}
	if cmd != nil {
		t.Fatal("invalid value must not dispatch an add")
	}
	if !strings.Contains(v.formErr, "<bogus:1>") {
		t.Fatalf("formErr = %q, want it to name the bad token", v.formErr)
	}
	if v.fValue.Value() != "x-<bogus:1>-y" {
		t.Fatal("typed value must be preserved")
	}
}

// The empty-value message moves inline too (it used to be a bottom-bar
// statusMsg).
func TestPrefixSettingsEmptyValueInlineError(t *testing.T) {
	v := &prefixSettingsView{mode: pfForm}
	v.fValue = newTextField("   ")
	m2, cmd := v.update(Model{}, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || v.mode != pfForm {
		t.Fatal("empty value must keep the form open without dispatching")
	}
	if v.formErr != "prefix value is required" {
		t.Fatalf("formErr = %q", v.formErr)
	}
	if m2.statusMsg != "" {
		t.Fatalf("statusMsg = %q, want the error inline instead", m2.statusMsg)
	}
}

func TestPrefixSettingsValidValueClosesFormAndDispatches(t *testing.T) {
	v := &prefixSettingsView{mode: pfForm}
	v.fValue = newTextField("feat/<date>") // bare <date> is valid since Task 1
	_, cmd := v.update(Model{}, tea.KeyMsg{Type: tea.KeyEnter})
	if v.mode != pfBrowse {
		t.Fatal("valid value must close the form")
	}
	if cmd == nil {
		t.Fatal("valid value must dispatch the add")
	}
	if v.formErr != "" {
		t.Fatalf("formErr = %q, want empty", v.formErr)
	}
}

// Reopening the form must not show a stale error from the previous attempt.
func TestPrefixSettingsReopenClearsInlineError(t *testing.T) {
	v := &prefixSettingsView{mode: pfBrowse, formErr: "stale"}
	v.update(Model{}, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if v.formErr != "" {
		t.Fatalf("formErr = %q, want cleared on open", v.formErr)
	}
}

func TestPrefixSettingsFormRendersInlineError(t *testing.T) {
	m := Model{}
	m.width, m.height = 120, 40
	v := &prefixSettingsView{mode: pfForm, formErr: "invalid prefix: template: unknown token <bogus>"}
	v.fValue = newTextField("<bogus>")
	if !strings.Contains(v.box(m), "unknown token <bogus>") {
		t.Fatal("form box must render the inline error line")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run TestPrefixSettings -v`
Expected: the five new tests FAIL (`formErr` undefined → compile error first; after adding the field stub they fail on behavior). The pre-existing four still pass.

- [ ] **Step 3: Implement**

In `internal/tui/prefix_settings.go`:

a) Add `"github.com/homeend/gigagit/internal/domain"` to the imports.

b) Add the field to the struct (after `field int`):

```go
	field  int // 0 = value, 1 = scope
	formErr string // inline validation error shown in the form; "" = none
```

c) In `updateBrowse`, the `"n", "a"` case gains a reset line:

```go
	case "n", "a":
		v.fValue = newTextField("")
		v.scope = model.ProfileScopeGlobal
		v.field = 0
		v.formErr = ""
		v.mode = pfForm
		return m, nil
```

d) Replace the `tea.KeyEnter` case in `updateForm`:

```go
	case tea.KeyEnter:
		p, ok := v.formPrefix()
		if !ok {
			v.formErr = "prefix value is required"
			return m, nil
		}
		if err := domain.ValidatePrefixValue(p.Value); err != nil {
			v.formErr = err.Error()
			return m, nil
		}
		v.formErr = ""
		v.mode = pfBrowse
		return m, m.addPrefixCmd(p)
```

e) In `box`'s `pfForm` branch, build `parts` incrementally so the error line slots in under the fields (replace the current single literal):

```go
		parts := []string{
			"Add branch prefix", "",
			viewField(cur+"value: ", v.fValue, v.field == 0, textW),
			scopeCursor + "scope: " + scopeVal,
		}
		if v.formErr != "" {
			parts = append(parts, "", errorStyle.Render(v.formErr))
		}
		parts = append(parts,
			"",
			"Tokens: <user:LABEL> <seq:NAME:N> <date> <date:FMT> <parent-branch> <repo> <random-*>",
			"",
			"[↑/↓] field  [←/→] scope  [enter] save  [esc] back",
		)
		return popupBox(inner, strings.Join(parts, "\n"))
```

(The `Tokens:` hint now lists bare `<date>` too. The footer hint gains `[ctrl+d] formats` in Task 3, not here.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run TestPrefixSettings -v`
Expected: all nine PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/tui/prefix_settings.go internal/tui/prefix_settings_test.go
git add internal/tui/prefix_settings.go internal/tui/prefix_settings_test.go
git commit -m "feat(tui): invalid branch prefix keeps the add form open with an inline error"
```

---

### Task 3: `ctrl+d` token/date-format cheat sheet over the form

**Files:**
- Modify: `internal/tui/popup_help.go` (new sheet builder + widen the file's header comment)
- Modify: `internal/tui/prefix_settings.go` (`updateForm` ctrl+d case; footer hint)
- Modify: `internal/tui/help.go:257` (advertise the sheet)
- Test: `internal/tui/prefix_settings_test.go`, `internal/tui/popup_help_test.go` (create if absent; check first — `ls internal/tui/popup_help_test.go`)

**Interfaces:**
- Consumes: `newContentPopup(title string, lines []contentLine) *contentPopup` (`content_popup.go:44`); `contentLine{text string, heading bool}`; `cheatRow`'s `padRight` helper; `m.pushLayer(l layer) Model` (`layer_stack.go:52`); `layerOf[*contentPopup](m)` for the test.
- Produces: `prefixTokensHelpTitle` const and `prefixTokensHelp(now time.Time) []contentLine` in `popup_help.go`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/prefix_settings_test.go`:

```go
// ctrl+d works even while typing (the textfield doesn't consume it) and must
// not disturb the form's state.
func TestPrefixSettingsCtrlDOpensFormatHelp(t *testing.T) {
	v := &prefixSettingsView{mode: pfForm}
	v.fValue = newTextField("feat/")
	m2, _ := v.update(Model{}, tea.KeyMsg{Type: tea.KeyCtrlD})
	if layerOf[*contentPopup](m2) == nil {
		t.Fatal("ctrl+d must push the token/date-format help sheet")
	}
	if v.mode != pfForm || v.fValue.Value() != "feat/" {
		t.Fatal("form state must survive opening the help sheet")
	}
}
```

Add to `internal/tui/popup_help_test.go` (create the file with `package tui` and imports `strings`, `testing`, `time` if it doesn't exist):

```go
func TestPrefixTokensHelpContent(t *testing.T) {
	now := time.Date(2026, 6, 11, 14, 5, 9, 0, time.UTC)
	var joined strings.Builder
	for _, l := range prefixTokensHelp(now) {
		joined.WriteString(l.text + "\n")
	}
	s := joined.String()
	for _, want := range []string{
		"<date>", "<date:FMT>", "yyyy", "MM", "dd", "HH", "mm", "ss",
		"2026-06-11",     // the live <date> example
		"20260611-1405",  // the <date:yyyyMMdd-HHmm> example
		"<user:LABEL>", "<random-alpha:N>",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("help sheet missing %q", want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestPrefixSettingsCtrlD|TestPrefixTokensHelp' -v`
Expected: compile error — `prefixTokensHelp` undefined.

- [ ] **Step 3: Implement the sheet builder**

Append to `internal/tui/popup_help.go` (add `"time"` to its imports), and extend the file's header comment to mention the prefix-form sheet:

```go
const prefixTokensHelpTitle = "Prefix tokens & date formats"

// prefixTokensHelp is the ctrl+d cheat sheet shown over the add-prefix form
// (Settings → Branch prefixes). now feeds the live examples so the sheet
// shows real output, not stale sample dates.
func prefixTokensHelp(now time.Time) []contentLine {
	tok := func(k, desc string) contentLine {
		return contentLine{text: padRight(k, 22) + desc}
	}
	return []contentLine{
		{text: "Tokens", heading: true},
		tok("<user:LABEL>", "asks you for LABEL whenever the prefix is used"),
		tok("<seq:NAME>", "per-repo counter NAME (1, 2, …)"),
		tok("<seq:NAME:N>", "the same, zero-padded to N digits"),
		tok("<date>", "today as yyyy-MM-dd"),
		tok("<date:FMT>", "now, formatted by FMT (see below)"),
		tok("<parent-branch>", "the branch the new branch forks from"),
		tok("<repo>", "the repository directory name"),
		tok("<random-alpha:N>", "N random lowercase letters"),
		tok("<random-num:N>", "N random digits"),
		{},
		{text: "Date format (FMT)", heading: true},
		tok("yyyy", "year, 4 digits"),
		tok("MM", "month 01–12"),
		tok("dd", "day 01–31"),
		tok("HH", "hour 00–23"),
		tok("mm", "minute 00–59"),
		tok("ss", "second 00–59"),
		{text: "Any other characters are literal separators."},
		{},
		{text: "Examples", heading: true},
		tok("<date>", now.Format("2006-01-02")),
		tok("<date:yyyy-MM-dd>", now.Format("2006-01-02")),
		tok("<date:yyyyMMdd-HHmm>", now.Format("20060102-1504")),
	}
}
```

- [ ] **Step 4: Wire ctrl+d + advertise**

a) In `prefix_settings.go` `updateForm`'s **type** switch (alongside `tea.KeyEsc`/`tea.KeyEnter`), add (and add `"time"` to the file's imports):

```go
	case tea.KeyCtrlD:
		return m.pushLayer(newContentPopup(prefixTokensHelpTitle, prefixTokensHelp(time.Now()))), nil
```

b) Change the form's footer hint line (from Task 2's step 3e) to:

```go
			"[↑/↓] field  [←/→] scope  [enter] save  [ctrl+d] formats  [esc] back",
```

c) In `internal/tui/help.go:257`, extend the Settings `enter` row description — append before the closing quote: `. In the add-prefix form, ctrl+d shows a token/date-format cheat sheet`.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestPrefixSettings|TestPrefixTokensHelp' -v`
Expected: all PASS (including a re-run of Task 2's tests — the footer hint change touches the same render).

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/tui/popup_help.go internal/tui/popup_help_test.go internal/tui/prefix_settings.go internal/tui/prefix_settings_test.go internal/tui/help.go
git add internal/tui/
git commit -m "feat(tui): ctrl+d date-format cheat sheet in the add-prefix form"
```

---

### Task 4: Docs sweep + full verification

**Files:**
- Modify: `README.md:190` (and any other `<date:YYYY-MM-DD>` in the branch-prefixes section — check with `grep -n 'YYYY-MM-DD\|<date' README.md`; line 481's review-report path is a file layout, NOT a date token — leave it)
- Modify: `internal/config/template.go:25` (`path_template` settingDoc description)
- Modify: `internal/agentskill/using-gg.md:241` + `internal/agentskill/agentskill.go:19` (`Version` 46 → 47)
- Modify: `CHANGELOG.md` (under `## [Unreleased]` → `### Added`)

**Interfaces:**
- Consumes: Tasks 1-3 behavior (docs must describe what shipped, no more).
- Produces: nothing downstream.

- [ ] **Step 1: README**

Line 190: change `` `<date:YYYY-MM-DD>`, `<seq:NAME:N>`, and `<user:LABEL>`. `` to `` `<date:yyyy-MM-dd>` (bare `<date>` defaults to `yyyy-MM-dd`), `<seq:NAME:N>`, and `<user:LABEL>`. `` — keep the rest of the sentence. If the grep finds a branch-prefixes feature bullet mentioning date tokens, align its spelling too.

- [ ] **Step 2: settingDoc**

`internal/config/template.go:25`: change the description's token list to `(tokens: <repo> <branch> <parent-branch> <date> <date:…> <seq:…>)`.

- [ ] **Step 3: using-gg.md + version bump**

`internal/agentskill/using-gg.md:241`: change `` `<date:…>` `` to `` `<date>`/`<date:…>` (bare `<date>` = today as `yyyy-MM-dd`) `` in the `gg prefix` token list. Bump `agentskill.go` `const Version = 46` to `47`.

- [ ] **Step 4: CHANGELOG**

Add under `## [Unreleased]` → `### Added` (top of that list):

```markdown
- **Branch-prefix editing niceties (Settings → Branch prefixes).** An invalid
  prefix no longer closes the add form and buries the error in the status
  bar: the form stays open with the typed value intact and the error shown
  inline, ready to fix. `ctrl+d` in the form opens a cheat sheet of every
  template token and the `<date:FMT>` format letters, with live examples.
  And a bare `<date>` token (no format) now defaults to `yyyy-MM-dd`
  everywhere templates resolve — prefixes and the worktree `path_template`.
```

- [ ] **Step 5: Full verification**

Run: `go build ./cmd/gg && ./test.sh race`
Expected: build OK; vet+gofmt stage clean; unit tests PASS; e2e PASS.

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/config/template.go internal/agentskill/agentskill.go
git add README.md internal/config/template.go internal/agentskill/ CHANGELOG.md
git commit -m "docs: bare <date> default + prefix-form inline validation and ctrl+d format help"
```

---

## Post-merge note (for the human)

`agentskill.Version` was bumped — after merging, run `gg init --update` in repos with installed agent skills to refresh the using-gg copies.
