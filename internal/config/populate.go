package config

import (
	"os"
	"strings"
)

// linePresent reports whether trimmed (a whitespace-stripped file line) records
// a presence of setting d — either as an active or commented assignment. For
// keys with a scalar default, it delegates to lineAssignsKey (which handles
// both "key = …" and "# key = …"). For nil-default keys (rendered as
// "# key   # …" with no "= value"), it performs a bare-key prefix match
// instead, since lineAssignsKey requires "=" after the key name.
func linePresent(trimmed string, d settingDoc) bool {
	if lineAssignsKey(trimmed, d.key) {
		return true
	}
	if d.value != nil {
		return false // scalar keys are only present if "=" was found
	}
	// nil-default key: present if the line (active or commented) is exactly
	// the key or the key followed by whitespace (no "=").
	bare := strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
	return bare == d.key || strings.HasPrefix(bare, d.key+" ") || strings.HasPrefix(bare, d.key+"\t")
}

// populate returns raw with every settingDocs key not already present added as
// a commented documentation line (default + description + " [populated]"). A
// key counts as present if any active or commented assignment for it exists
// within its [section]. Existing lines are preserved verbatim; the result is
// idempotent. An empty raw yields the full set, one [section] block per section.
func populate(raw string) string {
	var lines []string
	if len(raw) > 0 {
		lines = strings.Split(strings.TrimRight(raw, "\n"), "\n")
	}

	// present[section][key] = true if an assignment line (active or commented)
	// exists under that section.
	present := map[string]map[string]bool{}
	headerAt := map[string]int{} // section -> line index of its [section] header
	section := ""
	for i, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section = strings.TrimSuffix(strings.TrimPrefix(trimmed, "["), "]")
			if _, ok := headerAt[section]; !ok {
				headerAt[section] = i
			}
			continue
		}
		for _, d := range settingDocs {
			if d.section == section && linePresent(trimmed, d) {
				if present[section] == nil {
					present[section] = map[string]bool{}
				}
				present[section][d.key] = true
			}
		}
	}

	render := func(d settingDoc) string {
		if d.value == nil {
			return "# " + d.key + "   # " + d.comment + " [populated]"
		}
		return "# " + d.key + " = " + tomlScalar(d.value) + "   # " + d.comment + " [populated]"
	}

	// Absent docs grouped by section, in settingDocs order.
	order := []string{"worktree", "ui", "debug", "refresh"}
	missing := map[string][]string{}
	for _, d := range settingDocs {
		if present[d.section][d.key] {
			continue
		}
		missing[d.section] = append(missing[d.section], render(d))
	}

	// Insert into existing sections (back-to-front so earlier indices stay valid).
	type insertion struct {
		at   int
		body []string
	}
	var inserts []insertion
	for sec, body := range missing {
		if at, ok := headerAt[sec]; ok {
			inserts = append(inserts, insertion{at: at + 1, body: body})
		}
	}
	// Sort descending by index without importing sort: simple selection.
	for i := 0; i < len(inserts); i++ {
		max := i
		for j := i + 1; j < len(inserts); j++ {
			if inserts[j].at > inserts[max].at {
				max = j
			}
		}
		inserts[i], inserts[max] = inserts[max], inserts[i]
	}
	for _, ins := range inserts {
		lines = append(lines[:ins.at], append(append([]string{}, ins.body...), lines[ins.at:]...)...)
	}

	// Append brand-new sections in canonical order.
	for _, sec := range order {
		body, ok := missing[sec]
		if !ok {
			continue
		}
		if _, exists := headerAt[sec]; exists {
			continue // already inserted above
		}
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, "["+sec+"]")
		lines = append(lines, body...)
	}

	return strings.Join(lines, "\n") + "\n"
}

// PopulateFile reads path (a missing file is treated as empty), adds every
// settingDocs key not already present as a commented line, and writes the
// result back atomically — only when something changed. It returns the number
// of keys added.
func PopulateFile(path string) (added int, err error) {
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return 0, err
	}
	before := string(raw)
	after := populate(before)
	if after == before {
		return 0, nil
	}
	added = countAdded(before)
	if err := atomicWriteFile(path, []byte(after)); err != nil {
		return 0, err
	}
	return added, nil
}

// countAdded reports how many settingDocs keys are absent from raw (i.e. how
// many populate would add).
func countAdded(raw string) int {
	var lines []string
	if len(raw) > 0 {
		lines = strings.Split(strings.TrimRight(raw, "\n"), "\n")
	}
	present := map[string]map[string]bool{}
	section := ""
	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section = strings.TrimSuffix(strings.TrimPrefix(trimmed, "["), "]")
			continue
		}
		for _, d := range settingDocs {
			if d.section == section && linePresent(trimmed, d) {
				if present[section] == nil {
					present[section] = map[string]bool{}
				}
				present[section][d.key] = true
			}
		}
	}
	n := 0
	for _, d := range settingDocs {
		if !present[d.section][d.key] {
			n++
		}
	}
	return n
}
