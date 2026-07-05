package exttool

import (
	"errors"
	"os"
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
	dets := Detect(fakeLook(map[string]string{"claude": "/usr/bin/claude", "meld": "/usr/bin/meld"}), fakeStat(nil))
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
	dets := Detect(fakeLook(nil), fakeStat(map[string]bool{probe: true}))
	if len(dets) != 1 || dets[0].Tool.ID != "meld" || dets[0].Bin != probe {
		t.Fatalf("dets = %+v, want one meld detection with Bin=%q", dets, probe)
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

// TestJunieYoloTemplates pins the Junie opt-in variants: --brave appended to
// the same --merge/--rebase invocations, gated by the same when_op filters.
// (--brave verified against Junie 26.6.8 on 2026-07-05: "Turns on Brave Mode
// (interactive only)" — gg's terminal handover IS Junie's interactive mode.)
func TestJunieYoloTemplates(t *testing.T) {
	for _, tc := range []struct {
		name, whenOp, want string
	}{
		{"Junie merge (yolo)", "merge", "<bin> --merge <env:GG_SOURCE> --brave"},
		{"Junie rebase (yolo)", "rebase", "<bin> --rebase <env:GG_SOURCE> --brave"},
	} {
		ct := findTemplate(t, CatConflict, tc.name)
		if !ct.OptIn {
			t.Errorf("%s must be OptIn", tc.name)
		}
		if ct.WhenOp != tc.whenOp {
			t.Errorf("%s WhenOp = %q, want %q", tc.name, ct.WhenOp, tc.whenOp)
		}
		if ct.Command != tc.want {
			t.Errorf("%s Command = %q, want %q", tc.name, ct.Command, tc.want)
		}
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

func TestStage1CatalogIsConflictOnly(t *testing.T) {
	for _, tl := range Builtins() {
		for _, ct := range tl.Commands {
			if ct.Category != CatConflict {
				t.Errorf("stage 1 ships conflict templates only; found %s/%s", ct.Category, ct.Name)
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
