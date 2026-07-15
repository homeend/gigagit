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
