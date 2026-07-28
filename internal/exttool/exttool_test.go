package exttool

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/template"
)

// fakeLook simulates exec.LookPath over a fixed set of installed binaries.
func fakeLook(installed map[string]string) func(string) (string, error) {
	return func(name string) (string, error) {
		if p, ok := installed[name]; ok {
			return p, nil
		}
		return "", errors.New("not found")
	}
}

// fakeStat simulates os.Stat over a fixed set of existing paths.
func fakeStat(existing map[string]bool) func(string) (os.FileInfo, error) {
	return func(path string) (os.FileInfo, error) {
		if existing[path] {
			return nil, nil // callers only check the error
		}
		return nil, os.ErrNotExist
	}
}

func TestDetectFindsBinsOnPath(t *testing.T) {
	dets := Detect(fakeLook(map[string]string{"claude": "/usr/bin/claude", "meld": "/usr/bin/meld"}), fakeStat(nil), "")
	got := map[string]string{}
	for _, d := range dets {
		got[d.Tool.ID] = d.Bin
	}
	if got["claude"] != "claude" {
		t.Errorf("claude Bin = %q, want bare name %q (PATH hit)", got["claude"], "claude")
	}
	if got["meld"] != "meld" {
		t.Errorf("meld Bin = %q, want %q", got["meld"], "meld")
	}
	if _, ok := got["junie"]; ok {
		t.Errorf("junie detected but not installed")
	}
}

func TestDetectExtraProbeYieldsAbsolutePath(t *testing.T) {
	// Meld's Windows install dir is off PATH; an ExtraProbes hit must return
	// the absolute path so the generated command can run it.
	var meld Tool
	for _, tl := range Builtins() {
		if tl.ID == "meld" {
			meld = tl
		}
	}
	if len(meld.ExtraProbes) == 0 {
		t.Fatal("meld has no ExtraProbes; expected the Windows install path")
	}
	probe := meld.ExtraProbes[0]
	dets := Detect(fakeLook(nil), fakeStat(map[string]bool{probe: true}), "")
	if len(dets) != 1 || dets[0].Tool.ID != "meld" || dets[0].Bin != probe {
		t.Fatalf("dets = %+v, want one meld detection with Bin=%q", dets, probe)
	}
}

func TestDetectTildeProbeExpandsAgainstHome(t *testing.T) {
	tool := Tool{ID: "fake", Label: "Fake", Bins: []string{"fakebin"},
		ExtraProbes: []string{"~/.fake/bin/fake"},
		Commands:    []CommandTemplate{{Category: CatConflict, Name: "Fake", Mode: ModeTerminal, Command: "<bin>"}}}

	// ~/ probe expands against home; the expanded absolute path is the Bin.
	want := filepath.Join("/home/u", ".fake", "bin", "fake")
	dets := detectIn([]Tool{tool}, fakeLook(nil), fakeStat(map[string]bool{want: true}), "/home/u")
	if len(dets) != 1 || dets[0].Bin != want {
		t.Fatalf("dets = %+v, want one detection with Bin=%q", dets, want)
	}

	// Empty home skips ~/ probes entirely (hermeticity — tests must never
	// resolve against the developer's real home).
	dets = detectIn([]Tool{tool}, fakeLook(nil), fakeStat(map[string]bool{want: true}), "")
	if len(dets) != 0 {
		t.Fatalf("empty home must skip ~/ probes, got %+v", dets)
	}

	// A PATH hit still wins over the probe and keeps the bare name.
	dets = detectIn([]Tool{tool}, fakeLook(map[string]string{"fakebin": "/usr/bin/fakebin"}), fakeStat(nil), "/home/u")
	if len(dets) != 1 || dets[0].Bin != "fakebin" {
		t.Fatalf("PATH hit must win with bare name, got %+v", dets)
	}
}

func TestBuiltinsCatalogInvariants(t *testing.T) {
	seen := map[string]bool{}
	for _, tl := range Builtins() {
		if tl.ID == "" || tl.Label == "" || len(tl.Bins) == 0 {
			t.Errorf("tool %+v: ID/Label/Bins must be set", tl)
		}
		for _, ct := range tl.Commands {
			switch ct.Category {
			case CatConflict, CatCommitMessage, CatReview:
			default:
				t.Errorf("%s/%s: bad category %q", tl.ID, ct.Name, ct.Category)
			}
			switch ct.Mode {
			case ModeTerminal, ModeCapture:
			default:
				t.Errorf("%s/%s: bad mode %q", tl.ID, ct.Name, ct.Mode)
			}
			switch ct.WhenOp {
			case "", "merge", "rebase", "cherry-pick", "revert":
			default:
				t.Errorf("%s/%s: bad when_op %q", tl.ID, ct.Name, ct.WhenOp)
			}
			if ct.PerFile && ct.Category != CatConflict {
				t.Errorf("%s/%s: per_file outside conflict", tl.ID, ct.Name)
			}
			if !strings.Contains(ct.Command, "<bin>") {
				t.Errorf("%s/%s: command must start from <bin>", tl.ID, ct.Name)
			}
			// Injection posture: a default must never substitute a raw prose
			// value into shell text — only <bin>/<env:...>/path/enum tokens.
			for _, badTok := range []string{"<op>", "<source>", "<target>", "<conflicted-files>"} {
				if strings.Contains(ct.Command, badTok) {
					t.Errorf("%s/%s: raw prose token %s in a default command", tl.ID, ct.Name, badTok)
				}
			}
			key := string(ct.Category) + "\x00" + ct.Name
			if seen[key] {
				t.Errorf("duplicate (category,name): %s/%s", ct.Category, ct.Name)
			}
			seen[key] = true
		}
	}
}

// findTemplate returns the catalog template with the given (category, name),
// failing the test when it is missing.
func findTemplate(t *testing.T, cat Category, name string) CommandTemplate {
	t.Helper()
	for _, tl := range Builtins() {
		for _, ct := range tl.Commands {
			if ct.Category == cat && ct.Name == name {
				return ct
			}
		}
	}
	t.Fatalf("template %s/%s not in catalog", cat, name)
	return CommandTemplate{}
}

// TestClaudeYoloTemplate pins the opt-in yolo variant's contract: OptIn set,
// --dangerously-skip-permissions present, NO allow/disallow flags (bypass
// mode skips permission evaluation entirely, listing them would be dead
// weight), and — the argv-order live bug — the double-quoted prompt as the
// FIRST argument after <bin>, before any flag.
func TestClaudeYoloTemplate(t *testing.T) {
	ct := findTemplate(t, CatConflict, "Claude (yolo)")
	if !ct.OptIn {
		t.Error("Claude (yolo) must be OptIn (wizard-unchecked by default)")
	}
	if ct.Mode != ModeTerminal {
		t.Errorf("mode = %q, want %q", ct.Mode, ModeTerminal)
	}
	if !strings.Contains(ct.Command, "--dangerously-skip-permissions") {
		t.Error("yolo command must carry --dangerously-skip-permissions")
	}
	for _, flag := range []string{"--allowedTools", "--disallowedTools", "--permission-mode"} {
		if strings.Contains(ct.Command, flag) {
			t.Errorf("yolo command must not carry %s (bypass mode ignores permission rules)", flag)
		}
	}
	// The prompt (a double-quoted string opening right after <bin>) precedes
	// every flag — a flag before the prompt would eat it (the variadic-flag
	// live bug documented on claudeConflictCommand).
	if !strings.HasPrefix(ct.Command, `<bin> "`) {
		t.Errorf("prompt must be the first argument after <bin>: %q", ct.Command)
	}
	if strings.Index(ct.Command, "--dangerously-skip-permissions") < strings.Index(ct.Command, `"A git`) {
		t.Errorf("flag precedes the prompt: %q", ct.Command)
	}
	// The prompt keeps its do-NOT-commit clause as guidance.
	if !strings.Contains(ct.Command, "Do NOT run git commit") {
		t.Error("yolo prompt must keep the do-NOT-commit guidance clause")
	}
	// Both Claude templates share the exact same prompt text.
	base := findTemplate(t, CatConflict, "Claude")
	if !strings.HasPrefix(base.Command, `<bin> `+claudeConflictPrompt) || !strings.HasPrefix(ct.Command, `<bin> `+claudeConflictPrompt) {
		t.Error("Claude and Claude (yolo) must share claudeConflictPrompt verbatim")
	}
	// The generated command validates on both OS renderings (also covered by
	// the blanket TestBuiltinTemplateTokensValidate; asserted here so this
	// test pins the yolo contract end to end on its own).
	for _, goos := range []string{"linux", "windows"} {
		gen := GenerateCommandFor(ct, "claude", goos)
		if err := template.ValidateCommandTokens(gen, ct.PerFile); err != nil {
			t.Errorf("GenerateCommandFor(%s): %v", goos, err)
		}
	}
}

// TestJunieYoloTemplates pins the Junie opt-in variant: --brave appended to
// the same --prompt invocation, no when_op filter (Junie has no --merge/
// --rebase to gate on real installs — see Builtins' doc comment).
// (--brave verified against a live Junie CLI on 2026-07-05: "Turns on Brave
// Mode (interactive only)" — gg's terminal handover IS Junie's interactive
// mode.)
func TestJunieYoloTemplates(t *testing.T) {
	ct := findTemplate(t, CatConflict, "Junie (yolo)")
	if !ct.OptIn {
		t.Error("Junie (yolo) must be OptIn (wizard-unchecked by default)")
	}
	if ct.WhenOp != "" {
		t.Errorf("Junie (yolo) WhenOp = %q, want \"\" (any paused op)", ct.WhenOp)
	}
	if !strings.HasSuffix(ct.Command, " --brave") {
		t.Errorf("Junie (yolo) Command = %q, want it to end with --brave", ct.Command)
	}
	base := findTemplate(t, CatConflict, "Junie")
	if want := base.Command + " --brave"; ct.Command != want {
		t.Errorf("Junie (yolo) Command = %q, want %q", ct.Command, want)
	}
	if !strings.Contains(ct.Command, "--prompt") {
		t.Errorf("Junie (yolo) Command = %q, want it to use --prompt", ct.Command)
	}
}

// TestOptInMarksExactlyThePermissionBypassVariants: OptIn's meaning is "this
// template bypasses the agent's own permission prompts" — the wizard shows
// such rows unchecked. That is exactly the set of templates carrying a
// bypass flag: --dangerously-* (claude, codex, antigravity), --yolo (kimi),
// --brave (junie). Name-based "(yolo)" matching stopped being the rule when
// antigravity's capture lanes (which NEED the bypass to work headless at
// all) joined as OptIn rows without the suffix.
func TestOptInMarksExactlyThePermissionBypassVariants(t *testing.T) {
	bypass := func(cmd string) bool {
		return strings.Contains(cmd, "--dangerously-") ||
			strings.Contains(cmd, "--yolo") ||
			strings.Contains(cmd, "--brave")
	}
	for _, tl := range Builtins() {
		for _, ct := range tl.Commands {
			if want := bypass(ct.Command); ct.OptIn != want {
				t.Errorf("%s/%s: OptIn = %v, want %v (OptIn ⇔ a permission-bypass flag)", tl.ID, ct.Name, ct.OptIn, want)
			}
			if strings.Contains(ct.Name, "(yolo)") && !ct.OptIn {
				t.Errorf("%s/%s: a (yolo)-named variant must be OptIn", tl.ID, ct.Name)
			}
		}
	}
}

func TestGenerateCommand(t *testing.T) {
	ct := CommandTemplate{Command: "<bin> --auto-merge <local>"}
	if got := GenerateCommand(ct, "meld"); got != "meld --auto-merge <local>" {
		t.Errorf("bare bin: got %q", got)
	}
	if got := GenerateCommand(ct, `C:\Program Files\Meld\Meld.exe`); got != `"C:\Program Files\Meld\Meld.exe" --auto-merge <local>` {
		t.Errorf("spaced bin must be double-quoted: got %q", got)
	}
}

func TestGenerateCommandForEnvToken(t *testing.T) {
	ct := CommandTemplate{Command: "<bin> --merge <env:GG_SOURCE>"}
	if got := GenerateCommandFor(ct, "junie", "linux"); got != `junie --merge ${GG_SOURCE}` {
		t.Errorf("linux: got %q", got)
	}
	if got := GenerateCommandFor(ct, "junie", "windows"); got != `junie --merge %GG_SOURCE%` {
		t.Errorf("windows: got %q", got)
	}
	// <bin> substitution still happens alongside <env:...> rendering.
	if got := GenerateCommandFor(ct, "junie", "darwin"); got != `junie --merge ${GG_SOURCE}` {
		t.Errorf("non-windows treated as POSIX: got %q", got)
	}
}

// TestGenerateCommandForEnvTokenNestsInDoubleQuotedPrompt is the hardening
// regression: ${NAME} must survive as ONE word inside a template's own
// double-quoted prompt string, even when the underlying value contains a
// space — the reason the spec mandates ${NAME} over "$NAME" (which would
// close the prompt's quote early and re-open a second one, splitting the
// value into two argv words at the space).
func TestGenerateCommandForEnvTokenNestsInDoubleQuotedPrompt(t *testing.T) {
	ct := CommandTemplate{Command: `<bin> "Read the file at <env:GG_CONTEXT_FILE> and summarize it."`}
	got := GenerateCommandFor(ct, "claude", "linux")
	want := `claude "Read the file at ${GG_CONTEXT_FILE} and summarize it."`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestBuiltinsCommitMessageTemplates pins the stage-2 commit_message catalog
// rows (Claude, Junie): capture mode, no yolo variant, and the argv-order
// contract that bit Stage 1 — the prompt is the first argument after <bin>
// (here after `<bin> -p`), with Claude's variadic --allowedTools coming LAST.
func TestBuiltinsCommitMessageTemplates(t *testing.T) {
	var claude, junie *CommandTemplate
	for _, tl := range Builtins() {
		for i := range tl.Commands {
			c := &tl.Commands[i]
			if c.Category != CatCommitMessage {
				continue
			}
			if c.Mode != ModeCapture {
				t.Fatalf("%s: commit_message must be capture", c.Name)
			}
			switch tl.ID {
			case "claude":
				claude = c
			case "junie":
				junie = c
			}
		}
	}
	if claude == nil || junie == nil {
		t.Fatal("want claude + junie commit_message templates")
	}

	// Prompt is the FIRST arg after <bin> (-p/--task); env tokens render per-OS.
	gen := GenerateCommandFor(*claude, "claude", "linux")
	if !strings.HasPrefix(gen, `claude -p "`) {
		t.Fatalf("claude prompt not first: %q", gen)
	}
	if !strings.Contains(gen, "${GG_CONTEXT_FILE}") || !strings.Contains(gen, "${GG_STAGED_DIFF}") {
		t.Fatalf("claude missing env refs: %q", gen)
	}
	// Variadic --allowedTools must come AFTER the prompt.
	if strings.Index(gen, "--allowedTools") < strings.Index(gen, `"`) {
		t.Fatal("allowedTools before prompt")
	}

	// Junie is a task-agent: it returns the message by WRITING it to
	// $GG_MESSAGE_FILE (the engine reads that file and prefers it over stdout),
	// not by printing it, so its template must reference GG_MESSAGE_FILE. The
	// prompt is the first arg after `--task`, with the input context files too.
	gj := GenerateCommandFor(*junie, "junie", "linux")
	if !strings.HasPrefix(gj, `junie --task "`) {
		t.Fatalf("junie prompt not first after --task: %q", gj)
	}
	if !strings.Contains(gj, "${GG_MESSAGE_FILE}") {
		t.Fatalf("junie commit template must write to GG_MESSAGE_FILE: %q", gj)
	}
	if !strings.Contains(gj, "${GG_CONTEXT_FILE}") || !strings.Contains(gj, "${GG_STAGED_DIFF}") {
		t.Fatalf("junie missing input env refs: %q", gj)
	}
}

func TestBuiltinsReviewTemplates(t *testing.T) {
	var claude, junie *CommandTemplate
	for _, tl := range Builtins() {
		for i := range tl.Commands {
			c := &tl.Commands[i]
			if c.Category != CatReview {
				continue
			}
			if c.Mode != ModeCapture {
				t.Fatalf("%s: review must be capture (verified 2026-07-07)", c.Name)
			}
			switch tl.ID {
			case "claude":
				claude = c
			case "junie":
				junie = c
			}
		}
	}
	if claude == nil || junie == nil {
		t.Fatal("want claude + junie review templates")
	}
	gc := GenerateCommandFor(*claude, "claude", "linux")
	if !strings.Contains(gc, "/code-review <range>") {
		t.Fatalf("claude review must run /code-review over <range>: %q", gc)
	}
	gj := GenerateCommandFor(*junie, "junie", "linux")
	if !strings.Contains(gj, "${GG_MESSAGE_FILE}") || !strings.Contains(gj, "${GG_REVIEW_DIFF}") {
		t.Fatalf("junie review must write GG_MESSAGE_FILE and read GG_REVIEW_DIFF: %q", gj)
	}
	if !strings.HasPrefix(gj, `junie --task "`) {
		t.Fatalf("junie prompt must be first after --task: %q", gj)
	}
}

// TestKimiTemplates pins the Kimi Code catalog rows (verified against a live
// kimi 0.27.0 on 2026-07-20): commit_message and review are capture-mode
// `-p` runs that return through $GG_MESSAGE_FILE (print-mode stdout is a
// report, never the answer); the conflict row is the same `-p` shape in the
// background capture lane — `kimi -p` draws no terminal UI until its final
// response, so a handover would leave the user staring at a dead screen.
// kimi rejects `-y`/`--auto` with `-p`, and print mode approves the edits
// itself (live probe: resolved a real paused merge, edit + `git add`, exit 0).
func TestKimiTemplates(t *testing.T) {
	cm := findTemplate(t, CatCommitMessage, "Kimi")
	gc := GenerateCommandFor(cm, "kimi", "linux")
	if !strings.HasPrefix(gc, `kimi -p "`) {
		t.Fatalf("kimi commit prompt not first after -p: %q", gc)
	}
	for _, ref := range []string{"${GG_MESSAGE_FILE}", "${GG_CONTEXT_FILE}", "${GG_STAGED_DIFF}"} {
		if !strings.Contains(gc, ref) {
			t.Fatalf("kimi commit template missing %s: %q", ref, gc)
		}
	}

	rv := findTemplate(t, CatReview, "Kimi")
	gr := GenerateCommandFor(rv, "kimi", "linux")
	if !strings.Contains(gr, "${GG_MESSAGE_FILE}") || !strings.Contains(gr, "${GG_REVIEW_DIFF}") {
		t.Fatalf("kimi review must write GG_MESSAGE_FILE and read GG_REVIEW_DIFF: %q", gr)
	}
	if !strings.Contains(gr, "<range>") {
		t.Fatalf("kimi review must carry the <range> token: %q", gr)
	}

	cf := findTemplate(t, CatConflict, "Kimi")
	if cf.OptIn || cf.Mode != ModeCapture {
		t.Fatalf("kimi conflict must be a plain background-capture entry (no yolo variant exists): %+v", cf)
	}
	if !strings.HasPrefix(cf.Command, `<bin> -p "`) {
		t.Fatalf("kimi conflict command = %q, want `<bin> -p \"…\"", cf.Command)
	}
	if !strings.Contains(cf.Command, "Do NOT run git commit") {
		t.Fatal("kimi conflict prompt must keep the sequencer-boundary clause")
	}
	for _, goos := range []string{"linux", "windows"} {
		for _, ct := range []CommandTemplate{cm, rv, cf} {
			gen := GenerateCommandFor(ct, "kimi", goos)
			if err := template.ValidateCommandTokens(gen, ct.PerFile); err != nil {
				t.Errorf("GenerateCommandFor(%s) %s: %v", goos, ct.Name, err)
			}
		}
	}
}

// TestCodexTemplates pins the verified codex shapes (codex-cli 0.144.6,
// probed 2026-07-20): exec is the capture lane, the final message arrives
// via --output-last-message (the native GG_MESSAGE_FILE channel), and the
// file argument is double-quoted in the template so a temp path with spaces
// cannot word-split — the first standalone <env:> use in the catalog.
func TestCodexTemplates(t *testing.T) {
	var codex Tool
	for _, tl := range Builtins() {
		if tl.ID == "codex" {
			codex = tl
		}
	}
	if codex.ID == "" {
		t.Fatal("codex not in catalog")
	}
	byName := map[string]CommandTemplate{}
	for _, ct := range codex.Commands {
		byName[string(ct.Category)+"/"+ct.Name] = ct
	}

	commit := byName["commit_message/Codex"]
	gen := GenerateCommandFor(commit, "codex", "linux")
	if !strings.HasPrefix(gen, `codex exec "`) {
		t.Fatalf("codex commit prompt not first after exec: %q", gen)
	}
	if !strings.Contains(gen, `--output-last-message "${GG_MESSAGE_FILE}"`) {
		t.Fatalf("codex commit must write the quoted message file: %q", gen)
	}
	if !strings.Contains(gen, "--sandbox read-only") {
		t.Fatalf("codex capture lanes must be read-only sandboxed: %q", gen)
	}

	review := byName["review/Codex"]
	gr := GenerateCommandFor(review, "codex", "linux")
	if !strings.Contains(gr, "${GG_REVIEW_DIFF}") || !strings.Contains(gr, "<range>") {
		t.Fatalf("codex review must read GG_REVIEW_DIFF and label <range>: %q", gr)
	}
	if !strings.Contains(gr, `--output-last-message "${GG_MESSAGE_FILE}"`) {
		t.Fatalf("codex review must write the quoted message file: %q", gr)
	}

	yolo := byName["conflict/Codex (yolo)"]
	if !yolo.OptIn {
		t.Fatal("codex yolo conflict must be OptIn")
	}
	if !strings.Contains(yolo.Command, "--dangerously-bypass-approvals-and-sandbox") {
		t.Fatalf("codex yolo must bypass approvals: %q", yolo.Command)
	}
	if def := byName["conflict/Codex"]; def.OptIn || strings.Contains(def.Command, "--dangerously-") {
		t.Fatalf("default codex conflict must not bypass approvals: %+v", def)
	}
}

// TestAntigravityTemplates pins the verified agy shapes (agy 1.1.4, probed
// 2026-07-20). Headless -p AUTO-DENIES every permission-gated tool (even
// read_file on gg's context files, which live outside the workspace);
// --mode accept-edits does not lift it and agy has no CLI allowlist flag.
// The only per-run remedy is --dangerously-skip-permissions, so BOTH
// capture templates carry it and are OptIn — the pairing is the safety
// property. The conflict default runs interactively (TTY) where agy
// prompts normally, so it carries no bypass flag.
func TestAntigravityTemplates(t *testing.T) {
	var agy Tool
	for _, tl := range Builtins() {
		if tl.ID == "antigravity" {
			agy = tl
		}
	}
	if agy.ID == "" {
		t.Fatal("antigravity not in catalog")
	}
	if len(agy.Bins) != 1 || agy.Bins[0] != "agy" {
		t.Fatalf("antigravity Bins = %v, want [agy]", agy.Bins)
	}
	for _, ct := range agy.Commands {
		if ct.Mode == ModeCapture {
			if !ct.OptIn || !strings.Contains(ct.Command, "--dangerously-skip-permissions") {
				t.Errorf("%s/%s: capture lane must pair OptIn with --dangerously-skip-permissions: OptIn=%v cmd=%q",
					ct.Category, ct.Name, ct.OptIn, ct.Command)
			}
			if !strings.Contains(ct.Command, "${GG_MESSAGE_FILE}") && !strings.Contains(ct.Command, "<env:GG_MESSAGE_FILE>") {
				t.Errorf("%s/%s: capture lane must use the GG_MESSAGE_FILE channel: %q", ct.Category, ct.Name, ct.Command)
			}
		}
	}
	byName := map[string]CommandTemplate{}
	for _, ct := range agy.Commands {
		byName[string(ct.Category)+"/"+ct.Name] = ct
	}
	def := byName["conflict/Antigravity"]
	if def.OptIn || strings.Contains(def.Command, "--dangerously-") {
		t.Fatalf("default conflict lane must not bypass permissions: %+v", def)
	}
	if !strings.Contains(def.Command, "--prompt-interactive") {
		t.Fatalf("conflict lane must pre-submit the prompt interactively: %q", def.Command)
	}
	gen := GenerateCommandFor(byName["commit_message/Antigravity"], "agy", "linux")
	if !strings.HasPrefix(gen, `agy -p "`) {
		t.Fatalf("agy commit prompt not first after -p: %q", gen)
	}
	if !strings.Contains(gen, "${GG_CONTEXT_FILE}") || !strings.Contains(gen, "${GG_STAGED_DIFF}") {
		t.Fatalf("agy commit missing input env refs: %q", gen)
	}
	gr := GenerateCommandFor(byName["review/Antigravity"], "agy", "linux")
	if !strings.Contains(gr, "${GG_REVIEW_DIFF}") || !strings.Contains(gr, "<range>") {
		t.Fatalf("agy review must read GG_REVIEW_DIFF and label <range>: %q", gr)
	}
}

// A Windows config must be materialized as a SINGLE line: cmd.exe cannot run
// a quoted string that spans lines or a POSIX continuation, and the generated
// text is what the approval popup shows before the first run — gg must not
// execute something the user did not see.
func TestGenerateCommandForWindowsIsSingleLine(t *testing.T) {
	for _, tc := range []struct{ name, mustEnd string }{
		{"Claude (yolo)", "--dangerously-skip-permissions"},
		{"Claude", `"Bash(git push *)"`},
	} {
		tmpl, ok := conflictTemplate(t, "claude", tc.name)
		if !ok {
			t.Fatalf("no %q conflict template in the catalog", tc.name)
		}
		got := GenerateCommandFor(tmpl, "claude", "windows")
		if strings.Contains(got, "\n") {
			t.Errorf("%s: still spans lines:\n%s", tc.name, got)
		}
		if !strings.HasSuffix(strings.TrimSpace(got), tc.mustEnd) {
			t.Errorf("%s: does not end with %s:\n%s", tc.name, tc.mustEnd, got)
		}
		if strings.Contains(got, `\`+"\n") || strings.HasSuffix(strings.TrimSpace(got), `\`) {
			t.Errorf("%s: a continuation backslash survived:\n%s", tc.name, got)
		}
		// POSIX keeps the readable multi-line form — its shell accepts it.
		if posix := GenerateCommandFor(tmpl, "claude", "linux"); !strings.Contains(posix, "\n") {
			t.Errorf("%s: the POSIX form should be unchanged (multi-line)", tc.name)
		}
	}
}

func conflictTemplate(t *testing.T, toolID, name string) (CommandTemplate, bool) {
	t.Helper()
	for _, tool := range Builtins() {
		if tool.ID != toolID {
			continue
		}
		for _, c := range tool.Commands {
			if c.Category == CatConflict && c.Name == name {
				return c, true
			}
		}
	}
	return CommandTemplate{}, false
}
