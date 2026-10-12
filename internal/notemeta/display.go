package notemeta

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Display is an author's declaration: per meta key, how it is shown.
type Display map[string]Key

// Key declares one meta key. Show lists its places; Values (and Other, for a
// value not listed) give each value's presentation and semantics; As maps
// the key's VALUE straight onto a semantic ("level" or "audience").
type Key struct {
	Show   []string         `toml:"show,omitempty" json:"show,omitempty"`
	Label  string           `toml:"label,omitempty" json:"label,omitempty"`
	Order  int              `toml:"order,omitempty" json:"order,omitempty"`
	Style  string           `toml:"style,omitempty" json:"style,omitempty"`
	As     string           `toml:"as,omitempty" json:"as,omitempty"`
	Values map[string]Value `toml:"values,omitempty" json:"values,omitempty"`
	Other  *Value           `toml:"other,omitempty" json:"other,omitempty"`
}

// Value is one value's presentation (Color, Glyph) and semantics (Level,
// Audience).
type Value struct {
	Color    string `toml:"color,omitempty" json:"color,omitempty"`
	Glyph    string `toml:"glyph,omitempty" json:"glyph,omitempty"`
	Level    string `toml:"level,omitempty" json:"level,omitempty"`
	Audience string `toml:"audience,omitempty" json:"audience,omitempty"`
}

// ParseDisplay reads a declaration. Absent, blank or null is none (nil). JSON
// that is not a declaration's shape is an error — the command that carried
// it fails. An unknown field is not: it is named as a problem and the rest is
// kept (only here — once decoded, the field is gone).
func ParseDisplay(raw []byte) (Display, []Problem, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil, nil
	}
	var d Display
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	err := dec.Decode(&d)
	if err == nil {
		return d, nil, nil
	}
	if !strings.Contains(err.Error(), "unknown field") {
		return nil, nil, fmt.Errorf("display: %w", err)
	}
	d = nil
	if lerr := json.Unmarshal(raw, &d); lerr != nil {
		return nil, nil, fmt.Errorf("display: %w", lerr)
	}
	return d, []Problem{Problem("display: " + strings.TrimPrefix(err.Error(), "json: "))}, nil
}

// sortedKeys is d's keys by Order, then name: the one display order.
func sortedKeys(d Display) []string {
	keys := make([]string, 0, len(d))
	for k := range d {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := d[keys[i]], d[keys[j]]
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		return keys[i] < keys[j]
	})
	return keys
}

// validate drops what gg cannot show and names it: an unknown place, style,
// `as`, colour, level or audience; a glyph outside the safe set; keys past
// MaxDisplayKeys (in display order) and values past MaxDisplayValues (by
// name). The result shares nothing with d.
func validate(d Display) (Display, []Problem) {
	if d == nil {
		return nil, nil
	}
	out := Display{}
	var probs []Problem
	for i, k := range sortedKeys(d) {
		if i >= MaxDisplayKeys {
			probs = append(probs, Problem(fmt.Sprintf("display.%s: a declaration holds at most %d keys", k, MaxDisplayKeys)))
			continue
		}
		in := d[k]
		at := "display." + k
		key := Key{Label: in.Label, Order: in.Order}
		for _, p := range in.Show {
			if oneOf(p, places) {
				key.Show = append(key.Show, p)
				continue
			}
			probs = append(probs, Problem(fmt.Sprintf("%s.show: %q is not a place (badge, frame, count, line)", at, p)))
		}
		switch {
		case in.Style == "" || in.Style == StyleFilled || in.Style == StyleText:
			key.Style = in.Style
		default:
			probs = append(probs, Problem(fmt.Sprintf("%s.style: %q is not a style (filled, text)", at, in.Style)))
		}
		switch {
		case in.As == "" || oneOf(in.As, semantics):
			key.As = in.As
		default:
			probs = append(probs, Problem(fmt.Sprintf("%s.as: %q is not a semantic (level, audience)", at, in.As)))
		}
		names := make([]string, 0, len(in.Values))
		for v := range in.Values {
			names = append(names, v)
		}
		sort.Strings(names)
		for j, v := range names {
			if j >= MaxDisplayValues {
				probs = append(probs, Problem(fmt.Sprintf("%s.values.%s: a key declares at most %d values", at, v, MaxDisplayValues)))
				continue
			}
			if key.Values == nil {
				key.Values = map[string]Value{}
			}
			cv, ps := checkValue(in.Values[v], at+".values."+v)
			key.Values[v] = cv
			probs = append(probs, ps...)
		}
		if in.Other != nil {
			cv, ps := checkValue(*in.Other, at+".other")
			key.Other = &cv
			probs = append(probs, ps...)
		}
		out[k] = key
	}
	return out, probs
}

func checkValue(in Value, at string) (Value, []Problem) {
	var out Value
	var probs []Problem
	if in.Color != "" {
		if oneOf(in.Color, Colors) {
			out.Color = in.Color
		} else {
			probs = append(probs, Problem(fmt.Sprintf("%s.color: %q is not a colour (%s)", at, in.Color, strings.Join(Colors, ", "))))
		}
	}
	if in.Glyph != "" {
		if GlyphOK(in.Glyph) {
			out.Glyph = in.Glyph
		} else {
			probs = append(probs, Problem(fmt.Sprintf("%s.glyph: %q is not one safe character", at, in.Glyph)))
		}
	}
	if in.Level != "" {
		if oneOf(in.Level, Levels) {
			out.Level = in.Level
		} else {
			probs = append(probs, Problem(fmt.Sprintf("%s.level: %q is not a level (%s)", at, in.Level, strings.Join(Levels, ", "))))
		}
	}
	if in.Audience != "" {
		if oneOf(in.Audience, Audiences) {
			out.Audience = in.Audience
		} else {
			probs = append(probs, Problem(fmt.Sprintf("%s.audience: %q is not an audience (author, reviewer)", at, in.Audience)))
		}
	}
	return out, probs
}

// Effective builds a note's declaration from its levels, farthest first
// (document, file, remark — or root, reply): key by key, the nearest entry
// REPLACES the farther one wholesale. nil when no level declares anything.
func Effective(levels ...Display) Display {
	var out Display
	for _, l := range levels {
		for k, v := range l {
			if out == nil {
				out = Display{}
			}
			out[k] = v
		}
	}
	return out
}

// StatusKey is the one key gg declares itself: the Mark use case's tag
// (agreed / disagreed / deferred), used when the effective declaration has
// no entry for it.
const StatusKey = "status"

// StatusDisplay is that built-in declaration.
var StatusDisplay = Key{Show: []string{PlaceBadge}, Order: 100, Style: StyleText,
	Values: map[string]Value{"agreed": {Color: "green"}, "disagreed": {Color: "red"}, "deferred": {Color: "grey"}}}
