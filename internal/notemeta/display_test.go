package notemeta

import (
	"strings"
	"testing"
)

func TestParseDisplayEmptyIsNone(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"", "  ", "null"} {
		d, probs, err := ParseDisplay([]byte(raw))
		if err != nil || d != nil || probs != nil {
			t.Fatalf("ParseDisplay(%q) = %v, %v, %v", raw, d, probs, err)
		}
	}
}

func TestParseDisplayMalformedIsAnError(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{`[]`, `{"kind": 3}`, `{"kind": {"show": "badge"}}`, `{`} {
		if _, _, err := ParseDisplay([]byte(raw)); err == nil {
			t.Errorf("ParseDisplay(%s) = nil error", raw)
		}
	}
}

func TestParseDisplayUnknownFieldIsAProblemNotAnError(t *testing.T) {
	t.Parallel()
	d, probs, err := ParseDisplay([]byte(`{"kind": {"show": ["badge"], "values": {"issue": {"colour": "red"}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(probs) != 1 || !strings.Contains(string(probs[0]), "colour") {
		t.Fatalf("problems = %v", probs)
	}
	if _, ok := d["kind"]; !ok {
		t.Fatalf("display lost its key: %v", d)
	}
}

func TestValidateDropsBadEntries(t *testing.T) {
	t.Parallel()
	d := Display{
		"kind": {Show: []string{"badge", "sidebar"}, Style: "bold", As: "colour",
			Values: map[string]Value{
				"issue": {Color: "crimson", Glyph: "✓", Level: "urgent", Audience: "everyone"},
				"good":  {Color: "green", Glyph: "+", Level: "info", Audience: "reviewer"},
			}},
	}
	got, probs := validate(d)
	k := got["kind"]
	if len(k.Show) != 1 || k.Show[0] != "badge" || k.Style != "" || k.As != "" {
		t.Fatalf("key = %+v", k)
	}
	if v := k.Values["issue"]; v != (Value{}) {
		t.Fatalf("issue kept bad fields: %+v", v)
	}
	if v := k.Values["good"]; v != (Value{Color: "green", Glyph: "+", Level: "info", Audience: "reviewer"}) {
		t.Fatalf("good = %+v", v)
	}
	// sidebar, bold, colour(as), crimson, ✓, urgent, everyone
	if len(probs) != 7 {
		t.Fatalf("problems = %d %v", len(probs), probs)
	}
}

func TestValidateCapsKeysAndValues(t *testing.T) {
	t.Parallel()
	d := Display{}
	for i := 0; i < 10; i++ {
		d[string(rune('a'+i))] = Key{Order: i, Show: []string{"line"}}
	}
	vals := map[string]Value{}
	for i := 0; i < 20; i++ {
		vals[string(rune('A'+i))] = Value{Color: "red"}
	}
	d["a"] = Key{Order: 0, Show: []string{"badge"}, Values: vals}
	got, probs := validate(d)
	if len(got) != MaxDisplayKeys {
		t.Fatalf("kept %d keys", len(got))
	}
	if _, ok := got["i"]; ok {
		t.Fatalf("kept key i past the cap (order 8)")
	}
	if len(got["a"].Values) != MaxDisplayValues {
		t.Fatalf("kept %d values", len(got["a"].Values))
	}
	if len(probs) != 2+4 {
		t.Fatalf("problems = %d %v", len(probs), probs)
	}
}

func TestEffectiveNearestReplacesWholeKey(t *testing.T) {
	t.Parallel()
	doc := Display{"kind": {Show: []string{"badge"}, Label: "kind"}, "intent": {Show: []string{"line"}}}
	file := Display{"kind": {Show: []string{"line"}}}
	got := Effective(doc, file, nil)
	if k := got["kind"]; len(k.Show) != 1 || k.Show[0] != "line" || k.Label != "" {
		t.Fatalf("kind = %+v (the nearer entry must replace, not merge)", k)
	}
	if _, ok := got["intent"]; !ok {
		t.Fatal("intent lost")
	}
	if Effective(nil, nil) != nil {
		t.Fatal("no levels must stay nil")
	}
}
