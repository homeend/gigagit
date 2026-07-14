package i18n

import (
	"fmt"
	"sort"
	"strings"
)

// CheckVerbs reports whether translation uses the same format-verb multiset as
// key. %% is a literal percent, not a verb. An explicit argument index
// (%[2]s) is allowed — indexes, flags, width, and precision are stripped;
// only the verb letters must match as a multiset, so a translation may
// reorder arguments but never add, drop, or retype one.
func CheckVerbs(key, translation string) error {
	kv, tv := verbs(key), verbs(translation)
	if len(kv) != len(tv) {
		return fmt.Errorf("i18n: translation has %d format verbs, key has %d", len(tv), len(kv))
	}
	sort.Strings(kv)
	sort.Strings(tv)
	for i := range kv {
		if kv[i] != tv[i] {
			return fmt.Errorf("i18n: translation verbs %v != key verbs %v", tv, kv)
		}
	}
	return nil
}

// verbs extracts the format-verb letters of s, skipping %% and stripping
// flags, argument index, width, and precision from each clause.
func verbs(s string) []string {
	var out []string
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			continue
		}
		i++
		if i >= len(s) {
			break
		}
		if s[i] == '%' {
			continue // %% literal
		}
		for i < len(s) && strings.ContainsRune("0123456789.#+- []*", rune(s[i])) {
			i++
		}
		if i < len(s) {
			out = append(out, string(s[i]))
		}
	}
	return out
}
