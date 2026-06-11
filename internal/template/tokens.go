// Package template resolves gigagit's worktree naming templates. It is pure:
// it performs no I/O and draws all time/randomness from an injected Ctx, so
// resolution is deterministic and fully unit-testable.
package template

import "regexp"

// tokenRe matches a single <...> token, capturing the inside (no '>' allowed,
// so tokens never span). Used by every parsing/resolution function.
var tokenRe = regexp.MustCompile(`<([^>]+)>`)

// UserLabels returns the distinct <user:LABEL> labels in order of first
// appearance, so a frontend knows which input fields to render.
func UserLabels(tmpl string) []string {
	return distinctTokenArgs(tmpl, "user")
}

// SeqNames returns the distinct <seq:NAME> counter names in order of first
// appearance, so the create flow knows which counters to peek and bump.
func SeqNames(tmpl string) []string {
	return distinctTokenArgs(tmpl, "seq")
}

// distinctTokenArgs scans tmpl for tokens of the form <prefix:ARG...> and
// returns the first colon-separated segment after the prefix (for seq this is
// the NAME, ignoring any :N padding; for user it is the LABEL), distinct and
// ordered.
func distinctTokenArgs(tmpl, prefix string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range tokenRe.FindAllStringSubmatch(tmpl, -1) {
		body := m[1]
		p, rest, ok := cutColon(body)
		if !ok || p != prefix {
			continue
		}
		arg, _, _ := cutColon(rest) // for "user" rest has no further colon; for seq, drop :N
		if arg == "" {
			arg = rest
		}
		if !seen[arg] {
			seen[arg] = true
			out = append(out, arg)
		}
	}
	return out
}

// cutColon splits s on the first ':' into (before, after, found).
func cutColon(s string) (string, string, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			return s[:i], s[i+1:], true
		}
	}
	return s, "", false
}
