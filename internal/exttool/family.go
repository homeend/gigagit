package exttool

// Key identifies a template's family — the config block key shape
// (config.ToolCommand.Key).
func (t CommandTemplate) Key() string { return string(t.Category) + "\x00" + t.Name }

// withDefaults fills Version 0 → 1 so only a bumped family spells its version.
func withDefaults(tools []Tool) []Tool {
	for i := range tools {
		for j := range tools[i].Commands {
			if tools[i].Commands[j].Version == 0 {
				tools[i].Commands[j].Version = 1
			}
		}
	}
	return tools
}

// HasRanged reports whether any variant of tl names a version range.
func HasRanged(tl Tool) bool {
	for _, ct := range tl.Commands {
		if ct.Range != "" {
			return true
		}
	}
	return false
}

// FamilyVersion is the family's template version (0 = no such family).
func FamilyVersion(tl Tool, category Category, name string) int {
	for _, ct := range tl.Commands {
		if ct.Category == category && ct.Name == name {
			return ct.Version
		}
	}
	return 0
}

// Variant picks the family's variant for an agent version. A known version
// picks the range holding it (none = unsupported). An unknown one picks the
// variant whose Range equals stampRange, else the family's only variant.
func Variant(tl Tool, category Category, name string, v Version, known bool, stampRange string) (CommandTemplate, bool) {
	var fam []CommandTemplate
	for _, ct := range tl.Commands {
		if ct.Category == category && ct.Name == name {
			fam = append(fam, ct)
		}
	}
	if known {
		for _, ct := range fam {
			if r, err := ParseRange(ct.Range); err == nil && r.Contains(v) {
				return ct, true
			}
		}
		return CommandTemplate{}, false
	}
	if len(fam) == 1 {
		return fam[0], true
	}
	for _, ct := range fam {
		if stampRange != "" && ct.Range == stampRange {
			return ct, true
		}
	}
	return CommandTemplate{}, false
}

// Pick is the catalog as offered for one agent version: one row per family,
// in catalog order. An unknown version offers each family's newest variant
// (the one with the highest lower bound), so the wizard still lists every
// family.
func Pick(tl Tool, v Version, known bool) []CommandTemplate {
	var out []CommandTemplate
	seen := map[string]int{}
	for _, ct := range tl.Commands {
		r, err := ParseRange(ct.Range)
		if err != nil {
			continue
		}
		if known && !r.Contains(v) {
			continue
		}
		i, dup := seen[ct.Key()]
		if !dup {
			seen[ct.Key()] = len(out)
			out = append(out, ct)
			continue
		}
		have, _ := ParseRange(out[i].Range)
		if have.Min.less(r.Min) {
			out[i] = ct
		}
	}
	return out
}

// PickBest is the install-time pick: one row per family, in catalog order —
// the variant whose range holds a known version, else (unknown version, or
// a known one outside every range) the family's newest variant, so an
// installer never hides a family.
func PickBest(tl Tool, v Version, known bool) []CommandTemplate {
	newest := Pick(tl, Version{}, false)
	if !known {
		return newest
	}
	fit := map[string]CommandTemplate{}
	for _, ct := range Pick(tl, v, true) {
		fit[ct.Key()] = ct
	}
	for i, ct := range newest {
		if f, ok := fit[ct.Key()]; ok {
			newest[i] = f
		}
	}
	return newest
}
