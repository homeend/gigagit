package agentstate

import "testing"

// Captured 2026-10-03 from gg's own emulator (Session.Text(): trailing
// blanks trimmed, a wide glyph's right half is a space) — Antigravity CLI
// 1.2.15 and codex 0.160.0, started in a fresh scratch repository.
const (
	agyIdle = `

      ▄▀▀▄        Antigravity CLI 1.2.15
     ▀▀▀▀▀▀       homeendsleep@gmail.com (Antigravity Starter Quota)
    ▀▀▀▀▀▀▀▀      Gemini 3.8 Flash (High)
   ▄▀▀    ▀▀▄     /tmp/TestZZLiveAgentsagy3070203655/001
  ▄▀▀      ▀▀▄

────────────────────────────────────────────────────────────────────────────────────────────────────
>
────────────────────────────────────────────────────────────────────────────────────────────────────
? for shortcuts                                                              Gemini 3.8 Flash · high
`
	agyWork = `

      ▄▀▀▄        Antigravity CLI 1.2.15
     ▀▀▀▀▀▀       homeendsleep@gmail.com (Antigravity Starter Quota)
    ▀▀▀▀▀▀▀▀      Gemini 3.8 Flash (High)
   ▄▀▀    ▀▀▄     /tmp/TestZZLiveAgentsagy3070203655/001
  ▄▀▀      ▀▀▄

────────────────────────────────────────────────────────────
> Using your shell tool run exactly: sleep 25; echo DONE-42 — then tell me the output.
⢿  Generating...
────────────────────────────────────────────────────────────────────────────────────────────────────
>
────────────────────────────────────────────────────────────────────────────────────────────────────
esc to cancel                                                                Gemini 3.8 Flash · high
`
	agyRun = `

      ▄▀▀▄        Antigravity CLI 1.2.15
     ▀▀▀▀▀▀       homeendsleep@gmail.com (Antigravity Starter Quota)
    ▀▀▀▀▀▀▀▀      Gemini 3.8 Flash (High)
   ▄▀▀    ▀▀▄     /tmp/TestZZLiveAgentsagy3070203655/001
  ▄▀▀      ▀▀▄

────────────────────────────────────────────────────────────
> Using your shell tool run exactly: sleep 25; echo DONE-42 — then tell me the output.

▸ Thought for 2s, 367 tokens
  The command requires a 25-second execution, exceeding the asynchronous wait time. Setting the...

● Bash(sleep 25; echo DONE-42) (ctrl+o to expand)
⡿  Running command...
└ Tip: Use /diff to view uncommitted changes in your workspace.
────────────────────────────────────────────────────────────────────────────────────────────────────
>
────────────────────────────────────────────────────────────────────────────────────────────────────
esc to cancel                                                                Gemini 3.8 Flash · high
`
	agyPerm = `

      ▄▀▀▄        Antigravity CLI 1.2.11
     ▀▀▀▀▀▀       homeendsleep@gmail.com (Antigravity Starter Quota)
    ▀▀▀▀▀▀▀▀      Gemini 3.8 Flash (High)
   ▄▀▀    ▀▀▄     /tmp/TestZZLiveAgentsagy1169563258/001
  ▄▀▀      ▀▀▄

────────────────────────────────────────────────────────────
> Using your shell tool run exactly: sleep 25; echo DONE-42 — then tell me the output.

▸ Thought for 3s, 311 tokens
  The working directory is valid. The command ` + "`" + `sleep 25; echo DONE-42` + "`" + ` will be executed within t...

● Bash(sleep 25; echo DONE-42) (ctrl+o to expand)

Command
────────────────────────────────────────────────────────────────────────────────────────────────────

Requesting permission for:
   sleep 25; echo DONE-42

Run this command?
> 1. Yes, run command
  2. Yes, and always allow in this conversation for commands that start with 'sleep'
  3. Yes, and always allow for commands that start with 'sleep' (Persist to settings.json)
  4. No, cancel

  ↑/↓ Navigate · tab Amend · ctrl+g edit/expand command
esc to cancel                                                                Gemini 3.8 Flash · high
`
	agyTrustLive = `Accessing workspace:

/tmp/TestZZLiveAgentsagy1169563258/001

Do you trust the contents of this project?

Antigravity CLI requires permission to read, edit, and execute files here.

> Yes, I trust this folder
  No, exit

  ↑/↓ Navigate · enter Confirm
                                                                             Gemini 3.8 Flash · high
`
	agyDone = `

      ▄▀▀▄        Antigravity CLI 1.2.15
     ▀▀▀▀▀▀       homeendsleep@gmail.com (Antigravity Starter Quota)
    ▀▀▀▀▀▀▀▀      Gemini 3.8 Flash (High)
   ▄▀▀    ▀▀▄     /tmp/TestZZLiveAgentsagy3070203655/001
  ▄▀▀      ▀▀▄

────────────────────────────────────────────────────────────
> Using your shell tool run exactly: sleep 25; echo DONE-42 — then tell me the output.

▸ Thought for 2s, 367 tokens
  The command requires a 25-second execution, exceeding the asynchronous wait time. Setting the...

● Bash(sleep 25; echo DONE-42) (ctrl+o to expand)

  I have launched the command (sleep 25; echo DONE-42) in the background. I will provide the
  output as soon as it finishes.

  The command has completed. The output is:

    DONE-42

────────────────────────────────────────────────────────────────────────────────────────────────────
>
────────────────────────────────────────────────────────────────────────────────────────────────────
? for shortcuts                                                              Gemini 3.8 Flash · high
`

	codexStart = `
  >_ OpenAI Codex (v0.160.0)
     /tmp/TestZZLiveAgentscodex1482821410/001

  Your move, teammate.





















› Ask Codex to do anything


  ? for shortcuts
`
	codexTrust = `
  Folder access
  /tmp/TestZZLiveAgentscodex1482821410/001

  Trust this folder? Codex can read, edit, and run files here, subject to your permission
  settings. Folder settings can run code automatically, even without a model request. Continue
  only if you trust these files. Your trust decision will be saved.

› 1. Trust and continue
  2. Back to Agent Command Center

  enter continue · esc back
`
)

func TestClassifyAntigravityLive(t *testing.T) {
	r := DefaultRules("antigravity")
	cases := map[string]State{agyIdle: Waiting, agyDone: Waiting, agyWork: Working, agyRun: Working, agyPerm: Question, agyTrustLive: Question}
	for in, want := range cases {
		if got := Classify(r, Tail(in, 15)); got != want {
			t.Errorf("%q…: got %q want %q", in[:min(len(in), 40)], got, want)
		}
	}
	opts := DialogOptions(agyPerm, Tail(agyPerm, 15))
	if len(opts) != 4 || opts[0].Key != "1" || opts[3].Label != "No, cancel" {
		t.Fatalf("permission options = %+v", opts)
	}
	opts = DialogOptions(agyTrustLive, Tail(agyTrustLive, 15))
	if len(opts) != 2 || opts[0].Label != "Yes, I trust this folder" || !opts[0].Current {
		t.Fatalf("trust options = %+v", opts)
	}
}

func TestClassifyCodexLive(t *testing.T) {
	r := DefaultRules("codex")
	if got := Classify(r, Tail(codexStart, 15)); got != Waiting {
		t.Errorf("startup idle: %q", got)
	}
	lines := Tail(codexTrust, 15)
	if got := Classify(r, lines); got != Question {
		t.Errorf("trust: %q", got)
	}
	if opts := Options(lines); len(opts) < 2 || opts[0].Label != "Trust and continue" {
		t.Errorf("trust options = %+v", opts)
	}
}
