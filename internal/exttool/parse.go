package exttool

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

// ErrEmptyMessage means the tool produced no usable message and no explicit error.
var ErrEmptyMessage = errors.New("exttool: empty commit message from tool")

type captureEnvelope struct {
	IsError          bool    `json:"is_error"`
	Result           *string `json:"result"`
	Subject          string  `json:"subject"` // defensive: some tools may emit top-level subject/body
	Body             string  `json:"body"`
	StructuredOutput *struct {
		Subject string `json:"subject"`
		Body    string `json:"body"`
	} `json:"structured_output"`
}

// ParseCaptureMessage interprets an agent's captured output as a commit message
// (subject + body), format-agnostic across the shapes gg's catalog tools emit:
// Claude plain text; Claude --output-format json (.result); Claude --json-schema
// (.structured_output); the raw message text a task-agent writes to
// $GG_MESSAGE_FILE (e.g. Junie — the engine reads that file and prefers it over
// stdout, so what reaches here is a clean message, not Junie's stdout report);
// and non-JSON fallbacks. A tool-reported error (is_error) is returned as err.
func ParseCaptureMessage(stdout []byte) (subject, body string, err error) {
	t := bytes.TrimSpace(stdout)
	if len(t) > 0 && t[0] == '{' {
		var env captureEnvelope
		if json.Unmarshal(t, &env) == nil {
			switch {
			case env.IsError:
				msg := "tool reported an error"
				if env.Result != nil {
					if trimmed := strings.TrimSpace(*env.Result); trimmed != "" {
						msg = trimmed
					}
				}
				return "", "", errors.New(msg)
			case env.StructuredOutput != nil && strings.TrimSpace(env.StructuredOutput.Subject) != "":
				return strings.TrimSpace(env.StructuredOutput.Subject),
					strings.TrimSpace(env.StructuredOutput.Body), nil
			case strings.TrimSpace(env.Subject) != "":
				return strings.TrimSpace(env.Subject), strings.TrimSpace(env.Body), nil
			case env.Result != nil:
				text := stripCodeFence(*env.Result)
				subject, body = SplitMessage(text)
				if subject == "" {
					return "", "", ErrEmptyMessage
				}
				return subject, body, nil
			}
		}
		// JSON-looking but unrecognized (no matching case above, e.g. no "result"
		// key at all) or malformed JSON that failed to unmarshal: fall through to
		// raw-text handling and treat the input verbatim as the message.
	}
	subject, body = SplitMessage(stripCodeFence(string(t)))
	if subject == "" {
		return "", "", ErrEmptyMessage
	}
	return subject, body, nil
}

// stripCodeFence removes a wrapping markdown code fence from agent output.
// Agents frequently wrap a commit message in ``` … ``` (sometimes with a
// language tag, e.g. ```text) even when told not to, which otherwise makes the
// opening fence the commit subject. It fires ONLY when the first non-blank line
// opens a fence, so a message that merely contains ``` in its body is left
// untouched; a matching trailing bare ``` line is dropped as well.
func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	lines := strings.Split(s, "\n")
	lines = lines[1:] // drop the opening ``` / ```lang line
	if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "```" {
		lines = lines[:len(lines)-1] // drop the closing ``` line
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// SplitMessage splits a commit message into (subject, body): the first line is
// the subject, the rest (after leading blank lines) the body. The one canonical
// split rule (the TUI amend pre-fill delegates here).
func SplitMessage(msg string) (subject, body string) {
	msg = strings.TrimRight(msg, "\n")
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		return msg[:i], strings.TrimLeft(msg[i+1:], "\n")
	}
	return msg, ""
}
