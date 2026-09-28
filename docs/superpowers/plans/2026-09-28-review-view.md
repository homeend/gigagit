# Review View Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
> **gigagit rule:** NEVER use subagents — this plan is executed inline by the session that wrote it (superpowers:executing-plans).

**Goal:** Reviews are one structured document (agent-context v1 + `meta`), and opening one builds a review view of the commit: the file tree with an Overview row, `◆n` per file, the review's notes at their lines (read-only, never stored) and the overview rendered as markdown.

**Architecture:** `notebatch.ParseReview` is the one parser. The review context document (engine) and every built-in review template (exttool) ask for that document on `$GG_MESSAGE_FILE`. `domain.SaveReview` stores canonical JSON (or the text as received) and reports `Structured`; `domain.Review.Doc` is parsed on read; `domain.ReviewNotesFor` builds read-only `review:` notes at read time (the `forgeNotesFor` pattern). A preflight feature migrates stored copies of the old built-in commands with consent. The TUI gets a review mode on the files view (`m.filesReview`), a markdown overview popup, a `lineProse` stack element, and review notes in the diff.

**Tech Stack:** Go 1.26, Bubble Tea, lipgloss, `internal/markdown`, real `git` in tests.

**Spec:** `docs/superpowers/specs/2026-09-28-review-view-design.md`

## Global Constraints

- The stored review is ONE note (unchanged from `2026-09-27-review-notes`); only its content changes.
- Document: `{"version":1,"summary":"<markdown>","meta":{},"files":[{"path","summary","meta","annotations":[{"newRange"|"oldRange":[a,b],"summary","rationale","meta"}]}]}`; old `tags`/`confidence` fold into `meta`.
- Agents write ONLY the JSON to `$GG_MESSAGE_FILE`; `$GG_NOTES_FILE` is removed.
- Review notes are read-only: ids `review:<noteID>:<n>`; domain mutations refuse them with `ErrReadOnlyNote`.
- In the review view the diff shows ONLY the review's notes; the normal commit view never shows them.
- The template migration asks for consent (`Lossless: false`); an edited command is never touched; the rest of the config file stays byte-identical.
- Every TUI string via `i18n.T`, all four bundles; engine/CLI prose English.
- TUI/web tests run with `domain.NotesDisabled = true` → a test needing reviews calls `svc.UseNotesDir(t.TempDir())`.
- Commit trailers: `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>` + `Claude-Session: https://claude.ai/code/session_01A4Gy5opUFNLUZk6eNCpi43`.

## Review Focus

1. **An agent that wraps the JSON in a ```json fence or Claude's `--output-format json` envelope** — must parse as structured (Task 1 `TestParseReviewUnwrapsFenceAndEnvelope`).
2. **A note on a file the commit does not change / a line past the end** — goes to "Other notes", never to a missing tree row or a panic (Task 5 `TestReviewNotesOtherSplit`, Task 8 `TestReviewOverviewListsOtherNotes`).
3. **A stored command that differs from an old built-in by the resolved binary path** (`/usr/local/bin/claude` vs `claude`) — still recognised and upgraded with the same binary (Task 4 `TestUpgradeReviewCommandKeepsTheBinary`).
4. **A config file with other content around the command** — only the matched body changes (Task 6 `TestReplaceToolCommandBodiesLeavesTheRestAlone`).
5. **`c`/reply on a review note in the diff** — refused with a clear status, nothing stored (Task 2 `TestReviewNoteIsReadOnly`, Task 8 `TestReviewNoteCannotBeReplied`).

---

### Task 1: `notebatch.ParseReview`

**Files:** Create `internal/notebatch/review.go`, `internal/notebatch/review_test.go`.

**Interfaces — Produces:**
```go
type MetaKV struct{ Key, Value string }
type ReviewNote struct{ Side string; Range [2]int; Summary, Rationale string; Meta []MetaKV }
type ReviewFile struct{ Path, Summary string; Meta []MetaKV; Notes []ReviewNote }
type ReviewDoc struct{ Overview string; Meta []MetaKV; Files []ReviewFile }
var ErrNotReviewDoc = errors.New("notebatch: not a gg review document")
func ParseReview(data []byte) (ReviewDoc, error)
func (d ReviewDoc) Canonical() []byte // indented JSON in the documented shape
func (d ReviewDoc) NoteCount() (notes, files int)
```

- [ ] **Step 1: failing tests** (`review_test.go`):
```go
func TestParseReviewFullDocument(t *testing.T) {
	doc, err := ParseReview([]byte(`{"version":1,"summary":"## Overview\nok","meta":{"verdict":"approve"},
	 "files":[{"path":"a/b.go","summary":"one line","annotations":[
	  {"newRange":[3,4],"summary":"S","rationale":"R","meta":{"severity":"bug","confidence":"high"}},
	  {"oldRange":[7,7],"summary":"gone"}]}]}`))
	if err != nil { t.Fatal(err) }
	if doc.Overview != "## Overview\nok" || len(doc.Files) != 1 || len(doc.Files[0].Notes) != 2 { t.Fatalf("%+v", doc) }
	n := doc.Files[0].Notes[0]
	if n.Side != "new" || n.Range != [2]int{3, 4} || n.Summary != "S" || n.Rationale != "R" { t.Fatalf("%+v", n) }
	if got := n.Meta; len(got) != 2 || got[0] != (MetaKV{"confidence", "high"}) || got[1] != (MetaKV{"severity", "bug"}) { t.Fatalf("meta %+v (want sorted by key)", got) }
	if doc.Files[0].Notes[1].Side != "old" { t.Fatal("oldRange must be the old side") }
	if doc.Meta[0] != (MetaKV{"verdict", "approve"}) { t.Fatalf("%+v", doc.Meta) }
}
func TestParseReviewFoldsOldTagsAndConfidence(t *testing.T) {
	doc, err := ParseReview([]byte(`{"version":1,"summary":"x","files":[{"path":"a","annotations":[{"newRange":[1,1],"summary":"s","tags":["bug","nit"],"confidence":"low"}]}]}`))
	if err != nil { t.Fatal(err) }
	m := doc.Files[0].Notes[0].Meta
	if len(m) != 2 || m[0] != (MetaKV{"confidence", "low"}) || m[1] != (MetaKV{"tags", "bug, nit"}) { t.Fatalf("%+v", m) }
}
func TestParseReviewMetaValues(t *testing.T) {
	doc, _ := ParseReview([]byte(`{"version":1,"summary":"x","meta":{"n":3,"ok":true,"o":{"a":1}}}`))
	want := []MetaKV{{"n", "3"}, {"o", `{"a":1}`}, {"ok", "true"}}
	if !reflect.DeepEqual(doc.Meta, want) { t.Fatalf("%+v", doc.Meta) }
}
func TestParseReviewRejects(t *testing.T) {
	for name, in := range map[string]string{
		"prose":        "The advisor confirms four findings.",
		"no summary":   `{"version":1,"files":[]}`,
		"bad version":  `{"version":2,"summary":"x"}`,
		"both ranges":  `{"version":1,"summary":"x","files":[{"path":"a","annotations":[{"newRange":[1,1],"oldRange":[1,1],"summary":"s"}]}]}`,
		"no range":     `{"version":1,"summary":"x","files":[{"path":"a","annotations":[{"summary":"s"}]}]}`,
		"empty path":   `{"version":1,"summary":"x","files":[{"path":"","annotations":[]}]}`,
		"bad range":    `{"version":1,"summary":"x","files":[{"path":"a","annotations":[{"newRange":[5,2],"summary":"s"}]}]}`,
	} {
		if _, err := ParseReview([]byte(in)); !errors.Is(err, ErrNotReviewDoc) { t.Errorf("%s: err %v, want ErrNotReviewDoc", name, err) }
	}
}
func TestParseReviewUnwrapsFenceAndEnvelope(t *testing.T) {
	body := `{"version":1,"summary":"ok"}`
	for name, in := range map[string]string{
		"fence":    "Here it is:\n```json\n" + body + "\n```\n",
		"envelope": `{"type":"result","result":` + strconv.Quote(body) + `}`,
	} {
		if doc, err := ParseReview([]byte(in)); err != nil || doc.Overview != "ok" { t.Errorf("%s: %+v %v", name, doc, err) }
	}
}
func TestParseReviewCanonicalRoundTrips(t *testing.T) {
	in := `{"version":1,"summary":"o","files":[{"path":"a","annotations":[{"newRange":[1,2],"summary":"s","meta":{"k":"v"}}]}]}`
	doc, _ := ParseReview([]byte(in))
	back, err := ParseReview(doc.Canonical())
	if err != nil || !reflect.DeepEqual(doc, back) { t.Fatalf("%v\n%+v\n%+v", err, doc, back) }
}
```
- [ ] **Step 2:** `go test ./internal/notebatch -run Review` → build failure (undefined `ParseReview`).
- [ ] **Step 3: implement** `review.go`: raw structs with `json.RawMessage` for `meta`; `unwrap(data)`: trim; if it starts with `{` and decodes to an object with a string `result` field and no `version` → use `result`; else if it contains a ```` ``` ```` fence whose body starts with `{` → use the first such body; `strict`: `version` must be 1, `summary` non-empty after TrimSpace, each file path non-empty, each annotation exactly one range with `1 <= a <= b` and non-empty summary; every failure wraps `ErrNotReviewDoc` (`fmt.Errorf("%w: %s", ErrNotReviewDoc, why)`). `metaKVs(raw)`: decode `map[string]json.RawMessage`; value string → as is, else compact JSON text (numbers/bools print naturally); sort by key. Fold `tags` → `MetaKV{"tags", strings.Join(tags, ", ")}` and `confidence` → `MetaKV{"confidence", v}` unless `meta` already has the key. `Canonical()` marshals a struct mirror (`version`, `summary`, `meta` as `map[string]string`, `files` with `newRange`/`oldRange`) with `json.MarshalIndent(..., "", "  ")`.
- [ ] **Step 4:** `go test ./internal/notebatch` → ok.
- [ ] **Step 5:** commit `feat(notebatch): ParseReview — the structured review document`.

### Task 2: read-only `review:` note ids

**Files:** `internal/model/note.go`, `internal/domain/notes.go` (NoteEdit/NoteReply/NoteRemove), tests `internal/model/note_test.go`, `internal/domain/review_notes_test.go`.

**Produces:** `const model.ReviewNoteIDPrefix = "review:"`, `func model.IsReviewNoteID(id string) bool`, `func model.IsReadOnlyNoteID(id string) bool` (forge or review).

- [ ] **Step 1: failing test** `TestReviewNoteIsReadOnly`: `svc.NoteEdit(ctx, "review:abc:0", "x", "")`, `NoteReply(ctx, "review:abc:0", model.Note{Summary:"r"})`, `NoteRemove(ctx, "review:abc:0")` each return `errors.Is(err, ErrReadOnlyNote)`.
- [ ] **Step 2:** run → FAIL (not found / nil).
- [ ] **Step 3:** add the helpers; replace the three `model.IsForgeNoteID(...)` guards in `domain/notes.go` with `model.IsReadOnlyNoteID(...)`.
- [ ] **Step 4:** `go test ./internal/model ./internal/domain -run 'ReadOnly|IsReview'` → ok.
- [ ] **Step 5:** commit `feat(notes): review: note ids are read-only`.

### Task 3: the review context document asks for the document; `$GG_NOTES_FILE` goes

**Files:** `internal/engine/review_changes.go`, its test; callers `internal/domain/review.go` (`ReviewReportNotes`), `internal/domain/task_kinds.go` (`ReviewTask`), `internal/cli/review.go`.

**Produces:** `engine.ReviewChanges` without `NotesFile`; `ReviewReportNotes` renamed back to one `ReviewReport(ctx, target, agent, cmd, env)`; `ReviewTask(ctx, tc, target)` (no notesFile arg); `engine.ReviewOutputInstruction() string` (exported for the skill doc test).

- [ ] **Step 1: failing tests** in `internal/engine/review_changes_test.go`:
```go
func TestReviewContextAlwaysAsksForTheDocument(t *testing.T) {
	op := ReviewChanges{RangeLabel: "a..b"}
	s := op.reviewSummary("/tmp/d.diff", "1\t0\tf.go", false)
	for _, want := range []string{"## Review output", `"version": 1`, `"summary"`, `"annotations"`, `"newRange"`, `"meta"`, "$GG_MESSAGE_FILE", "ONLY"} {
		if !strings.Contains(s, want) { t.Errorf("context lacks %q:\n%s", want, s) }
	}
}
func TestReviewPrepareSetsNoNotesFile(t *testing.T) { /* Prepare with a fake repo (existing test helper in this file); assert no env entry starts with GG_NOTES_FILE= */ }
```
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3:** delete `NotesFile`, `notesInstruction` and the env line; append `ReviewOutputInstruction()` to every `reviewSummary`:
```go
func ReviewOutputInstruction() string {
	return "\n## Review output\n" +
		"Write ONLY this JSON document (no prose around it) to the file named by\n" +
		"$GG_MESSAGE_FILE — it replaces a free-form report:\n\n" +
		"{\n  \"version\": 1,\n  \"summary\": \"<markdown: the overall review — what changed, what matters, the verdict>\",\n" +
		"  \"meta\": { \"verdict\": \"approve | comment | request changes\" },\n" +
		"  \"files\": [\n    { \"path\": \"<repo-relative path>\", \"summary\": \"<optional one line about this file>\",\n" +
		"      \"annotations\": [\n        { \"newRange\": [<first>, <last>], \"summary\": \"<one line>\",\n" +
		"          \"rationale\": \"<why it matters / how it fails>\",\n" +
		"          \"meta\": { \"severity\": \"bug | risk | design | nit\", \"confidence\": \"low | medium | high\" } }\n" +
		"      ] }\n  ]\n}\n\n" +
		"Rules: line numbers are 1-based and inclusive; use \"newRange\" for a line in the\n" +
		"new version of the file and \"oldRange\" for a removed line. \"meta\" is optional\n" +
		"and free-form (string values). Annotate what a reader would not spot; leave\n" +
		"\"files\" empty when there is nothing line-specific to say. Do not modify the\n" +
		"repository and do not commit.\n"
}
```
Update `ReviewReportNotes` → `ReviewReport` (drop `notesFile`), `ReviewTask(ctx, tc, target)`, and every caller (`grep -rn "ReviewReportNotes\|ReviewTask(" internal`); the CLI stops creating the temp notes file (Task 7 finishes the CLI).
- [ ] **Step 4:** `go build ./... && go test ./internal/engine ./internal/domain -run Review` → ok.
- [ ] **Step 5:** commit `feat(engine): the review context asks for the structured document; drop $GG_NOTES_FILE`.

### Task 4: built-in review templates + the superseded list

**Files:** `internal/exttool/exttool.go`, `internal/exttool/review_upgrade.go` (new), tests.

**Produces:** new prompt constants; `var SupersededReviewCommands = []struct{ Old, New string }` (templates with `<bin>`); `func UpgradeReviewCommand(stored string) (string, bool)`.

- [ ] **Step 1: failing tests:**
```go
func TestBuiltInReviewTemplatesAskForTheDocument(t *testing.T) {
	for _, tc := range Catalog() /* the existing catalog accessor */ {
		for _, c := range tc.Commands {
			if c.Category != CatReview { continue }
			if strings.Contains(c.Command, "/code-review") { t.Errorf("%s still runs /code-review", c.Name) }
			if !strings.Contains(c.Command, "Review output") { t.Errorf("%s: prompt does not point at the Review output section", c.Name) }
		}
	}
}
func TestUpgradeReviewCommandKeepsTheBinary(t *testing.T) {
	old := strings.Replace(oldClaudeReviewCommand, "<bin>", "/usr/local/bin/claude", 1)
	got, ok := UpgradeReviewCommand(old)
	if !ok || !strings.HasPrefix(got, "/usr/local/bin/claude -p ") || strings.Contains(got, "/code-review") { t.Fatalf("%v %q", ok, got) }
	if _, ok := UpgradeReviewCommand(old + " --extra"); ok { t.Fatal("an edited command must not match") }
	if _, ok := UpgradeReviewCommand(strings.ReplaceAll(old, "\n", "\r\n")); !ok { t.Fatal("CRLF line ends must still match") }
}
```
(Find the catalog accessor name: `grep -n "^func " internal/exttool/exttool.go`.)
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3:** before editing, copy every current review command constant (claude, junie, kimi, codex, agy, interactiveReviewPrompt-based entries: Claude/Junie/Codex interactive + yolo variants) verbatim into `review_upgrade.go` as `oldClaudeReviewCommand` etc. Then define:
```go
const structuredReviewTask = `You are reviewing a code change. Read the review brief at <env:GG_CONTEXT_FILE> (the full diff is at <env:GG_REVIEW_DIFF>, range <range>) and follow its "Review output" section exactly: write ONLY the JSON review document it describes into the file at <env:GG_MESSAGE_FILE> (an absolute path outside the repository). Do NOT modify any repository files and do NOT run git commit.`
```
Claude capture: `<bin> -p "` + structuredReviewTask + `" --output-format json --permission-mode acceptEdits --allowedTools "Read" "Write" "Bash(git diff *)" "Bash(git log *)" "Bash(git show *)" "Bash(git status *)"` (keep the multi-line `\` layout). Junie/Kimi/Agy: `"` + structuredReviewTask + `"` in their existing flag layout. Codex: final message = the JSON (`--output-last-message` already writes the file): "…your final message must be ONLY the JSON review document…". Interactive: structuredReviewTask + " Overwrite the file each time you revise the review, then wait for further instructions." Fill `SupersededReviewCommands` pairing each old constant with its new one. `UpgradeReviewCommand`: normalise `\r\n`→`\n` and TrimSpace on both; for each pair split `Old` on `<bin>` into prefix/suffix (exactly one `<bin>` at the start in every template) → stored must `HasSuffix(suffix)` and the remainder (the binary) must contain no whitespace or be one double-quoted token; return `strings.Replace(New, "<bin>", bin, 1)`. Update the verification comments ("not re-verified against the live tool: <date>").
- [ ] **Step 4:** `go test ./internal/exttool` → ok (existing template tests may pin old text — update them to the new contract and ledger each).
- [ ] **Step 5:** commit `feat(exttool): built-in review templates ask for the structured document`.

### Task 5: domain — canonical storage, `Review.Doc`, `ReviewNotesFor`, other notes

**Files:** `internal/domain/review_notes.go`, `internal/domain/review.go`, tests in `review_notes_test.go`.

**Produces:**
```go
// SaveReview returns Structured
func (s *Service) SaveReview(ctx, cmd SaveReview) (id, warn string, err error) // unchanged signature
type SaveReviewResult ... // NOT added: Structured is reported via Review(ctx,id).Doc != nil
// Review gains:
//   Doc *notebatch.ReviewDoc // nil = stored as text
func (s *Service) ReviewNotesFor(ctx context.Context, reviewID, path string, d Diff) ([]ResolvedNote, error)
type ReviewOtherNote struct{ Path string; Side string; Range [2]int; Summary string }
func (s *Service) ReviewOtherNotes(ctx context.Context, reviewID string) ([]ReviewOtherNote, error)
func (s *Service) ReviewFileCounts(ctx context.Context, reviewID string) (map[string]int, error) // path → notes on files the commit changes
// ReviewResult (ReviewReport) gains Structured bool
```
- [ ] **Step 1: failing tests:**
  - `TestSaveReviewStoresCanonicalJSON`: text = "```json\n{…valid…}\n```" → `Review(id).Doc != nil`, stored `Text` parses and has no fence.
  - `TestSaveReviewKeepsProseAsText`: prose → `Doc == nil`, `Text` == prose.
  - `TestReviewNotesForAnchorsOnTheCommitDiff`: repo with a commit adding `f.txt` (3 lines), document with a `newRange [2,2]` note on `f.txt` → `ReviewNotesFor(ctx, id, "f.txt", <that commit's Diff via svc.Differ or a Diff built from rows>)` returns one note, id `review:<id>:0`, `Source agent`, `Author` = agent, `Status active`, `Range {2,2}`, and `Note.Tags` = the meta as `"key: value"`.
  - `TestReviewNotesOtherSplit`: notes on `missing.go` and on `f.txt` line 99 → `ReviewOtherNotes` returns both; `ReviewFileCounts` = `{"f.txt": 0 or 1 accordingly}` excludes them.
  - `TestReviewReportReportsStructured`: a fake tool printing a valid document → `res.Structured`; prose → false.
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3:** in `putReview`, `if doc, err := notebatch.ParseReview([]byte(cmd.Text)); err == nil { text = string(doc.Canonical()) }`; `reviewOf` sets `Doc` by parsing `n.Rationale` (nil on error). `ReviewNotesFor`: find the review, `Doc` nil → nil; collect the file's notes (path compare after `filepath.ToSlash`), build `model.Note{ID: fmt.Sprintf("review:%s:%d", id, i), Source: agent, Author: r.Agent, Address: {StateCommitted, Commit: r.Commit, Path: path}, Side: side, Range: rng, Summary, Rationale, Tags: meta as "k: v", Created: r.Created}`, compute `ContextHash` from the diff's own side lines (`anchorLines(lines, rng)` over `sideLinesOf(d, side)` — reuse the helpers `NotesFor`/`resolveNotes` use; read them first), then `resolveNotes(ns, oldLines, newLines)`. Lines past the side's end are skipped here and reported by `ReviewOtherNotes`. `ReviewOtherNotes` / `ReviewFileCounts`: take the commit's changed files (`s.CommitFiles(ctx, r.Commit)`) and each note's side length (`noteSideLines` at `{StateCommitted, Commit, Path}`), split in/out. `ReviewReport` sets `Structured` from `notebatch.ParseReview(report) == nil`.
- [ ] **Step 4:** `go test ./internal/domain -run Review` → ok.
- [ ] **Step 5:** commit `feat(domain): structured reviews — canonical storage and review notes at read time`.

### Task 6: preflight migration of stored old commands

**Files:** `internal/config/tools.go` (+ test), `internal/domain/features.go`, `internal/domain/preflight.go`, `internal/domain/review_upgrade.go` (new) + test.

**Produces:** `func config.ReplaceToolCommandBodies(path string, replace func(body string) (string, bool)) (int, error)`; `FeatureStructuredReviews = "structured-reviews"`, `StoreReviewCommands = "review-commands"`; `(s *Service) legacyReviewCommandsPresent(ctx) bool`; migration action `"upgrade-review-commands"` → `upgradeReviewCommands{Paths []string}` implementing `engine.MigrationAction`.

- [ ] **Step 1: failing tests:**
```go
func TestReplaceToolCommandBodiesLeavesTheRestAlone(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.toml")
	in := "# mine\n[ui]\ntheme = \"dark\"\n\n[[tools.command]]\ncategory = \"review\"\nname = \"Claude\"\ncommand = '''\nOLD\n'''\n\n[[tools.command]]\ncategory = \"review\"\nname = \"Mine\"\ncommand = '''\nKEEP\n'''\n"
	os.WriteFile(p, []byte(in), 0o644)
	n, err := ReplaceToolCommandBodies(p, func(b string) (string, bool) { return "NEW", b == "OLD" })
	got, _ := os.ReadFile(p)
	if err != nil || n != 1 || string(got) != strings.Replace(in, "\nOLD\n", "\nNEW\n", 1) { t.Fatalf("%d %v\n%s", n, err, got) }
}
```
domain: `TestStructuredReviewsMigrationUpgradesOldCommands` — `t.Setenv("XDG_CONFIG_HOME", dir)`, write the global config with an old Claude review block (binary `claude`) and an edited one; `svc.Preflight` → the feature is `Repairable`; `PendingMigrations` includes it and it is NOT lossless; apply it via the existing consent entry point (read `domain/preflight.go` for the apply function the CLI/TUI call) → the old block's body is the new template with `claude`, the edited block unchanged, `Preflight` now `Satisfied`; `RunAutoMigrations` does not touch a fresh old block.
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3:** `ReplaceToolCommandBodies`: scan lines; inside a `[[tools.command]]` block, a line `command = '''` starts a body that ends at a line `'''`; call `replace(strings.TrimRight(body,"\n"))`; on true write the new body (refuse a new body containing `'''`); write with `atomicWriteFile` only when n > 0. Feature in `Features()`:
```go
{
	ID: FeatureStructuredReviews, Criticality: preflight.Optional,
	Requires: []preflight.Requirement{preflight.LegacyStore{Store: StoreReviewCommands}},
	Migrate: &preflight.Migration{Store: StoreReviewCommands, From: 1, To: 2, Action: "upgrade-review-commands",
		Describe: func() preflight.Text { return preflight.Text{Format: "Updates the review commands gg once wrote into your config to the structured-review prompt, so reviews open as a rendered review view. Only commands identical to an old built-in are changed; commands you edited are left alone."} }},
},
```
Probe (in both `probesFrom` and `RunAutoMigrations`' legacy map): `StoreReviewCommands: {Present: s.legacyReviewCommandsPresent(ctx)}` — reads the global path (`config.DefaultGlobalPath()`) and the active repo path (as `EffectiveConfig` computes it), loads each with `config.Load` of that single file (or parses `[[tools.command]]` via the config package's loader), true when any `review` command's `exttool.UpgradeReviewCommand` ok. `migrationAction`: `case "upgrade-review-commands": return upgradeReviewCommands{Paths: s.reviewCommandConfigPaths(ctx)}, nil`; its `Apply` calls `config.ReplaceToolCommandBodies(p, exttool.UpgradeReviewCommand)` per path and sums n; `Describe()` "upgrade review commands". Check the TUI/web/CLI consent screens need no per-feature code (they list `PendingMigrations`); if the TUI notice needs a translation key for the feature, add it ×4.
- [ ] **Step 4:** `go test ./internal/config ./internal/domain -run 'ReplaceToolCommand|StructuredReviews|Preflight|Migration'` → ok.
- [ ] **Step 5:** commit `feat(preflight): migrate stored copies of the old review commands (consent)`.

### Task 7: CLI output

**Files:** `internal/cli/review.go`, `internal/cli/review_test.go`, `internal/agentskill/using-gg.md`, `internal/agentskill/reviewing-with-gg.md`, `internal/agentskill/agentskill.go` (Version, ReviewVersion).

- [ ] **Step 1: failing tests:** a fake tool printing a valid document → stdout = overview + blank line + `f.go:3 — S` (and `f.go:-7 — gone` for an old-side note); stderr `note: <id>`; a prose tool → stdout = the text, stderr has `warning: the review is not in gg review format; stored as text`; `--notes` with a valid document → the notes are stored as permanent notes (`gg note list --rev <sha> --file f.go` shows `S`).
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3:** print from `notebatch.ParseReview(res.Content)`; `--notes` → `importReviewNotes(ctx, svc, target, arg, "", res.Content, …)` (the importer already falls back to a report that starts with `{`; pass the CANONICAL text) and drop the temp notes file. Skill docs: `reviewing-with-gg.md` gains a "Review document" section quoting `engine.ReviewOutputInstruction()`'s schema (add a test that the skill text contains `"annotations"` and `"meta"`); `using-gg.md` updates the `gg review` paragraph (stdout shape, warning, `--notes` = keep permanently). Bump `Version` and `ReviewVersion`; run `go run ./cmd/gg init --update` and commit `.claude/skills/*`.
- [ ] **Step 4:** `go test ./internal/cli -run Review ./internal/agentskill` → ok.
- [ ] **Step 5:** commit `feat(cli): gg review prints the structured review; --notes keeps its notes`.

### Task 8: TUI review view (tree, overview popup, notes in the diff, entry points)

**Files:** create `internal/tui/review_view.go`, `internal/tui/review_view_test.go`; modify `files_view.go` (mode + lines), `model.go` (reviewViewMsg), `note_keys.go` (`loadNotesCmd` review branch), `diff_stack.go`/`stackNotesCmd` (review branch), `diff_notes.go` (`noteBoxTitle` meta), `review.go`/`task_tab.go`/`branch_reviews.go`/`all_notes_popup.go`/`files_view.go` (entry points), i18n ×4.

**Produces:** `func (m Model) openReviewView(id string) (Model, tea.Cmd)`; `Model.filesReview *reviewViewState` (`id`, `review domain.Review`, `counts map[string]int`, `other []domain.ReviewOtherNote`); `contentLine.overview bool`; `func (m Model) openReviewOverview() (Model, tea.Cmd)` (popup).

- [ ] **Step 1: failing tests** (model tests over a real repo: reuse `stackRepoModel`, save a structured review on `commits[0]` with notes on `a.go` new line 3 and on `zzz.go`):
  - `TestReviewViewTree`: `openReviewView(id)` + drain → `m.filesMode == filesModeChanged`, `m.filesReview != nil`, first visible line `≡ Overview` (`overview: true`), the `a.go` row contains `◆1`, the next row is its dim file summary; title `Review: …`.
  - `TestReviewOverviewPopupRendersMarkdown`: enter on Overview → a popup whose lines contain the heading text without `##`, inline code without backticks, and "Other notes" with `zzz.go`.
  - `TestReviewDiffShowsOnlyReviewNotes`: a stored line note on `a.go` at that commit + the review; open `a.go` from the review view → drained `notesLoadedMsg` notes are exactly the review's (`review:` ids), with the meta in the box title; the same file opened from the normal commit view shows only the stored note.
  - `TestReviewNoteCannotBeReplied`: `c` on the review note → status says it is read-only, store unchanged.
  - `TestAtNotesEntryOpensTheReviewView`: from the normal commit files view, enter on the `@notes/` entry → review view (not a diff layer).
  - `TestUnstructuredReviewOpensMarkdownViewer`: a prose review → enter opens the `srcNote` viewer with markdown display on (`d.p.markdown == true` or the chosen flag), no review view.
  - `TestReviewViewEscReturns`: opened from View all notes → esc returns to the popup (`handOffToFilesView`).
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3: implement.** `openReviewView`: load `svc.Review(ctx,id)` off-thread → `reviewViewMsg{review, counts, other}`; nil `Doc` → `openReviewNote(id, title)` with markdown mode; else `openChangedFiles(model.Commit{Hash: r.Commit})` (via `handOffToFilesView` when a popup is open) then set `m.filesReview`, `m.filesTitle = i18n.T("Review: %s", label)`; when the commit-files message lands in review mode, build lines with `reviewTreeLines(state, commitFileLines(files))`: prepend `contentLine{text: "≡ " + i18n.T("Overview"), overview: true, path: ""}` (a selectable non-file row: extend the files-view enter handler: `if l.overview { return m.openReviewOverview() }`), append `"  ◆n"` to rows whose path has a count, insert after each such row `contentLine{text: "      " + summary, heading: true}` (dim, not selectable — check the files-view skip rule for `heading`). Do NOT add the `@notes/` group in review mode. `openReviewOverview`: a `contentPopup` with `prMarkdownLines(r.Doc.Overview, "")`, a blank line, meta chips line, then `i18n.T("Other notes")` heading and `path:line — summary` rows, `popupMax`, `y` copies `Doc.Overview`; enter on an Other-notes row opens that file's content at the commit (`openPreview(hash, path)`; when `ShowFile` fails the preview shows the error). `loadNotesCmd`: when `m.filesReview != nil && v.noteAddr.Commit == m.filesReview.review.Commit` → `svc.ReviewNotesFor(ctx, id, addr.Path, d)` instead of `NotesFor`; same in `stackNotesCmd`. `noteBoxTitle`: for `model.IsReviewNoteID(r.Note.ID)`: `i18n.T("review") + " · " + author + " · " + path R<n>` + `" · " + strings.Join(r.Note.Tags, " · ")`. Reply/edit keys (`c`, `r`, `e`, `x` in the note keys): when the target id is read-only set `m.diffNotice = i18n.T("▸ review notes are read-only — gg review --notes keeps them")` and return. Leaving the files view clears `m.filesReview`. Entry points: `@notes/` line enter (files view, not the diff), Branches review row enter + "Show review", View all notes `anReview` enter, tasks tab `openTask` for a review with `NoteID`, `applyReviewResult` → all call `openReviewView(id)`. The `srcNote` viewer's markdown mode: `openReviewNote` sets `d.markdown = true` and the load path runs `prMarkdownLines` over the bytes (read how `fileViewer` fills lines; add the branch in `fill`). i18n keys ×4: `Overview`, `Review: %s`, `Other notes`, `▸ review notes are read-only — gg review --notes keeps them`, `%s — %d notes on %d files`, `not in gg review format — shown as text`.
- [ ] **Step 4:** `go test ./internal/tui -run 'Review|AllNotes|Branch|TaskTab|MenuLabel|I18n|Bundle'` → ok, then the whole `./internal/tui`.
- [ ] **Step 5:** commit `feat(tui): the review view — tree, overview, review notes in the diff`.

### Task 9: stacked overview element

**Files:** `internal/tui/diff_stack.go`, `diff_stack_render.go`, `diff_stack_keys.go`, test `review_view_test.go`.

**Produces:** `lineProse` line kind; `stackFile.overview bool`, `stackFile.prose []mdRow`.

- [ ] **Step 1: failing test** `TestReviewStackPutsTheOverviewFirst`: review view → `S` (stack) → `v.stk.files[0].overview`, its header row text contains `Overview`, the next lines are the rendered overview (heading text without `##`), and `N` from it lands on the first real file.
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3:** `buildTreeStack`: in review mode prepend `stackFile{overview: true, path: "≡ Overview", load: stackLoaded, prose: mdRows(markdown.Parse(doc.Overview), width-2)}` and skip the `overview` contentLine; `spliceStack`: for an overview file emit header, rule, then one `diffLine{file:i, kind: lineProse, Fold: j}` per prose row (reuse `Fold` as the row index or add a `prose int` field to `diffLine`); `isStop` includes `lineProse` (the cursor can read it); `stackRow`: `case lineProse:` render `colouredLine("  ", row.text, row.cls, nil, lipgloss.NewStyle(), nil, w)`; header for an overview file reads `▾ ≡ Overview`; the loader skips overview files; notes for it are none.
- [ ] **Step 4:** `go test ./internal/tui -run 'Stack|Review'` → ok.
- [ ] **Step 5:** commit `feat(tui): the review overview is the first element of the stack`.

### Task 10: web report dialog

**Files:** `internal/web/review.go`, `internal/web/static/review.js`, `internal/web/static/index.html` (dialog body element), `internal/web/review_test.go`.

- [ ] **Step 1: failing test:** the review op's done payload for a structured document has `overviewMd` (a markdown Doc tree), `notes` (`[{path, line, side, summary, meta}]`) and `structured: true`; for prose, `structured: false` and `report` is the text.
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3:** server: parse `res.Content`; add `overviewMd: markdown.Parse(doc.Overview)`, `notes`, `structured`, `warn`. Client: `openReport` renders `mdHTML(overviewMd, esc)` into a new `<div id="report-md">` (hidden when absent), then a `<ul>` of `path:line — summary` with meta chips; prose falls back to `#report-body` text plus the warning line.
- [ ] **Step 4:** `go test ./internal/web -run Review` → ok.
- [ ] **Step 5:** commit `feat(web): the review dialog renders the structured review`.

### Task 11: docs, headless check, race

- [ ] CHANGELOG (new top section), README (review paragraph: the view, the document, the migration), `docs/CLAUDE-details.md` (a "Review view" subsection: `ParseReview`, `review:` ids, `ReviewNotesFor`, `filesReview`, `lineProse`, the migration and its probe).
- [ ] Build a binary; in a scratch repo run `gg review` with a fake tool that writes a structured document to `$GG_MESSAGE_FILE`; `./tui-capture.sh` the review view: tree, overview popup, a diff with the note box, the stack.
- [ ] `./test.sh race` → all green.
- [ ] commit `docs: the review view`.
