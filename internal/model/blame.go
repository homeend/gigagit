package model

// BlameLine is one source line annotated with the commit that last changed it.
// Hash "" means the line is not yet committed (a working-tree change); git
// reports such lines under the all-zero sha, which the parser normalises to "".
type BlameLine struct {
	Hash    string // full commit sha; "" for not-yet-committed
	Author  string // author name
	Time    int64  // author-time, unix epoch seconds
	Summary string // commit subject (first line)
	LineNo  int    // final line number, 1-based
	Content string // the source line text (no trailing newline)
}
