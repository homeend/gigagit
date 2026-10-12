package notemeta

// Badge is one chip: a key's value in its colour, glyph and style.
type Badge struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Color string `json:"color,omitempty"`
	Glyph string `json:"glyph,omitempty"`
	Style string `json:"style,omitempty"`
}

// Line is one "label: value" row under a note's title.
type Line struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Change is an added value shown over the author's (From "" when the
// author wrote no value for the key).
type Change struct {
	Key  string `json:"key"`
	From string `json:"from,omitempty"`
	To   string `json:"to"`
	By   string `json:"by,omitempty"`
}

// Counted is one key/value a note contributes to its review's counts.
type Counted struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Value string `json:"value"`
	Color string `json:"color,omitempty"`
}

// Resolved is what to show for one note — the one input every renderer
// (TUI, web, CLI text, forge adapters) reads.
type Resolved struct {
	// Values is every effective key/value (the author's, then the added
	// ones), shown or not: what a text listing prints.
	Values   []KV      `json:"values,omitempty"`
	Badges   []Badge   `json:"badges,omitempty"`
	Frame    string    `json:"frame,omitempty"`
	Lines    []Line    `json:"lines,omitempty"`
	Counted  []Counted `json:"counted,omitempty"`
	Level    string    `json:"level,omitempty"`
	Audience string    `json:"audience,omitempty"`
	Stored   []KV      `json:"stored,omitempty"`
	Added    []Change  `json:"added,omitempty"`
	Problems []Problem `json:"problems,omitempty"`
}

// Empty reports a note with nothing to show and nothing to report.
func (r Resolved) Empty() bool {
	return len(r.Values) == 0 && len(r.Badges) == 0 && r.Frame == "" && len(r.Lines) == 0 && len(r.Stored) == 0 &&
		len(r.Added) == 0 && len(r.Problems) == 0 && r.Level == "" && r.Audience == ""
}

// Resolve evaluates a note: its author's meta, its effective declaration and
// the meta added later (spec §3, §2.4). The declaration is re-validated here,
// so a bad entry is a problem for as long as it stands.
func Resolve(meta []KV, d Display, added []Added) Resolved {
	var out Resolved
	vals, order, changes := overlay(meta, added)
	out.Added = changes
	for _, k := range order {
		out.Values = append(out.Values, KV{k, vals[k]})
	}
	decl, probs := validate(d)
	out.Problems = probs
	if len(decl) == 0 {
		// No declaration at all: every key is a line (what tags looked like).
		for _, k := range order {
			if k == StatusKey {
				continue
			}
			out.Lines = append(out.Lines, Line{k, vals[k]})
		}
		decl = Display{}
	}
	if _, ok := decl[StatusKey]; !ok {
		decl[StatusKey] = StatusDisplay
	}
	declared := map[string]bool{}
	directLevel, directAudience := false, false
	for _, k := range sortedKeys(decl) {
		declared[k] = true
		v, ok := vals[k]
		if !ok {
			continue
		}
		key := decl[k]
		spec := key.Values[v]
		if _, listed := key.Values[v]; !listed && key.Other != nil {
			spec = *key.Other
		}
		label := key.Label
		if label == "" {
			label = k
		}
		for _, p := range key.Show {
			switch p {
			case PlaceBadge:
				c := spec.Color
				if c == "" {
					c = "grey"
				}
				st := key.Style
				if st == "" {
					st = StyleFilled
				}
				out.Badges = append(out.Badges, Badge{Key: k, Value: v, Color: c, Glyph: spec.Glyph, Style: st})
			case PlaceFrame:
				if out.Frame == "" && spec.Color != "" {
					out.Frame = spec.Color
				}
			case PlaceCount:
				out.Counted = append(out.Counted, Counted{Key: k, Label: label, Value: v, Color: spec.Color})
			case PlaceLine:
				out.Lines = append(out.Lines, Line{label, v})
			}
		}
		switch key.As {
		case "level":
			if oneOf(v, Levels) {
				out.Level, directLevel = v, true
			} else {
				out.Problems = append(out.Problems, Problem("meta "+k+": "+quote(v)+" is not a level"))
			}
		case "audience":
			if oneOf(v, Audiences) {
				out.Audience, directAudience = v, true
			} else {
				out.Problems = append(out.Problems, Problem("meta "+k+": "+quote(v)+" is not an audience"))
			}
		}
		if spec.Level != "" && !directLevel && out.Level == "" {
			out.Level = spec.Level
		}
		if spec.Audience != "" && !directAudience && out.Audience == "" {
			out.Audience = spec.Audience
		}
	}
	if len(d) > 0 {
		for _, k := range order {
			if !declared[k] {
				out.Stored = append(out.Stored, KV{k, vals[k]})
			}
		}
	}
	return out
}

// overlay applies the added entries over the author's meta: the values, the
// key order (author's first, then new keys in added order) and the changes.
func overlay(meta []KV, added []Added) (map[string]string, []string, []Change) {
	vals := map[string]string{}
	var order []string
	for _, kv := range meta {
		if _, dup := vals[kv.Key]; dup {
			continue
		}
		vals[kv.Key] = kv.Value
		order = append(order, kv.Key)
	}
	var changes []Change
	for _, a := range added {
		from, had := vals[a.Key]
		if had && from == a.Value {
			continue
		}
		if !had {
			order = append(order, a.Key)
		}
		vals[a.Key] = a.Value
		changes = append(changes, Change{Key: a.Key, From: from, To: a.Value, By: a.By})
	}
	return vals, order, changes
}

func quote(s string) string { return "\"" + s + "\"" }

// CountValue is one value's tally within a key.
type CountValue struct {
	Value string `json:"value"`
	Color string `json:"color,omitempty"`
	N     int    `json:"n"`
}

// CountGroup is one counted key's values, in first-seen order.
type CountGroup struct {
	Key    string       `json:"key"`
	Label  string       `json:"label"`
	Values []CountValue `json:"values"`
}

// Counts is a set of notes' tallies: per counted key, plus how many notes
// are for the author and how many for the reviewer.
type Counts struct {
	Groups   []CountGroup `json:"groups,omitempty"`
	Author   int          `json:"author,omitempty"`
	Reviewer int          `json:"reviewer,omitempty"`
}

// Count tallies resolved notes (a review's remarks, a file's notes).
func Count(rs []Resolved) Counts {
	var c Counts
	at := map[string]int{}
	for _, r := range rs {
		switch r.Audience {
		case "author":
			c.Author++
		case "reviewer":
			c.Reviewer++
		}
		for _, x := range r.Counted {
			gi, ok := at[x.Key]
			if !ok {
				gi = len(c.Groups)
				at[x.Key] = gi
				c.Groups = append(c.Groups, CountGroup{Key: x.Key, Label: x.Label})
			}
			g := &c.Groups[gi]
			found := false
			for i := range g.Values {
				if g.Values[i].Value == x.Value {
					g.Values[i].N++
					found = true
					break
				}
			}
			if !found {
				g.Values = append(g.Values, CountValue{Value: x.Value, Color: x.Color, N: 1})
			}
		}
	}
	return c
}
