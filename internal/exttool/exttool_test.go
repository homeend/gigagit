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

// TestOptInMarksExactlyTheYoloVariants: OptIn is reserved for the aggressive
// variants; every base template (and Meld, not an agent) stays default-in.
func TestOptInMarksExactlyTheYoloVariants(t *testing.T) {
	for _, tl := range Builtins() {
		for _, ct := range tl.Commands {
			if want := strings.Contains(ct.Name, "(yolo)"); ct.OptIn != want {
				t.Errorf("%s/%s: OptIn = %v, want %v (OptIn ⇔ a yolo variant)", tl.ID, ct.Name, ct.OptIn, want)
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
			if c.OptIn {
				t.Fatalf("%s: no yolo for capture lane", c.Name)
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
