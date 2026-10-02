package template

import (
	"fmt"
	"regexp"
)

// textTokenRe is tokenRe for prose: a token never spans a line and never
// contains a '<', so a bare '<' ("a < b") cannot swallow the token after it.
var textTokenRe = regexp.MustCompile(`<([^<>\n]+)>`)

// textTokenKinds are the token prefixes ResolveText resolves; any other <…>
// is literal text (HTML tags, <me@example.com>).
var textTokenKinds = map[string]bool{
	"parent-branch": true, "repo": true, "branch": true, "date": true,
	"seq": true, "user": true, "random-alpha": true, "random-num": true,
}

// ResolveText is Resolve for a text template (a multi-line body). It differs
// in three ways: a <…> whose kind is not a token stays in the output as
// written; <branch> is Ctx.Branch verbatim (an unset branch is ""); a token
// never spans lines. A KNOWN token with malformed arguments is still an
// error — as are <branch:…> (it takes no argument) and <user:> (no label).
// Substituted values are never scanned again.
func ResolveText(tmpl string, inputs map[string]string, ctx Ctx) (string, error) {
	var firstErr error
	out := textTokenRe.ReplaceAllStringFunc(tmpl, func(tok string) string {
		body := tok[1 : len(tok)-1]
		kind, rest, hasColon := cutColon(body)
		if !textTokenKinds[kind] {
			return tok
		}
		fail := func(err error) string {
			if firstErr == nil {
				firstErr = err
			}
			return ""
		}
		switch {
		case body == "branch":
			return ctx.Branch
		case kind == "branch":
			// Not left to resolveToken: its <branch> is the path-template one.
			return fail(fmt.Errorf("template: <branch> takes no argument"))
		case kind == "user" && hasColon && rest == "":
			return fail(fmt.Errorf("template: <user:> requires a label, e.g. <user:issue-id>"))
		}
		val, err := resolveToken(body, inputs, ctx)
		if err != nil {
			return fail(err)
		}
		return val
	})
	if firstErr != nil {
		return "", firstErr
	}
	return out, nil
}

// TextTokenSet is what a text template asks for: the <user:…> labels to
// prompt for, the <seq:…> counters it consumes, and the other tokens it
// uses, each as written (for the "Automatic" line). All distinct, in order
// of first appearance.
type TextTokenSet struct {
	UserLabels []string
	SeqNames   []string
	Automatic  []string
}

// TextTokens scans a text template with ResolveText's token rules.
func TextTokens(tmpl string) TextTokenSet {
	var set TextTokenSet
	seen := map[string]bool{}
	add := func(list *[]string, key, v string) {
		if !seen[key] {
			seen[key] = true
			*list = append(*list, v)
		}
	}
	for _, m := range textTokenRe.FindAllStringSubmatch(tmpl, -1) {
		kind, rest, _ := cutColon(m[1])
		if !textTokenKinds[kind] {
			continue
		}
		switch kind {
		case "user":
			add(&set.UserLabels, "u:"+rest, rest)
		case "seq":
			name, _, _ := cutColon(rest)
			add(&set.SeqNames, "s:"+name, name)
			add(&set.Automatic, "a:"+m[0], m[0])
		default:
			add(&set.Automatic, "a:"+m[0], m[0])
		}
	}
	return set
}
