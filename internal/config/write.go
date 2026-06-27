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

// SetGlobalRefreshDisableAdaptive persists `[refresh] disable_adaptive` to the
// global config file (preserving comments), backing the Settings "Adaptive
// intervals" toggle — the third runtime config writer (see
// SetGlobalDebugLogOperations / SetGlobalRefreshEnabled). Unlike the other two
// (default-off positives), this backs a default-ON toggle, so it writes the
// explicit true/false either way.
func SetGlobalRefreshDisableAdaptive(path string, disable bool) error {
	return setScalarLine(path, "refresh", "disable_adaptive", strconv.FormatBool(disable))
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
	for i, ln := range lines {
		trimmed := strings.TrimSpace(ln)
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
