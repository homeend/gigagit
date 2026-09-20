// Package markdown parses the GitHub-flavoured subset of markdown that forge
// text (a pull request's description, comments and review threads) is written
// in, into a small JSON-serialisable block tree.
//
// There is ONE parser, and it is this one: the web paints the tree it is sent
// (static/markdown.js never parses), and the TUI lays the same tree out in
// rows. The JSON tags below are therefore a wire contract.
//
// Parse is total — it never fails and never panics — and bounded: oversized
// input, deep nesting and very wide tables degrade to plain paragraphs. Raw
// HTML is never interpreted; it is ordinary text. A link or image destination
// survives only when it is http(s) (see safeURL), so no consumer ever holds
// another scheme in Inline.URL.
//
// Pure: stdlib + internal/syntax (fenced code colouring). No git, domain, TUI
// or web imports.
package markdown

// Block kinds.
const (
	KindPara    = "p"
	KindHeading = "h"
	KindList    = "list"
	KindQuote   = "quote"
	KindCode    = "code"
	KindTable   = "table"
	KindRule    = "hr"
)

// Inline kinds.
const (
	InText   = "text"
	InStrong = "strong"
	InEm     = "em"
	InDel    = "del"
	InCode   = "code"
	InLink   = "link"
	InImage  = "image"
	InRef    = "ref" // an @mention or a #123 reference: styled, never an action
	InBreak  = "br"
)

// Task states of a list item.
const (
	TaskOpen = "open"
	TaskDone = "done"
)

// The bounds past which Parse degrades rather than recursing or allocating
// without limit.
const (
	MaxInput     = 256 << 10 // bytes; larger input is split into paragraphs only
	MaxDepth     = 8         // container (quote/list) and inline nesting
	MaxTableCols = 64
	MaxLexLines  = 2000 // a fenced block longer than this is not coloured
)

// Doc is one parsed text.
type Doc struct {
	Blocks []Block `json:"blocks"`
}

// Block is one block node; which fields are set depends on Kind.
type Block struct {
	Kind    string       `json:"k"`
	Level   int          `json:"level,omitempty"`   // h: 1–6
	Inline  []Inline     `json:"in,omitempty"`      // p, h
	Ordered bool         `json:"ordered,omitempty"` // list
	Start   int          `json:"start,omitempty"`   // list: the first number
	Items   []Item       `json:"items,omitempty"`   // list
	Blocks  []Block      `json:"blocks,omitempty"`  // quote
	Lang    string       `json:"lang,omitempty"`    // code: the info string's first word
	Lines   []CodeLine   `json:"lines,omitempty"`   // code
	Head    [][]Inline   `json:"head,omitempty"`    // table
	Rows    [][][]Inline `json:"rows,omitempty"`    // table
	Align   []string     `json:"align,omitempty"`   // table: "" | left | center | right
}

// Item is one list item.
type Item struct {
	Task   string  `json:"task,omitempty"` // "" | open | done
	Blocks []Block `json:"blocks"`
}

// CodeLine is one line of a fenced block, with its token runs when the
// block's language is known.
type CodeLine struct {
	Text string    `json:"t"`
	Toks []WireTok `json:"toks,omitempty"`
}

// WireTok is one coloured run: rune offsets [Start, End) and the class suffix
// syntax.Class.String() hands out ("kw", "str", …).
type WireTok struct {
	Start int    `json:"s"`
	End   int    `json:"e"`
	Class string `json:"c"`
}

// Inline is one inline node.
type Inline struct {
	Kind string   `json:"k"`
	Text string   `json:"t,omitempty"`   // text, code, ref; image: the alt text
	URL  string   `json:"url,omitempty"` // link, image — only ever http(s)
	In   []Inline `json:"in,omitempty"`  // strong, em, del, link
}
