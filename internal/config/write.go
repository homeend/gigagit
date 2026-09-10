package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// SetGlobalDebugLogOperations persists `[debug] log_operations` to the global
// config file (path = DefaultGlobalPath()), preserving the rest of the file —
// comments included. It is the FIRST runtime writer of the committed TOML
// config (config is otherwise read-only at runtime); it exists only to back the
// , Settings menu's operation-log toggle, an action the user explicitly drives.
//
// The global file is targeted on purpose: the operation log is a machine-level
// diagnostic, and writing a repo `.gg.toml` could commit a debug flag into a
// tracked repo.
func SetGlobalDebugLogOperations(path string, on bool) error {
	return setScalarLine(path, "debug", "log_operations", strconv.FormatBool(on))
}

// SetGlobalRefreshEnabled persists `[refresh] enabled` to the global config
// file (preserving comments), backing the Settings master auto-refresh toggle
// — the second runtime config writer (see SetGlobalDebugLogOperations).
func SetGlobalRefreshEnabled(path string, on bool) error {
	return setScalarLine(path, "refresh", "enabled", strconv.FormatBool(on))
}

// SetGlobalDisableRemoteTagsAuto persists `[refresh] disable_remote_tags_auto`
// to the global config file (preserving comments), backing the Settings
// "Auto remote-tag refresh" toggle.
func SetGlobalDisableRemoteTagsAuto(path string, disabled bool) error {
	return setScalarLine(path, "refresh", "disable_remote_tags_auto", strconv.FormatBool(disabled))
}

// SetCommitSort persists `[ui] commit_sort` to the given config file (the repo
// .gg.toml), preserving comments, backing the Settings "Commit sort" cycle. The
// value is a quoted string ("plain"|"date-order"). Per-repo on purpose: ordering
// is a per-repo cost trade-off (a huge repo may want "plain").
func SetCommitSort(path, mode string) error {
	return setScalarLine(path, "ui", "commit_sort", strconv.Quote(mode))
}

// SetShowGraph persists `[ui] show_graph` to the given config file (the repo
// .gg.toml), preserving comments, backing the Settings "Show graph" toggle. The
// value is a quoted string ("on"|"off"). Per-repo on purpose: whether the lane
// graph or the flat list suits a repo depends on that repo's history shape.
func SetShowGraph(path, value string) error {
	return setScalarLine(path, "ui", "show_graph", strconv.Quote(value))
}

// SetGlobalUILanguage persists `[ui] language` to the given config file
// (callers pass DefaultGlobalPath() — a language is per-human, not
// per-repo), preserving comments. The normal [ui] overlay still lets a repo
// .gg.toml override it for the odd shared-demo repo.
func SetGlobalUILanguage(path, code string) error {
	return setScalarLine(path, "ui", "language", strconv.Quote(code))
}

// SetGlobalUITheme persists `[ui] theme = "<name>"` to the GLOBAL config
// (callers pass DefaultGlobalPath() — a theme is per-human, like language),
// preserving comments. The normal [ui] overlay still lets a repo .gg.toml
// override it.
func SetGlobalUITheme(path, name string) error {
	return setScalarLine(path, "ui", "theme", strconv.Quote(name))
}

// SetRefreshInterval persists `[refresh] <source> = secs` to the given config
// file (the repo .gg.toml), preserving the rest of the file. Backs the Settings
// "Refresh rates" inline editor.
func SetRefreshInterval(path, source string, secs int) error {
	return setScalarLine(path, "refresh", source, strconv.Itoa(secs))
}

// SetRefreshWatch persists `[refresh] <source>_watch = <bool>` to the given
// config file (the repo .gg.toml), preserving the rest of the file. Backs the
// Refresh-rates editor's per-source file-watch toggle. source is the bare key
// (e.g. "worktrees"); "_watch" is appended.
func SetRefreshWatch(path, source string, on bool) error {
	return setScalarLine(path, "refresh", source+"_watch", strconv.FormatBool(on))
}

// SetWorktreePostCreateHook persists [worktree] post_create_hook to the given
// config file (the repo .gg.toml) as a TOML multi-line literal string
// (triple-single-quote delimited), preserving comments and unrelated lines.
// A trailing newline in script is trimmed so re-saving a parsed value is
// idempotent; an empty script removes the key. Backs the Settings “Worktree
// post-create hook” editor.
func SetWorktreePostCreateHook(path, script string) error {
	return setMultilineLiteral(path, "worktree", "post_create_hook", script)
}

// SetVersionsDisabled persists `[versions] disabled` to the given config file
// (the repo .gg.toml), backing the Settings Operations history toggle.
func SetVersionsDisabled(path string, disabled bool) error {
	return setScalarLine(path, "versions", "disabled", strconv.FormatBool(disabled))
}

// SetVersionsMaxAgeDays persists `[versions] max_age_days` (-1 = keep forever)
// to the given config file, backing the Settings Operations history editor.
func SetVersionsMaxAgeDays(path string, days int) error {
	return setScalarLine(path, "versions", "max_age_days", strconv.Itoa(days))
}

// SetThemeRole persists one [themes.<theme>] colour role to the given config
// file (callers pass DefaultGlobalPath() — a theme is per-human, like the
// language), preserving every other line and comment. It backs the Settings
// "Theme colours…" editor.
//
// values carries the new colour: one entry for a scalar role
// (`key = "#rrggbb"`), the WHOLE list for a list role (`lanes = ["…", …]`, an
// entry may be "" = inherit). No values at all — or the single empty value ""
// — REMOVES the key, restoring the built-in default.
//
// The section is the dotted header `[themes.<theme>]`. A commented header (the
// `# [themes.light]   # … [populated]` block `gg config populate` writes) is
// uncommented IN PLACE rather than shadowed by a second table, and a commented
// role line inside it is replaced in place by the active assignment — so
// editing a populated file keeps its shape instead of growing a duplicate.
// That substitution applies ONLY while the file has no active table of that
// name; once one exists it wins, and the commented block stays an inert
// comment (see hasActiveSection).
func SetThemeRole(path, themeName, key string, values ...string) error {
	section := "themes." + themeName
	if len(values) == 0 || (len(values) == 1 && values[0] == "") {
		return setLineInSection(path, section, key, "", true)
	}
	rendered := key + " = "
	if len(values) == 1 {
		rendered += tomlScalar(values[0])
	} else {
		rendered += tomlStringList(values)
	}
	return setLineInSection(path, section, key, rendered, false)
}

// RemoveThemeTable deletes the ACTIVE `[themes.<theme>]` table from the given
// config file — the Settings colour editor's whole-theme reset (D). Every
// other line and comment survives; a file without an active table of that
// name (only the commented `gg config populate` block, or nothing) is left
// byte-identical, and a missing file is a no-op.
//
// Inside the table, an active assignment gg itself wrote (no trailing doc) is
// deleted, while a populate row the user hand-uncommented — still carrying its
// `[populated]` marker — is RE-COMMENTED, mirroring SetThemeRole's removal
// rule. The header follows the body: when anything non-blank survives — a
// populate row (the table was the example block, which stays a documented,
// inert example) or a comment of the user's own — the header is re-commented
// too, so the survivors stay inside a `# [themes.<name>]` block instead of
// drifting into the section above them; otherwise the header goes along with
// the blank line that separated it from what precedes it, so the file closes
// up as if the table had never been added.
// (A row SetThemeRole rewrote lost its marker at that point, so it is deleted
// rather than re-commented — the same fate `d` gives it.)
func RemoveThemeTable(path, themeName string) error {
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
	header := "[themes." + themeName + "]"
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")

	start, end := -1, len(lines) // header index; exclusive end of its body
	skipUntil := ""
	for i, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if skipUntil != "" {
			if strings.Contains(trimmed, skipUntil) {
				skipUntil = ""
			}
			continue
		}
		// Any bracketed line ends a section, as in setLineInSection.
		if name, commented, ok := sectionHeader(trimmed); ok || strings.HasPrefix(trimmed, "[") {
			if start >= 0 {
				end = i
				break
			}
			if ok && !commented && name == header {
				start = i
			}
			continue
		}
		if d, ok := opensMultiline(trimmed); ok {
			skipUntil = d
		}
	}
	if start < 0 {
		return nil
	}

	var keep []string
	survives := false // anything non-blank left inside the table
	skipUntil = ""
	for _, ln := range lines[start+1 : end] {
		trimmed := strings.TrimSpace(ln)
		if skipUntil != "" {
			// The interior of an active multi-line value goes with its key.
			if strings.Contains(trimmed, skipUntil) {
				skipUntil = ""
			}
			continue
		}
		active := trimmed != "" && !strings.HasPrefix(trimmed, "#")
		switch {
		case active && strings.Contains(ln, "[populated]"):
			keep = append(keep, "# "+ln)
			survives = true
		case active:
			// gg's own line: dropped.
			if d, ok := opensMultiline(trimmed); ok {
				skipUntil = d
			}
		default:
			keep = append(keep, ln)
			survives = survives || trimmed != ""
		}
	}

	out := append([]string(nil), lines[:start]...)
	if survives {
		out = append(out, "# "+header)
	} else {
		for len(keep) > 0 && strings.TrimSpace(keep[len(keep)-1]) == "" {
			keep = keep[:len(keep)-1]
		}
		if len(keep) == 0 && end >= len(lines) {
			for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
				out = out[:len(out)-1] // the separator blank would end the file
			}
		}
	}
	out = append(out, keep...)
	out = append(out, lines[end:]...)

	if len(out) == 0 {
		return atomicWriteFile(path, []byte(""))
	}
	return atomicWriteFile(path, []byte(strings.Join(out, "\n")+"\n"))
}

// sectionHeader reports the `[name]` (or `[[name]]`) table a trimmed line
// declares, and whether that declaration is commented out. A COMMENTED header
// still ends the preceding section: a populate-generated file is a run of
// `# [themes.<name>]` blocks, and a writer that read straight through them
// would drop a key for one theme inside another theme's commented block.
//
// The shape is deliberately strict — brackets around a bare dotted name, only
// whitespace or a trailing `#` comment after them — so a shell line inside a
// multi-line tool command (`[ -d x ] && …`) is never mistaken for a header.
func sectionHeader(trimmed string) (name string, commented, ok bool) {
	s := trimmed
	if strings.HasPrefix(s, "#") {
		commented = true
		s = strings.TrimSpace(strings.TrimPrefix(s, "#"))
	}
	open, closer := "[", "]"
	if strings.HasPrefix(s, "[[") {
		open, closer = "[[", "]]"
	}
	if !strings.HasPrefix(s, open) {
		return "", false, false
	}
	end := strings.Index(s[len(open):], closer)
	if end < 1 {
		return "", false, false
	}
	inner := s[len(open) : len(open)+end]
	for _, r := range inner {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.', r == '_', r == '-':
		default:
			return "", false, false
		}
	}
	rest := strings.TrimSpace(s[len(open)+end+len(closer):])
	if rest != "" && !strings.HasPrefix(rest, "#") {
		return "", false, false
	}
	return open + inner + closer, commented, true
}

// hasActiveSection reports whether lines already carry an UNCOMMENTED header for
// section, skipping multi-line string interiors the way every writer here does.
// It is the pre-scan that decides whether a commented header may stand in for
// the section at all.
func hasActiveSection(lines []string, header string) bool {
	skipUntil := ""
	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if skipUntil != "" {
			if strings.Contains(trimmed, skipUntil) {
				skipUntil = ""
			}
			continue
		}
		if name, commented, ok := sectionHeader(trimmed); ok && !commented && name == header {
			return true
		}
		if d, ok := opensMultiline(trimmed); ok {
			skipUntil = d
		}
	}
	return false
}

// setLineInSection sets (or removes) one whole `key = …` line under a possibly
// DOTTED, possibly commented-out `[section]` header, via the same line-oriented
// edit setScalarLine uses so unrelated lines and comments survive. rendered is
// the complete replacement line; remove ignores it.
//
// Removal has two shapes: a line gg itself wrote (no trailing doc) is deleted,
// while a hand-uncommented populate row — recognisable by the `[populated]`
// marker it still carries — is RE-COMMENTED, so the example block keeps its
// documented row instead of losing it.
func setLineInSection(path, section, key, rendered string, remove bool) error {
	if path == "" {
		return fmt.Errorf("config: no config path; refusing to write")
	}
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	header := "[" + section + "]"

	var lines []string
	if len(raw) > 0 {
		lines = strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	}

	// A commented header only STANDS FOR the section while the file has no
	// active one. Otherwise the two shapes below both corrupt the file: a
	// commented populate block ahead of the real table would be uncommented into
	// a SECOND [themes.x] ("table x already exists" — gg then refuses to start),
	// and one after it would have its `# key = …` line activated where it sits,
	// i.e. inside whatever active section precedes it, so the colour parses fine
	// under the wrong table and is silently gone next start.
	activeHeader := hasActiveSection(lines, header)

	var (
		headerAt        = -1
		headerCommented bool
		keyAt           = -1
		keyCommented    bool
		inSection       bool
		skipUntil       string
	)
	for i, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if skipUntil != "" {
			if strings.Contains(trimmed, skipUntil) {
				skipUntil = ""
			}
			continue
		}
		// ANY bracketed line outside a multi-line string ends the section, even a
		// shape this writer cannot name (a quoted table like [themes."my theme"]),
		// so keys can never leak across one into the target table.
		if name, commented, ok := sectionHeader(trimmed); ok || strings.HasPrefix(trimmed, "[") {
			target := ok && name == header && (!commented || !activeHeader)
			inSection = target
			if target && headerAt < 0 {
				headerAt, headerCommented = i, commented
			}
			continue
		}
		if inSection && keyAt < 0 && lineAssignsKey(trimmed, key) {
			keyAt = i
			keyCommented = strings.HasPrefix(trimmed, "#")
		}
		if d, ok := opensMultiline(trimmed); ok {
			skipUntil = d
		}
	}

	switch {
	case remove:
		if keyAt < 0 || keyCommented {
			return nil // already absent (or already inert): nothing to write
		}
		if strings.Contains(lines[keyAt], "[populated]") {
			lines[keyAt] = "# " + lines[keyAt]
		} else {
			lines = append(lines[:keyAt], lines[keyAt+1:]...)
		}
	case keyAt >= 0:
		lines[keyAt] = rendered
		if headerCommented {
			lines[headerAt] = header
		}
	case headerAt >= 0:
		if headerCommented {
			lines[headerAt] = header
		}
		lines = append(lines[:headerAt+1], append([]string{rendered}, lines[headerAt+1:]...)...)
	default:
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, header, rendered)
	}

	if len(lines) == 0 {
		return atomicWriteFile(path, []byte(""))
	}
	return atomicWriteFile(path, []byte(strings.Join(lines, "\n")+"\n"))
}

// setScalarLine sets `key = value` under `[section]` in a TOML file via a
// line-oriented edit so unrelated lines and comments survive. It updates an
// existing assignment (uncommenting a commented one), inserts the key under an
// existing section header, or appends a fresh section — then writes atomically.
func setScalarLine(path, section, key, value string) error {
	if path == "" {
		return fmt.Errorf("config: no global config path; refusing to write")
	}
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	want := key + " = " + value
	header := "[" + section + "]"

	var (
		lines      []string
		inSection  bool
		headerAt   = -1
		replacedAt = -1
	)
	if len(raw) > 0 {
		lines = strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	}
	var skipUntil string
	for i, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if skipUntil != "" {
			if strings.Contains(trimmed, skipUntil) {
				skipUntil = ""
			}
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inSection = trimmed == header
			if inSection {
				headerAt = i
			}
			continue
		}
		if inSection && lineAssignsKey(trimmed, key) {
			lines[i] = want
			replacedAt = i
			break
		}
		if d, ok := opensMultiline(trimmed); ok {
			skipUntil = d
		}
	}

	switch {
	case replacedAt >= 0:
		// updated in place
	case headerAt >= 0:
		// section present, key absent: insert right after the header.
		lines = append(lines[:headerAt+1], append([]string{want}, lines[headerAt+1:]...)...)
	default:
		// no section: append one (with a blank separator if the file is non-empty).
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, header, want)
	}

	return atomicWriteFile(path, []byte(strings.Join(lines, "\n")+"\n"))
}

// lineAssignsKey reports whether a line (already trimmed) is an assignment of
// key, whether active (`key = …`) or commented (`# key = …`).
func lineAssignsKey(trimmed, key string) bool {
	trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
	if !strings.HasPrefix(trimmed, key) {
		return false
	}
	rest := strings.TrimSpace(trimmed[len(key):])
	return strings.HasPrefix(rest, "=")
}

// opensMultiline reports whether a trimmed line opens a multi-line TOML string
// (”'/""") that is NOT closed on the same line, returning the closing
// delimiter. Used so line-oriented writers skip a multi-line value's interior
// (a script line like "[ -d x ]" must not be mistaken for a section header).
func opensMultiline(trimmed string) (delim string, opens bool) {
	for _, d := range []string{"'''", `"""`} {
		if i := strings.Index(trimmed, "= "+d); i >= 0 {
			if !strings.Contains(trimmed[i+len("= "+d):], d) {
				return d, true
			}
		}
	}
	return "", false
}

// setMultilineLiteral sets key to value under [section] using a TOML
// multi-line literal string (triple-single-quote delimited) via a
// line-oriented, delimiter-aware edit. An empty value removes the key.
func setMultilineLiteral(path, section, key, value string) error {
	if path == "" {
		return fmt.Errorf("config: no config path; refusing to write")
	}
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	header := "[" + section + "]"

	var lines []string
	if len(raw) > 0 {
		lines = strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	}

	// Replacement block (empty value ⇒ delete the key). TrimRight so a parsed
	// value's trailing newline does not accumulate a blank line on re-save.
	var block []string
	if strings.TrimSpace(value) != "" {
		block = append([]string{key + " = '''"}, strings.Split(strings.TrimRight(value, "\n"), "\n")...)
		block = append(block, "'''")
	}

	var (
		inSection bool
		headerAt  = -1
		startAt   = -1
		endAt     = -1
		skipUntil string
	)
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if skipUntil != "" {
			if strings.Contains(trimmed, skipUntil) {
				skipUntil = ""
			}
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inSection = trimmed == header
			if inSection {
				headerAt = i
			}
			continue
		}
		if inSection && startAt == -1 && lineAssignsKey(trimmed, key) {
			startAt = i
			if d, ok := opensMultiline(trimmed); ok {
				endAt = len(lines) - 1
				for j := i + 1; j < len(lines); j++ {
					if strings.Contains(strings.TrimSpace(lines[j]), d) {
						endAt = j
						break
					}
				}
			} else {
				endAt = i // single-line assignment
			}
			continue
		}
		if d, ok := opensMultiline(trimmed); ok {
			skipUntil = d
		}
	}

	switch {
	case startAt >= 0:
		tail := append([]string{}, lines[endAt+1:]...)
		lines = append(lines[:startAt], append(block, tail...)...)
	case headerAt >= 0:
		if len(block) > 0 {
			lines = append(lines[:headerAt+1], append(block, lines[headerAt+1:]...)...)
		}
	default:
		if len(block) > 0 {
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, header)
			lines = append(lines, block...)
		}
	}

	if len(lines) == 0 {
		return atomicWriteFile(path, []byte(""))
	}
	return atomicWriteFile(path, []byte(strings.Join(lines, "\n")+"\n"))
}

// atomicWriteFile writes data to path via a temp file + rename so a concurrent
// reader never sees a half-written config.
func atomicWriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "config-*.toml")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

// CopyRepoConfig copies the whole config file src → dst, creating dst's parent
// directories and writing atomically (temp file + rename, via atomicWriteFile).
// A missing src is an error the caller surfaces ("nothing to move"). Because the
// committed .gg.toml and the private file share the exact schema, this is a
// verbatim byte copy — no merge.
func CopyRepoConfig(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return atomicWriteFile(dst, data)
}

// RemoveRepoConfig deletes path. An absent path is not an error, so a move
// (copy + remove-source) is idempotent.
func RemoveRepoConfig(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ActiveRepoConfigPath resolves the active per-repo write target: the private
// user-dir file when it exists on disk, else the committed path. An empty
// privatePath (no main-worktree anchor) always yields committedPath. This is the
// rule that keeps a Settings toggle after "move to private" from recreating a
// committed .gg.toml.
func ActiveRepoConfigPath(committedPath, privatePath string) string {
	if privatePath != "" {
		if _, err := os.Stat(privatePath); err == nil {
			return privatePath
		}
	}
	return committedPath
}
