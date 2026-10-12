package notemeta

import (
	"reflect"
	"testing"
	"time"
)

var reviewDisplay = Display{
	"kind": {Show: []string{"badge", "count"}, Order: 1, Values: map[string]Value{
		"issue": {Color: "red", Audience: "author"},
		"good":  {Color: "green", Audience: "reviewer"},
	}},
	"severity": {Show: []string{"badge", "frame"}, Order: 2, Style: "text", Values: map[string]Value{
		"high": {Color: "red", Level: "high"},
		"low":  {Color: "grey", Level: "low"},
	}},
	"audience": {As: "audience", Order: 3},
	"intent":   {Show: []string{"line"}, Order: 4},
}

func TestResolvePlacesAndSemantics(t *testing.T) {
	t.Parallel()
	r := Resolve([]KV{{"intent", "checked the lock"}, {"kind", "issue"}, {"severity", "high"}, {"raised_by", "opus"}}, reviewDisplay, nil)
	wantBadges := []Badge{
		{Key: "kind", Value: "issue", Color: "red", Style: "filled"},
		{Key: "severity", Value: "high", Color: "red", Style: "text"},
	}
	if !reflect.DeepEqual(r.Badges, wantBadges) {
		t.Fatalf("badges = %+v", r.Badges)
	}
	if r.Frame != "red" || r.Level != "high" || r.Audience != "author" {
		t.Fatalf("frame %q level %q audience %q", r.Frame, r.Level, r.Audience)
	}
	if !reflect.DeepEqual(r.Lines, []Line{{"intent", "checked the lock"}}) {
		t.Fatalf("lines = %+v", r.Lines)
	}
	if !reflect.DeepEqual(r.Counted, []Counted{{Key: "kind", Label: "kind", Value: "issue", Color: "red"}}) {
		t.Fatalf("counted = %+v", r.Counted)
	}
	if !reflect.DeepEqual(r.Stored, []KV{{"raised_by", "opus"}}) {
		t.Fatalf("stored = %+v", r.Stored)
	}
}

func TestResolveAsKeyWinsOverValueSemantics(t *testing.T) {
	t.Parallel()
	r := Resolve([]KV{{"kind", "good"}, {"audience", "author"}}, reviewDisplay, nil)
	if r.Audience != "author" {
		t.Fatalf("audience = %q, want the direct key's author over good's reviewer", r.Audience)
	}
	r = Resolve([]KV{{"kind", "good"}, {"audience", "everyone"}}, reviewDisplay, nil)
	if r.Audience != "reviewer" || len(r.Problems) != 1 {
		t.Fatalf("an invalid direct value must not win: %q %v", r.Audience, r.Problems)
	}
}

func TestResolveNoDeclarationShowsEveryKeyAsALine(t *testing.T) {
	t.Parallel()
	r := Resolve([]KV{{"severity", "risk"}, {"confidence", "high"}}, nil, nil)
	if !reflect.DeepEqual(r.Lines, []Line{{"severity", "risk"}, {"confidence", "high"}}) || len(r.Badges) != 0 {
		t.Fatalf("lines %+v badges %+v", r.Lines, r.Badges)
	}
}

func TestResolveAddedOverridesAndShowsBuiltInStatus(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	r := Resolve([]KV{{"kind", "issue"}, {"severity", "high"}}, reviewDisplay,
		[]Added{{Key: "severity", Value: "low", By: "MR", At: at}, {Key: "status", Value: "disagreed", By: "MR", At: at}})
	if r.Level != "low" || r.Frame != "grey" {
		t.Fatalf("the added severity must drive level/frame: %q %q", r.Level, r.Frame)
	}
	last := r.Badges[len(r.Badges)-1]
	if last != (Badge{Key: "status", Value: "disagreed", Color: "red", Style: "text"}) {
		t.Fatalf("status badge = %+v", last)
	}
	want := []Change{{Key: "severity", From: "high", To: "low", By: "MR"}, {Key: "status", To: "disagreed", By: "MR"}}
	if !reflect.DeepEqual(r.Added, want) {
		t.Fatalf("added = %+v", r.Added)
	}
}

func TestResolveFrameOwnedByFirstKeyInOrder(t *testing.T) {
	t.Parallel()
	d := Display{
		"a": {Show: []string{"frame"}, Order: 2, Values: map[string]Value{"x": {Color: "blue"}}},
		"b": {Show: []string{"frame"}, Order: 1, Values: map[string]Value{"y": {Color: "green"}}},
	}
	if r := Resolve([]KV{{"a", "x"}, {"b", "y"}}, d, nil); r.Frame != "green" {
		t.Fatalf("frame = %q, want b's green (order 1)", r.Frame)
	}
}

func TestResolveUnlistedValueUsesOtherThenGrey(t *testing.T) {
	t.Parallel()
	d := Display{"kind": {Show: []string{"badge"}, Other: &Value{Color: "purple"}}}
	if b := Resolve([]KV{{"kind", "thought"}}, d, nil).Badges[0]; b.Color != "purple" {
		t.Fatalf("other colour = %q", b.Color)
	}
	d = Display{"kind": {Show: []string{"badge"}}}
	if b := Resolve([]KV{{"kind", "thought"}}, d, nil).Badges[0]; b.Color != "grey" || b.Style != "filled" {
		t.Fatalf("default badge = %+v", b)
	}
}

func TestResolveKeepsValidationProblems(t *testing.T) {
	t.Parallel()
	d := Display{"kind": {Show: []string{"badge"}, Values: map[string]Value{"issue": {Color: "crimson"}}}}
	r := Resolve([]KV{{"kind", "issue"}}, d, nil)
	if len(r.Problems) != 1 || r.Badges[0].Color != "grey" {
		t.Fatalf("problems %v badge %+v", r.Problems, r.Badges)
	}
}

func TestCount(t *testing.T) {
	t.Parallel()
	rs := []Resolved{
		Resolve([]KV{{"kind", "issue"}}, reviewDisplay, nil),
		Resolve([]KV{{"kind", "good"}}, reviewDisplay, nil),
		Resolve([]KV{{"kind", "good"}}, reviewDisplay, nil),
		Resolve([]KV{{"intent", "x"}}, reviewDisplay, nil),
	}
	c := Count(rs)
	want := []CountGroup{{Key: "kind", Label: "kind", Values: []CountValue{{"issue", "red", 1}, {"good", "green", 2}}}}
	if !reflect.DeepEqual(c.Groups, want) || c.Author != 1 || c.Reviewer != 2 {
		t.Fatalf("counts = %+v", c)
	}
}

func TestResolveValuesListEveryEffectiveKey(t *testing.T) {
	t.Parallel()
	d := Display{"severity": {Show: []string{"frame"}, Values: map[string]Value{"high": {Color: "red"}}}, "kind": {Order: 1}}
	r := Resolve([]KV{{"severity", "high"}, {"kind", "issue"}}, d, []Added{{Key: "status", Value: "agreed"}})
	want := []KV{{"severity", "high"}, {"kind", "issue"}, {"status", "agreed"}}
	if !reflect.DeepEqual(r.Values, want) {
		t.Fatalf("values = %v", r.Values)
	}
}
