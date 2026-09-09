package i18n

import (
	"strings"
	"testing"
)

// CheckVerbs compares a SORTED multiset, so a translation that reorders the
// key's arguments without explicit indexes ("%s のノート %d 件" for
// "This deletes %d notes from %s.") satisfies it and still renders
// "%!s(int=2) のノート %!d(string=f.txt) 件" at runtime. The mismatch then makes
// T fall back to the English key, so nothing visibly breaks in a CI that never
// switches language — the defect only shows up in front of the user who did.
//
// This renders every bundled translation with placeholder arguments of the
// key's own types and fails on any Sprintf error verb, which is exactly that
// class of bug.
func TestBundledTranslationsRenderWithoutErrorVerbs(t *testing.T) {
	defer SetLanguage("en", "")
	for _, l := range Available("") {
		if l.Code == "en" {
			continue
		}
		if err := SetLanguage(l.Code, ""); err != nil {
			t.Fatalf("SetLanguage(%s): %v", l.Code, err)
		}
		for key := range ActiveTranslations() {
			kv, _ := verbs(key)
			args := make([]any, len(kv))
			for i, v := range kv {
				switch v {
				case "d", "*":
					args[i] = 7
				case "f", "e", "g":
					args[i] = 1.5
				case "c":
					args[i] = 'x'
				default:
					args[i] = "X"
				}
			}
			if got := T(key, args...); strings.Contains(got, "%!") {
				t.Errorf("%s: %q renders %q — reorder an argument with an explicit index (%%[2]s), not by moving the verb", l.Code, key, got)
			}
		}
	}
}
