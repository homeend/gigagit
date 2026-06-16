package hunkpick

import (
	"errors"
	"strings"
)

// conflict-marker prefixes (git writes exactly seven characters).
const (
	markStart = "<<<<<<<"
	markBase  = "|||||||"
	markSep   = "======="
	markEnd   = ">>>>>>>"
)

// ParseConflict splits a conflicted working-tree file into ordered Items:
// passthrough text becomes Literal items; each <<<<<<< / ======= / >>>>>>>
// region becomes a Block{Current, Incoming}. diff3 ||||||| base lines are
// skipped. Unbalanced or out-of-order markers return an error.
func ParseConflict(content []byte) (*Doc, error) {
	final := len(content) > 0 && content[len(content)-1] == '\n'
	text := string(content)
	if final {
		text = text[:len(text)-1]
	}
	var lines []string
	if text != "" {
		lines = strings.Split(text, "\n")
	}

	d := &Doc{FinalNewline: final}
	var lit []string
	flushLit := func() {
		if len(lit) > 0 {
			d.Items = append(d.Items, Item{Literal: lit})
			lit = nil
		}
	}

	const (
		stOut = iota
		stCurrent
		stBase
		stIncoming
	)
	state := stOut
	var cur, inc []string
	for _, ln := range lines {
		switch {
		case strings.HasPrefix(ln, markStart):
			if state != stOut {
				return nil, errors.New("hunkpick: nested <<<<<<< marker")
			}
			flushLit()
			state, cur, inc = stCurrent, nil, nil
		case strings.HasPrefix(ln, markBase) && state == stCurrent:
			state = stBase
		case strings.HasPrefix(ln, markSep):
			if state != stCurrent && state != stBase {
				return nil, errors.New("hunkpick: ======= without <<<<<<<")
			}
			state = stIncoming
		case strings.HasPrefix(ln, markEnd):
			if state != stIncoming {
				return nil, errors.New("hunkpick: >>>>>>> without =======")
			}
			d.Items = append(d.Items, Item{Block: &Block{Current: cur, Incoming: inc}})
			state = stOut
		default:
			switch state {
			case stOut:
				lit = append(lit, ln)
			case stCurrent:
				cur = append(cur, ln)
			case stIncoming:
				inc = append(inc, ln)
				// stBase lines are intentionally dropped.
			}
		}
	}
	if state != stOut {
		return nil, errors.New("hunkpick: unterminated conflict region")
	}
	flushLit()
	return d, nil
}
