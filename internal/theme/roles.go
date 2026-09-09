package theme

import "strconv"

// syntaxClassNames is the long name of every syntax class, in internal/syntax's
// Class order. theme is a DAG leaf (it cannot import syntax), so this is a
// deliberate MIRROR: internal/tui's TestThemeSyntaxRoleNamesMatchSyntaxClasses
// pins it against the real Class constants, and reordering either side fails
// there. The names are the ones the syntax role's doc line already spells out
// and the ones the colour editor shows as syntax[<Name>].
var syntaxClassNames = [11]string{
	"Plain", "Keyword", "Type", "Func", "Name",
	"String", "Number", "Comment", "Operator", "Punct", "Attr",
}

// SyntaxClassNames returns the syntax class names in Class order — the mirror
// the TUI's order gate compares against internal/syntax.
func SyntaxClassNames() [11]string { return syntaxClassNames }

// syntaxEntryDocs is the per-ENTRY description a syntax class needs: RoleDocs
// documents `lanes` and `syntax` as whole config keys, but the colour editor
// edits one entry at a time and shows a doc per row. (Lane docs are generated
// from the index in Roles.)
var syntaxEntryDocs = [11]string{
	"unclassified code (uncoloured by design)",
	"keywords in diff / blame / preview",
	"type names in diff / blame / preview",
	"function names in diff / blame / preview",
	"identifiers (uncoloured by design)",
	"string literals in diff / blame / preview",
	"number literals in diff / blame / preview",
	"comments in diff / blame / preview",
	"operators in diff / blame / preview",
	"punctuation in diff / blame / preview",
	"attributes and annotations in diff / blame / preview",
}

// RoleRef addresses ONE editable colour of a theme: a scalar role ("bg"), or a
// single entry of a list role ("lanes[3]", "syntax[Keyword]"). It is the unit
// the Settings colour editor lists, previews and writes — one RoleRef is one
// row and one `key = …` line in [themes.<name>].
//
// Key is the row's identity as shown to the user. ListKey is "" for a scalar
// and "lanes"/"syntax" for a list entry, whose position is Index (-1 for a
// scalar). IsBg says the colour paints a BACKGROUND, so a renderer samples it
// as a filled cell rather than as a coloured glyph.
type RoleRef struct {
	Key     string
	ListKey string
	Doc     string
	Index   int
	IsBg    bool
}

// Roles lists every editable role in editor order: the scalar roleFields (the
// order the config block and RoleDocs already use), then the seven graph lanes,
// then the eleven syntax classes. Plain and Name are included even though they
// are uncoloured by design — they hold "" and the editor shows that as the
// inherit marker, which is exactly the state a user may want to change.
func Roles() []RoleRef {
	out := make([]RoleRef, 0, len(roleFields)+len(Dark.Lanes)+len(Dark.Syntax))
	for _, f := range roleFields {
		out = append(out, RoleRef{Key: f.key, Doc: f.doc, Index: -1, IsBg: isBgKey(f.key)})
	}
	for i := range Dark.Lanes {
		out = append(out, RoleRef{
			Key:     "lanes[" + strconv.Itoa(i) + "]",
			ListKey: "lanes",
			Doc:     "graph lane " + strconv.Itoa(i),
			Index:   i,
		})
	}
	for i := range Dark.Syntax {
		out = append(out, RoleRef{
			Key:     "syntax[" + syntaxClassNames[i] + "]",
			ListKey: "syntax",
			Doc:     syntaxEntryDocs[i],
			Index:   i,
		})
	}
	return out
}

// isBgKey reports whether a scalar role paints a background: the key is "bg" or
// ends in "_bg". The naming is the contract — a background role that broke it
// would sample as a glyph colour and read wrong in the editor.
func isBgKey(key string) bool {
	return key == "bg" || len(key) > 3 && key[len(key)-3:] == "_bg"
}

// Get returns the role's value in th ("" = the role is unset / inherits).
func (r RoleRef) Get(th Theme) string {
	switch r.ListKey {
	case "lanes":
		return th.Lanes[r.Index]
	case "syntax":
		return th.Syntax[r.Index]
	}
	for _, f := range roleFields {
		if f.key == r.Key {
			return *f.get(&th)
		}
	}
	return ""
}

// Set returns th with the role set to v. No validation: callers check with
// ValidColour first (an empty v is a legitimate "inherit" value here).
func (r RoleRef) Set(th Theme, v string) Theme {
	out := th
	switch r.ListKey {
	case "lanes":
		out.Lanes[r.Index] = v
		return out
	case "syntax":
		out.Syntax[r.Index] = v
		return out
	}
	for _, f := range roleFields {
		if f.key == r.Key {
			*f.get(&out) = v
			return out
		}
	}
	return out
}

// OverrideGet returns what o sets for this role ("" = o leaves it alone). A
// list role reads "" while o's whole list is nil: the list is set as a unit.
func (r RoleRef) OverrideGet(o Override) string {
	switch r.ListKey {
	case "lanes":
		return listEntry(o.Lanes, r.Index)
	case "syntax":
		return listEntry(o.Syntax, r.Index)
	}
	return o.Value(r.Key)
}

func listEntry(list []string, i int) string {
	if i < 0 || i >= len(list) {
		return ""
	}
	return list[i]
}

// OverrideSet returns o with this role set to v (v "" clears it). Because a
// lanes/syntax list is written as ONE config line, setting an entry of a list o
// does not carry yet expands it from base's full palette first — the resulting
// line is complete, and the entries the user did not touch keep painting what
// they painted.
func (r RoleRef) OverrideSet(o Override, base Theme, v string) Override {
	out := o
	switch r.ListKey {
	case "lanes":
		out.Lanes = setListEntry(o.Lanes, base.Lanes[:], r.Index, v)
		return out
	case "syntax":
		out.Syntax = setListEntry(o.Syntax, base.Syntax[:], r.Index, v)
		return out
	}
	for _, f := range roleFields {
		if f.key == r.Key {
			*f.getO(&out) = v
			return out
		}
	}
	return out
}

// setListEntry returns a copy of list with index i set to v, materialising it
// from base when list is nil so the written array is always full length.
func setListEntry(list, base []string, i int, v string) []string {
	out := make([]string, len(base))
	if len(list) == len(base) {
		copy(out, list)
	} else {
		copy(out, base)
	}
	if i >= 0 && i < len(out) {
		out[i] = v
	}
	return out
}
