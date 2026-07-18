package i18n

import "testing"

func TestCheckVerbs(t *testing.T) {
	cases := []struct {
		key, tr string
		ok      bool
	}{
		{"plain text", "テキスト", true},
		{"committed %s %s", "%[2]s %[1]s をコミット", true}, // reorder allowed
		{"%d files", "%d файлов", true},
		{"100%% done", "готово на 100%%", true}, // %% is a literal, not a verb
		{"committed %s %s", "%s をコミット", false},  // count mismatch
		{"%d files", "%s files", false},         // type mismatch
		{"no verbs", "oops %s", false},          // translation invents a verb
	}
	for _, c := range cases {
		err := CheckVerbs(c.key, c.tr)
		if c.ok && err != nil {
			t.Errorf("CheckVerbs(%q, %q) = %v, want nil", c.key, c.tr, err)
		}
		if !c.ok && err == nil {
			t.Errorf("CheckVerbs(%q, %q) = nil, want error", c.key, c.tr)
		}
	}
}

func TestCheckVerbsRejectsDynamicWidthMismatch(t *testing.T) {
	if err := CheckVerbs("%d items", "%*d items"); err == nil {
		t.Fatal("want error: translation adds a *-width arg the key does not have")
	}
	if err := CheckVerbs("%*d", "%*d"); err != nil {
		t.Fatalf("matching *-width must pass: %v", err)
	}
	if err := CheckVerbs("%*.*f", "%*.*f"); err != nil {
		t.Fatalf("matching *-width and *-precision must pass: %v", err)
	}
}

func TestCheckVerbsExplicitIndexRange(t *testing.T) {
	if err := CheckVerbs("%s and %s", "%[2]s / %[1]s"); err != nil {
		t.Fatalf("in-range reorder must pass: %v", err)
	}
	if err := CheckVerbs("%s", "%[9]s"); err == nil {
		t.Fatal("want error: index 9 out of range for a 1-arg key")
	}
	if err := CheckVerbs("%s", "%[0]s"); err == nil {
		t.Fatal("want error: index 0 is invalid (Sprintf indexes are 1-based)")
	}
	if err := CheckVerbs("%s", "%[s"); err == nil {
		t.Fatal("want error: malformed index bracket")
	}
}
