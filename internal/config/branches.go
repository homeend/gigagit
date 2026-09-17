package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/branchfilter"
)

// BranchesConfig is the [branches] section: the five branch-filter slots as
// [[branches.filter]] blocks. The section exists so `gg config populate` has
// a plain [branches] header to document the blocks under — a bare
// [branch_filters] table would collide with array-of-table blocks of the
// same name.
type BranchesConfig struct {
	Filter []branchfilter.Slot `toml:"filter"`
}

// overlayBranchFilters is the second deliberate exception to the field-level
// overlay rule (the first is [[tools.command]]): blocks are keyed by slot
// number, and a repo block REPLACES the global block for that slot whole —
// a filter is one rule, not a set of independently inheritable fields.
// Slots the repo file omits fall through from global. Duplicates within one
// file are left in place (CompileAll keeps the first, so hand-editing never
// silently flips a rule).
func overlayBranchFilters(dst *BranchesConfig, src BranchesConfig) {
	if len(src.Filter) == 0 {
		return
	}
	// Replace only what a PREVIOUS layer contributed: drop every dst entry
	// whose slot this layer redefines, then append ALL of src — duplicates
	// included. Merging src into dst slot-by-slot instead would collapse a
	// same-file duplicate to last-wins before CompileAll ever sees it, so
	// the promised first-wins warning would never fire and the editor (which
	// targets the FIRST block) would write to a block nothing reads.
	defined := make(map[int]bool, len(src.Filter))
	for _, s := range src.Filter {
		defined[s.Slot] = true
	}
	out := make([]branchfilter.Slot, 0, len(dst.Filter)+len(src.Filter))
	for _, s := range dst.Filter {
		if !defined[s.Slot] {
			out = append(out, s)
		}
	}
	dst.Filter = append(out, src.Filter...)
}

const branchFilterHeader = "[[branches.filter]]"

// renderBranchFilter writes one block. Every field is emitted (even empty)
// so a later hand edit sees the whole vocabulary; strings use %q, which is
// valid TOML basic-string quoting for anything Go can print.
func renderBranchFilter(s branchfilter.Slot) []string {
	mode := s.Mode
	if mode == "" {
		mode = branchfilter.ModeHide
	}
	return []string{
		branchFilterHeader,
		"slot = " + strconv.Itoa(s.Slot),
		fmt.Sprintf("name = %q", s.Name),
		fmt.Sprintf("mode = %q", string(mode)),
		fmt.Sprintf("older_than = %q", strings.TrimSpace(s.OlderThan)),
		fmt.Sprintf("younger_than = %q", strings.TrimSpace(s.YoungerThan)),
		fmt.Sprintf("prefix = %q", s.Prefix),
		fmt.Sprintf("suffix = %q", s.Suffix),
		fmt.Sprintf("contains = %q", s.Contains),
		fmt.Sprintf("regex = %q", s.Regex),
	}
}

// branchFilterBlocks finds every ACTIVE [[branches.filter]] block in lines:
// [start, end) line spans (header included) and the slot number each carries
// (0 when the block has no parseable slot line). A commented header still
// ends the previous block, as everywhere else in this package.
func branchFilterBlocks(lines []string) (spans [][2]int, slots []int) {
	start := -1
	flush := func(end int) {
		if start < 0 {
			return
		}
		slot := 0
		skip := ""
		for _, ln := range lines[start+1 : end] {
			trimmed := strings.TrimSpace(ln)
			if skip != "" {
				if strings.Contains(trimmed, skip) {
					skip = ""
				}
				continue
			}
			// Only the first ACTIVE assignment counts: a commented `# slot = …`
			// left behind by a hand edit (or a prior gg write) must never
			// override the real one, or a later SetBranchFilter/RemoveBranchFilter
			// would target the wrong span — see the "trailing commented slot
			// line" regression this guards against.
			if !strings.HasPrefix(trimmed, "#") && slot == 0 && lineAssignsKey(trimmed, "slot") {
				_, v, _ := strings.Cut(trimmed, "=")
				// A trailing comment is part of the line, not of the value:
				// `slot = 2   # 1..5` must parse as 2, or both writers would
				// see slot 0 and miss the block entirely (Remove silently
				// no-ops, Set appends a duplicate). The value is an integer,
				// so there is no quoted string a `#` could belong to.
				v, _, _ = strings.Cut(v, "#")
				slot, _ = strconv.Atoi(strings.TrimSpace(v))
				continue
			}
			if d, ok := opensMultiline(trimmed); ok {
				skip = d
			}
		}
		spans = append(spans, [2]int{start, end})
		slots = append(slots, slot)
		start = -1
	}
	skip := ""
	for i, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if skip != "" {
			if strings.Contains(trimmed, skip) {
				skip = ""
			}
			continue
		}
		if name, commented, ok := sectionHeader(trimmed); ok || strings.HasPrefix(trimmed, "[") {
			flush(i)
			if ok && !commented && name == branchFilterHeader {
				start = i
			}
			continue
		}
		if d, ok := opensMultiline(trimmed); ok {
			skip = d
		}
	}
	flush(len(lines))
	return spans, slots
}

// SetBranchFilter writes s into the config file at path: the block whose
// `slot = N` matches is replaced in place, otherwise a new block is appended.
// Everything else in the file is preserved byte-for-byte. The file is
// created when missing.
func SetBranchFilter(path string, s branchfilter.Slot) error {
	if path == "" {
		return fmt.Errorf("config: no config path; refusing to write")
	}
	if s.Slot < 1 || s.Slot > branchfilter.MaxSlots {
		return fmt.Errorf("config: branch filter slot must be 1..%d", branchfilter.MaxSlots)
	}
	if c := branchfilter.Compile(s); c.Err != nil {
		return fmt.Errorf("config: branch filter slot %d: %w", s.Slot, c.Err)
	}
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var lines []string
	if len(raw) > 0 {
		lines = strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	}
	block := renderBranchFilter(s)
	spans, slots := branchFilterBlocks(lines)
	for i, span := range spans {
		if slots[i] != s.Slot {
			continue
		}
		// Keep any blank lines that trailed the old block: the span runs to
		// the next header, so trim trailing blanks off the body first.
		end := span[1]
		for end > span[0]+1 && strings.TrimSpace(lines[end-1]) == "" {
			end--
		}
		out := append([]string{}, lines[:span[0]]...)
		out = append(out, block...)
		out = append(out, lines[end:]...)
		return atomicWriteFile(path, []byte(strings.Join(out, "\n")+"\n"))
	}
	out := lines
	if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
		out = append(out, "")
	}
	out = append(out, block...)
	return atomicWriteFile(path, []byte(strings.Join(out, "\n")+"\n"))
}

// BranchFilterScopes reports which of the two files defines slot, decoding
// each on its own (no overlay) — the Settings "remove" needs provenance, not
// the effective value. An empty or unreadable path counts as not defining it.
func BranchFilterScopes(globalPath, repoPath string, slot int) (inGlobal, inRepo bool) {
	has := func(path string) bool {
		if path == "" {
			return false
		}
		c, ok, err := decodeFile(path)
		if err != nil || !ok {
			return false
		}
		for _, s := range c.Branches.Filter {
			if s.Slot == slot {
				return true
			}
		}
		return false
	}
	return has(globalPath), has(repoPath)
}

// RemoveBranchFilter deletes the ACTIVE block for slot from the file at
// path, with the blank line that separated it from what precedes it. A
// missing file or absent block is a no-op.
func RemoveBranchFilter(path string, slot int) error {
	if path == "" {
		return fmt.Errorf("config: no config path; refusing to write")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	spans, slots := branchFilterBlocks(lines)
	for i, span := range spans {
		if slots[i] != slot {
			continue
		}
		start := span[0]
		for start > 0 && strings.TrimSpace(lines[start-1]) == "" {
			start--
		}
		out := append([]string{}, lines[:start]...)
		rest := lines[span[1]:]
		if len(out) > 0 && len(rest) > 0 && strings.TrimSpace(rest[0]) != "" {
			out = append(out, "")
		}
		out = append(out, rest...)
		if len(out) == 0 {
			return atomicWriteFile(path, []byte(""))
		}
		return atomicWriteFile(path, []byte(strings.Join(out, "\n")+"\n"))
	}
	return nil
}
