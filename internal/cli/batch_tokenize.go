package cli

import "fmt"

// tokenizeBatchLine splits one batch-script line into argv tokens. Single
// and double quotes group words (and may appear mid-token, joining with the
// surrounding characters); there are no escapes, pipes, env expansion,
// globs, or continuations — a batch script is not a shell. Empty input
// yields nil.
func tokenizeBatchLine(line string) ([]string, error) {
	var (
		tokens []string
		cur    []rune
		inTok  bool
		quote  rune // 0 = unquoted
	)
	for _, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur = append(cur, r)
			}
		case r == '\'' || r == '"':
			quote = r
			inTok = true // a quote opens a token even if it stays empty
		case r == ' ' || r == '\t':
			if inTok {
				tokens = append(tokens, string(cur))
				cur, inTok = nil, false
			}
		default:
			cur = append(cur, r)
			inTok = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated %c quote", quote)
	}
	if inTok {
		tokens = append(tokens, string(cur))
	}
	return tokens, nil
}
