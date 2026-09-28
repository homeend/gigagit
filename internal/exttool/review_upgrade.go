package exttool

import "strings"

// Every earlier built-in review template, kept verbatim so a stored copy can be
// recognised and upgraded (the structured-reviews preflight migration). First-run
// detection writes the built-in templates INTO the user's config, so a machine
// that ran gg before the review document existed still holds these commands.
// Never edit these constants: they must match what older gg versions wrote.
// When a review template changes again, move the replaced text here and add a
// pair to SupersededReviewCommands.

const oldClaudeReviewCommand = `<bin> -p "/code-review <range>" \
  --output-format json \
  --permission-mode acceptEdits \
  --allowedTools "Read" "Bash(git diff *)" "Bash(git log *)" "Bash(git show *)" "Bash(git status *)"`

const oldJunieReviewPrompt = `"You are reviewing a code change. The full diff to review is in the file at <env:GG_REVIEW_DIFF> (range <range>). Write a concise code review — findings with severity and a short summary — into the file at <env:GG_MESSAGE_FILE> (an absolute path outside the repository). Do NOT modify any repository files and do NOT run git commit."`

const oldJunieReviewCommand = `<bin> --task ` + oldJunieReviewPrompt + ` --output-format json --skip-update-check`

const oldKimiReviewPrompt = `"You are reviewing a code change. The full diff to review is in the file at <env:GG_REVIEW_DIFF> (range <range>). Write a concise code review — findings with severity and a short summary — into the file at <env:GG_MESSAGE_FILE> (an absolute path outside the repository). Do NOT modify any repository files and do NOT run git commit."`

const oldKimiReviewCommand = `<bin> -p ` + oldKimiReviewPrompt

const oldCodexReviewPrompt = `"You are reviewing a code change. The full diff to review is in the file at <env:GG_REVIEW_DIFF> (range <range>). Your final message must be ONLY a concise code review - findings with severity and a short summary. Do NOT modify any repository files and do NOT run git commit."`

const oldCodexReviewCommand = `<bin> exec ` + oldCodexReviewPrompt + ` --sandbox read-only --output-last-message "<env:GG_MESSAGE_FILE>"`

const oldAgyReviewPrompt = `"You are reviewing a code change. The full diff to review is in the file at <env:GG_REVIEW_DIFF> (range <range>). Write a concise code review - findings with severity and a short summary - into the file at <env:GG_MESSAGE_FILE> (an absolute path outside the repository). Do NOT modify any repository files and do NOT run git commit."`

const oldAgyReviewCommand = `<bin> -p ` + oldAgyReviewPrompt + ` --dangerously-skip-permissions`

const oldInteractiveReviewPrompt = `"You are reviewing a code change. The summary is at <env:GG_CONTEXT_FILE> and the full diff at <env:GG_REVIEW_DIFF> (range <range>). Write a concise code review - findings with severity and a short summary - into the file at <env:GG_MESSAGE_FILE> (an absolute path outside the repository), overwriting it each time you revise the review. Do NOT modify any repository files and do NOT run git commit. Then wait for further instructions."`

// SupersededReviewCommands pairs every earlier built-in review template with
// the current one that replaces it. Templates, not commands: both still carry
// <bin> and <env:…> tokens.
var SupersededReviewCommands = []struct{ Old, New string }{
	{oldClaudeReviewCommand, claudeReviewCommand},
	{oldJunieReviewCommand, junieReviewCommand},
	{oldKimiReviewCommand, kimiReviewCommand},
	{oldCodexReviewCommand, codexReviewCommand},
	{oldAgyReviewCommand, agyReviewCommand},
	{`<bin> ` + oldInteractiveReviewPrompt, `<bin> ` + interactiveReviewPrompt},
	{`<bin> ` + oldInteractiveReviewPrompt + ` --dangerously-skip-permissions`, `<bin> ` + interactiveReviewPrompt + ` --dangerously-skip-permissions`},
	{`<bin> ` + oldInteractiveReviewPrompt + ` --dangerously-bypass-approvals-and-sandbox`, `<bin> ` + interactiveReviewPrompt + ` --dangerously-bypass-approvals-and-sandbox`},
	{`<bin> --prompt ` + oldInteractiveReviewPrompt, `<bin> --prompt ` + interactiveReviewPrompt},
	{`<bin> --prompt ` + oldInteractiveReviewPrompt + ` --brave`, `<bin> --prompt ` + interactiveReviewPrompt + ` --brave`},
	{`<bin> --prompt-interactive ` + oldInteractiveReviewPrompt, `<bin> --prompt-interactive ` + interactiveReviewPrompt},
	{`<bin> --prompt-interactive ` + oldInteractiveReviewPrompt + ` --dangerously-skip-permissions`, `<bin> --prompt-interactive ` + interactiveReviewPrompt + ` --dangerously-skip-permissions`},
}

// binMark stands in for the binary while an old template is rendered, so the
// stored command's own binary can be read back from between the rendered text
// around it.
const binMark = "\x00gg-bin\x00"

// UpgradeReviewCommand recognises a stored command as a rendering of an earlier
// built-in review template (GenerateCommandFor, on either OS) and returns the
// current template rendered the same way with the same binary. A command the
// user edited — anything but the old rendering byte for byte, line ends and
// surrounding blank lines aside — is not recognised.
func UpgradeReviewCommand(stored string) (string, bool) {
	stored = strings.TrimSpace(strings.ReplaceAll(stored, "\r\n", "\n"))
	for _, goos := range []string{"linux", "windows"} {
		for _, p := range SupersededReviewCommands {
			gen := strings.TrimSpace(GenerateCommandFor(CommandTemplate{Command: p.Old}, binMark, goos))
			i := strings.Index(gen, binMark)
			if i < 0 {
				continue
			}
			prefix, suffix := gen[:i], gen[i+len(binMark):]
			if len(stored) <= len(prefix)+len(suffix) || !strings.HasPrefix(stored, prefix) || !strings.HasSuffix(stored, suffix) {
				continue
			}
			bin := stored[len(prefix) : len(stored)-len(suffix)]
			if q := len(bin) >= 2 && bin[0] == '"' && bin[len(bin)-1] == '"'; q {
				bin = bin[1 : len(bin)-1]
				if strings.Contains(bin, `"`) {
					continue
				}
			} else if strings.ContainsAny(bin, " \t\n\"") {
				continue
			}
			return GenerateCommandFor(CommandTemplate{Command: p.New}, bin, goos), true
		}
	}
	return "", false
}
