package theme

import (
	"reflect"
	"strings"
	"testing"
)

func TestValidColour(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"#000000", true},
		{"#E9E9E5", true},
		{"#e9e9e5", true},
		{"0", true},
		{"240", true},
		{"255", true},
		{"", false},
		{"#12", false},
		{"#1234567", false},
		{"#gggggg", false},
		{"256", false},
		{"-1", false},
		{"+5", false},
		{"12.5", false},
		{"red", false},
		{" 240", false},
	} {
		if got := ValidColour(tc.in); got != tc.want {
			t.Errorf("ValidColour(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestOverlayAppliesSetFieldsOnly(t *testing.T) {
	t.Parallel()
	o := Override{Bg: "#112233", Dim: "42"}
	got, bad := Overlay(Light, o)
	if len(bad) != 0 {
		t.Fatalf("valid override reported %v", bad)
	}
	if got.Bg != "#112233" || got.Dim != "42" {
		t.Fatalf("set fields not applied: bg=%q dim=%q", got.Bg, got.Dim)
	}
	if got.Fg != Light.Fg || got.Muted != Light.Muted || got.Lanes != Light.Lanes || got.Syntax != Light.Syntax {
		t.Fatalf("unset fields must keep the base theme, got %+v", got)
	}
	if got.Name != Light.Name {
		t.Fatalf("Name must survive the overlay, got %q", got.Name)
	}
}

func TestOverlayZeroOverrideIsIdentity(t *testing.T) {
	t.Parallel()
	got, bad := Overlay(Dark, Override{})
	if len(bad) != 0 {
		t.Fatalf("zero override reported %v", bad)
	}
	if got != Dark {
		t.Fatalf("zero override must return the base unchanged, got %+v", got)
	}
}

func TestOverlayReportsInvalidAndKeepsValidSiblings(t *testing.T) {
	t.Parallel()
	o := Override{
		Bg:     "#12",
		Fg:     "#00FF00",
		Lanes:  []string{"1", "2", "3", "4", "5", "6"},
		Syntax: []string{"", "#111111", "zz", "", "", "", "", "", "", "", ""},
	}
	got, bad := Overlay(Terminal, o)
	want := []string{"bg=#12", "lanes=[6 entries, want 7]", "syntax[2]=zz"}
	if !reflect.DeepEqual(bad, want) {
		t.Fatalf("bad = %q, want %q", bad, want)
	}
	if got.Bg != "" {
		t.Fatalf("invalid bg must be skipped, got %q", got.Bg)
	}
	if got.Fg != "#00FF00" {
		t.Fatalf("valid sibling must still apply, fg = %q", got.Fg)
	}
	if got.Lanes != Terminal.Lanes {
		t.Fatalf("a wrong-length lanes list must be rejected whole, got %v", got.Lanes)
	}
	if got.Syntax[1] != "#111111" {
		t.Fatalf("valid syntax entry must apply, got %q", got.Syntax[1])
	}
	if got.Syntax[2] != "" {
		t.Fatalf("invalid syntax entry must be skipped, got %q", got.Syntax[2])
	}
}

// "" entries in lanes/syntax mean "inherit the base value", not "invalid" —
// Terminal.AsOverride() renders 7 empty lanes into the populate template.
func TestOverlayEmptyListEntriesInherit(t *testing.T) {
	t.Parallel()
	o := Terminal.AsOverride()
	got, bad := Overlay(Light, o)
	if len(bad) != 0 {
		t.Fatalf("all-empty override reported %v", bad)
	}
	if got != Light {
		t.Fatalf("all-empty override must leave the base alone, got %+v", got)
	}
}

func TestOverlayWrongLengthSyntaxReported(t *testing.T) {
	t.Parallel()
	_, bad := Overlay(Light, Override{Syntax: []string{"#111111"}})
	want := []string{"syntax=[1 entries, want 11]"}
	if !reflect.DeepEqual(bad, want) {
		t.Fatalf("bad = %q, want %q", bad, want)
	}
}

func TestMergePrecedence(t *testing.T) {
	t.Parallel()
	lo := Override{Bg: "#111111", Fg: "#222222", Lanes: []string{"1", "2", "3", "4", "5", "6", "7"}}
	hi := Override{Fg: "#333333", Dim: "#444444"}
	got := Merge(lo, hi)
	if got.Bg != "#111111" {
		t.Fatalf("lo-only field lost: bg = %q", got.Bg)
	}
	if got.Fg != "#333333" {
		t.Fatalf("hi must win: fg = %q", got.Fg)
	}
	if got.Dim != "#444444" {
		t.Fatalf("hi-only field lost: dim = %q", got.Dim)
	}
	if len(got.Lanes) != 7 || got.Lanes[0] != "1" {
		t.Fatalf("lo lanes must survive an hi with none: %v", got.Lanes)
	}
	hi2 := Override{Lanes: []string{"9", "9", "9", "9", "9", "9", "9"}}
	if got2 := Merge(lo, hi2); got2.Lanes[0] != "9" {
		t.Fatalf("hi lanes must win whole: %v", got2.Lanes)
	}
	// Merge must not alias lo's slice.
	got3 := Merge(lo, hi2)
	got3.Lanes[0] = "0"
	if lo.Lanes[0] != "1" {
		t.Fatal("Merge aliased the lo/hi slice instead of copying it")
	}
}

func TestAsOverrideRoundTrips(t *testing.T) {
	t.Parallel()
	for _, th := range []Theme{Light, Dark} {
		got, bad := Overlay(Terminal, th.AsOverride())
		if len(bad) != 0 {
			t.Fatalf("%s: AsOverride produced invalid entries %v", th.Name, bad)
		}
		got.Name = th.Name // Overlay never touches Name; Terminal's is "terminal".
		if got != th {
			t.Fatalf("%s: round trip = %+v, want %+v", th.Name, got, th)
		}
	}
}

// A future role must be added to roleFields, or these fail.
func TestRoleFieldsCoverEveryRole(t *testing.T) {
	t.Parallel()
	if len(roleFields) != len(Theme{}.roles()) {
		t.Fatalf("roleFields has %d entries, Theme has %d scalar roles — add a row in override.go", len(roleFields), len(Theme{}.roles()))
	}
	seen := map[string]int{}
	for _, f := range roleFields {
		seen[f.key]++
		if f.doc == "" {
			t.Errorf("roleFields[%s] has no doc", f.key)
		}
	}
	rt := reflect.TypeOf(Override{})
	for i := 0; i < rt.NumField(); i++ {
		key := rt.Field(i).Tag.Get("toml")
		if key == "" {
			t.Fatalf("Override field %s has no toml tag", rt.Field(i).Name)
		}
		if key == "lanes" || key == "syntax" {
			continue // slices, handled outside the scalar table
		}
		if seen[key] != 1 {
			t.Errorf("Override key %q appears %d times in roleFields, want exactly 1", key, seen[key])
		}
		delete(seen, key)
	}
	for key := range seen {
		if key == "lanes" || key == "syntax" {
			t.Errorf("roleFields must not carry the slice key %q", key)
			continue
		}
		t.Errorf("roleFields key %q has no Override field", key)
	}
}

// Every Override accessor must address a DISTINCT field (a copy/paste that
// points two rows at &o.Bg would otherwise pass the count check).
func TestRoleFieldsAccessorsAreDistinct(t *testing.T) {
	t.Parallel()
	var o Override
	var th Theme
	for i, f := range roleFields {
		*f.getO(&o) = f.key
		*f.get(&th) = f.key
		_ = i
	}
	for _, f := range roleFields {
		if got := *f.getO(&o); got != f.key {
			t.Errorf("Override accessor for %q reads back %q — two rows share a field", f.key, got)
		}
		if got := *f.get(&th); got != f.key {
			t.Errorf("Theme accessor for %q reads back %q — two rows share a field", f.key, got)
		}
	}
}

func TestRoleDocsOrderAndTail(t *testing.T) {
	t.Parallel()
	docs := RoleDocs()
	if len(docs) != len(roleFields)+2 {
		t.Fatalf("RoleDocs() = %d entries, want %d scalars + lanes + syntax", len(docs), len(roleFields))
	}
	if docs[0].Key != roleFields[0].key {
		t.Fatalf("RoleDocs must start in roleFields order, got %q", docs[0].Key)
	}
	if docs[len(docs)-2].Key != "lanes" || docs[len(docs)-1].Key != "syntax" {
		t.Fatalf("RoleDocs must end with lanes, syntax; got %q, %q", docs[len(docs)-2].Key, docs[len(docs)-1].Key)
	}
	for _, d := range docs {
		if d.Doc == "" {
			t.Errorf("RoleDocs[%s] has no description", d.Key)
		}
	}
	if !strings.Contains(docs[len(docs)-1].Doc, "inherit") {
		t.Fatalf("the syntax doc must explain that \"\" inherits: %q", docs[len(docs)-1].Doc)
	}
}
