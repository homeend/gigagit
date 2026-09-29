package exttool

import "testing"

func testTool() Tool {
	return Tool{ID: "t", Commands: []CommandTemplate{
		{Category: CatReview, Name: "A", Version: 3, Range: ">=2.1", Command: "<bin> new"},
		{Category: CatReview, Name: "A", Version: 3, Range: ">=1.8 <2.1", Command: "<bin> old"},
		{Category: CatConflict, Name: "B", Version: 1, Command: "<bin> b"},
	}}
}

func TestPickKnownVersion(t *testing.T) {
	t.Parallel()
	got := Pick(testTool(), Version{2, 0, 0}, true)
	if len(got) != 2 || got[0].Command != "<bin> old" || got[1].Name != "B" {
		t.Fatalf("Pick(2.0) = %+v", got)
	}
}

func TestPickUnknownVersionTakesNewestVariant(t *testing.T) {
	t.Parallel()
	got := Pick(testTool(), Version{}, false)
	if len(got) != 2 || got[0].Command != "<bin> new" {
		t.Fatalf("Pick(unknown) = %+v", got)
	}
}

func TestPickOutOfRangeDropsFamily(t *testing.T) {
	t.Parallel()
	got := Pick(testTool(), Version{1, 0, 0}, true)
	if len(got) != 1 || got[0].Name != "B" {
		t.Fatalf("Pick(1.0) = %+v", got)
	}
}

func TestVariantUnknownUsesStampRange(t *testing.T) {
	t.Parallel()
	ct, ok := Variant(testTool(), CatReview, "A", Version{}, false, ">=1.8 <2.1")
	if !ok || ct.Command != "<bin> old" {
		t.Fatalf("stamp range: %+v %v", ct, ok)
	}
	if _, ok := Variant(testTool(), CatReview, "A", Version{}, false, ""); ok {
		t.Fatal("multi-variant family, unknown version, no stamp: must be no variant")
	}
	if ct, ok := Variant(testTool(), CatConflict, "B", Version{}, false, ""); !ok || ct.Name != "B" {
		t.Fatal("single-variant family must resolve with an unknown version")
	}
}

// The catalog invariants (spec: overlapping ranges forbidden; "" alone).
func TestBuiltinsFamilyInvariants(t *testing.T) {
	t.Parallel()
	for _, tl := range Builtins() {
		fams := map[string][]CommandTemplate{}
		for _, ct := range tl.Commands {
			fams[ct.Key()] = append(fams[ct.Key()], ct)
		}
		for key, vs := range fams {
			for i, a := range vs {
				ra, err := ParseRange(a.Range)
				if err != nil {
					t.Fatalf("%s %q: %v", tl.ID, key, err)
				}
				if a.Version < 1 {
					t.Errorf("%s %q: Version %d < 1", tl.ID, key, a.Version)
				}
				if len(vs) > 1 && a.Range == "" {
					t.Errorf("%s %q: an any-version variant must be the family's only one", tl.ID, key)
				}
				for _, b := range vs[i+1:] {
					rb, _ := ParseRange(b.Range)
					if ra.Overlaps(rb) {
						t.Errorf("%s %q: ranges %q and %q overlap", tl.ID, key, a.Range, b.Range)
					}
					if a.Version != b.Version {
						t.Errorf("%s %q: variants disagree on Version", tl.ID, key)
					}
				}
			}
		}
		if HasRanged(tl) && tl.VersionArgs == nil {
			t.Errorf("%s has ranged variants but no VersionArgs", tl.ID)
		}
	}
}
