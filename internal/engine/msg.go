package engine

import "fmt"

// Msg is one localizable sentence fragment: an English format string
// (doubling as the i18n catalog key) plus its interpolation args. The zero
// Msg means "no localizable channel — render the English string". Formats
// are always untranslated English literals at call sites (an AST gate in
// internal/tui enforces this); args are data, never re-formatted, and may
// contain '%'.
type Msg struct {
	Format string
	Args   []any
}

// text renders the English form. Mirrors i18n.T's contract: with no args
// the format is returned verbatim (no Sprintf pass), so a literal '%' in
// an arg-less format is safe in both channels.
func (m Msg) text() string {
	if len(m.Args) == 0 {
		return m.Format
	}
	return fmt.Sprintf(m.Format, m.Args...)
}

// WithSummary sets both summary channels from one format. Value method,
// chainable off a composite literal:
//
//	res := Result{Changed: true}.WithSummary("created branch %s", op.Name)
func (r Result) WithSummary(format string, args ...any) Result {
	m := Msg{Format: format, Args: args}
	r.Summary = m.text()
	r.SummaryParts = []Msg{m}
	return r
}

// AppendSummary appends a suffix part to both channels; the glue (leading
// "; " or " (") lives inside the suffix format. On a receiver whose
// Summary was hand-built (non-empty, no parts) only the English string is
// extended and the parts stay empty, so the whole summary falls back to
// English at render rather than mixing channels.
func (r Result) AppendSummary(format string, args ...any) Result {
	m := Msg{Format: format, Args: args}
	if r.Summary == "" || len(r.SummaryParts) > 0 {
		r.SummaryParts = append(r.SummaryParts, m)
	}
	r.Summary += m.text()
	return r
}

// Progressf builds a glue-bearing Progress event — a Detail whose format
// mixes English words into the data. Pure-data details keep the plain
// Progress composite literal.
func Progressf(step, format string, args ...any) Progress {
	m := Msg{Format: format, Args: args}
	return Progress{Step: step, Detail: m.text(), DetailMsg: m}
}

// PromptReq builds a DecisionRequest with both prompt channels in
// lockstep. Options values stay English protocol (frontends translate
// labels at render).
func PromptReq(id, format string, options []string, args ...any) DecisionRequest {
	m := Msg{Format: format, Args: args}
	return DecisionRequest{ID: id, Prompt: m.text(), PromptMsg: m, Options: options}
}
