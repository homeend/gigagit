package i18n

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// CheckVerbs reports whether translation uses the same format-verb multiset
// as key. %% is a literal percent, not a verb. A dynamic width/precision
// (%*d) consumes an argument, so each * counts as its own multiset entry.
// An explicit argument index (%[2]s) is allowed — a translation may reorder
// arguments — but every index must address an argument the key actually
// supplies (Sprintf renders %!s(MISSING) otherwise). Keys themselves never
// use explicit indexes, so the key's verb count IS its argument count.
func CheckVerbs(key, translation string) error {
	kv, _ := verbs(key)
	tv, ti := verbs(translation)
	if len(kv) != len(tv) {
		return fmt.Errorf("i18n: translation has %d format verbs, key has %d", len(tv), len(kv))
	}
	skv := append([]string(nil), kv...)
	stv := append([]string(nil), tv...)
	sort.Strings(skv)
	sort.Strings(stv)
	for i := range skv {
		if skv[i] != stv[i] {
			return fmt.Errorf("i18n: translation verbs %v != key verbs %v", stv, skv)
		}
	}
	for _, n := range ti {
		if n < 1 || n > len(kv) {
			return fmt.Errorf("i18n: translation argument index %d out of range (key has %d arguments)", n, len(kv))
		}
	}
	return nil
}

// verbs extracts the format-verb letters of s, skipping %% and stripping
// flags, width, and precision from each clause. Each dynamic width or
// precision (*) is recorded as its own "*" entry because it consumes an
// argument. idx collects explicit argument indexes (%[2]s → 2); a malformed
// bracket emits the sentinel verb "!badindex", which can never match a key.
func verbs(s string) (out []string, idx []int) {
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
		if s[i] == '[' { // explicit argument index
			j := i + 1
			for j < len(s) && s[j] >= '0' && s[j] <= '9' {
				j++
			}
			if j == i+1 || j >= len(s) || s[j] != ']' {
				out = append(out, "!badindex")
				continue
			}
			n, _ := strconv.Atoi(s[i+1 : j])
			idx = append(idx, n)
			i = j + 1
			if i >= len(s) {
				break
			}
		}
		for i < len(s) {
			c := s[i]
			if c == '*' {
				out = append(out, "*")
				i++
				continue
			}
			if strings.ContainsRune("0123456789.#+- ", rune(c)) {
				i++
				continue
			}
			break
		}
		if i < len(s) {
			out = append(out, string(s[i]))
		}
	}
	return out, idx
}
