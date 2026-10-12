package theme

import "testing"

func TestOverrideMetaRoles(t *testing.T) {
	t.Parallel()
	out, bad := Overlay(Dark, Override{MetaRed: "#FF0000", MetaGrey: "245"})
	if len(bad) > 0 {
		t.Fatalf("bad = %v", bad)
	}
	if out.MetaRed != "#FF0000" || out.MetaGrey != "245" || out.MetaBlue != Dark.MetaBlue {
		t.Fatalf("overlay = %+v", out)
	}
	docs := map[string]bool{}
	for _, d := range RoleDocs() {
		docs[d.Key] = true
	}
	for _, k := range []string{"meta_red", "meta_orange", "meta_yellow", "meta_green", "meta_blue", "meta_purple", "meta_grey"} {
		if !docs[k] {
			t.Errorf("RoleDocs lacks %s", k)
		}
	}
	for _, th := range []Theme{Dark, Light} {
		for _, c := range []string{th.MetaRed, th.MetaOrange, th.MetaYellow, th.MetaGreen, th.MetaBlue, th.MetaPurple, th.MetaGrey} {
			if c == "" || !ValidColour(c) {
				t.Errorf("%s: meta colour %q", th.Name, c)
			}
		}
	}
}
