// Package i18n is the TUI's translation layer: English-text-as-key lookup
// over TOML bundles. The English string IS the key — a catalog miss returns
// the key itself, so untranslated strings degrade to English and extraction
// can land incrementally. Built-in bundles (ja/ko/zh/ru) are embedded; a
// custom file in $XDG_CONFIG_HOME/gg/lang/<code>.toml overlays a built-in
// per-key or adds a new language. Pure leaf: no gg imports.
package i18n

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"

	toml "github.com/pelletier/go-toml/v2"
)

//go:embed lang/*.toml
var builtinFS embed.FS

// Lang identifies a selectable language for the Settings picker.
type Lang struct {
	Code string // "ja", or a custom file's basename
	Name string // native display name from [meta] name; falls back to Code
}

// catalog is an immutable language snapshot; SetLanguage swaps the whole
// pointer, so T never sees a partially built map.
type catalog struct {
	code    string
	name    string
	strings map[string]string
}

var active atomic.Pointer[catalog]

func init() {
	active.Store(&catalog{code: "en", name: "English"})
}

// T translates key into the active language. On a catalog miss the key
// itself is the English text. With args the result goes through
// fmt.Sprintf (translations may reorder via %[n] indexing); without args it
// is returned verbatim, so a stray % in an arg-less string is harmless.
func T(key string, args ...any) string {
	c := active.Load()
	s := key
	if tr, ok := c.strings[key]; ok {
		s = tr
	}
	if len(args) == 0 {
		return s
	}
	return fmt.Sprintf(s, args...)
}

// ActiveCode reports the active language code ("en" when unset).
func ActiveCode() string { return active.Load().code }

// ActiveTranslations returns the active catalog's key→translation map —
// empty for English (English is the key itself; there is nothing to map).
// The returned map is the catalog's own storage and MUST NOT be mutated:
// catalogs are immutable once built (mergeBundle constructs the map fully
// before SetLanguage stores it) and shared across goroutines via the
// atomic pointer.
func ActiveTranslations() map[string]string {
	return active.Load().strings
}

// ActiveName reports the active language's native display name.
func ActiveName() string { return active.Load().name }

// SetLanguage builds and swaps the active catalog: the embedded bundle for
// code (if any) overlaid per-key by <customDir>/<code>.toml (if present).
// code "" or "en" resets to English. A code with neither an embedded nor a
// custom bundle is an error, as is a malformed bundle file; on error the
// previous catalog stays active (the caller fails soft to English itself if
// it wants that).
func SetLanguage(code, customDir string) error {
	if code == "" || code == "en" {
		active.Store(&catalog{code: "en", name: "English"})
		return nil
	}
	m := map[string]string{}
	name := ""
	found := false
	if data, err := builtinFS.ReadFile("lang/" + code + ".toml"); err == nil {
		n, merr := mergeBundle(m, data)
		if merr != nil {
			return fmt.Errorf("i18n: embedded bundle %s: %w", code, merr)
		}
		if n != "" {
			name = n
		}
		found = true
	}
	if customDir != "" {
		p := filepath.Join(customDir, code+".toml")
		if data, err := os.ReadFile(p); err == nil {
			n, merr := mergeBundle(m, data)
			if merr != nil {
				return fmt.Errorf("i18n: %s: %w", p, merr)
			}
			if n != "" {
				name = n
			}
			found = true
		} else if !os.IsNotExist(err) {
			// An existing-but-unreadable overlay must not silently behave
			// as "not found" — the user would see English with no clue why.
			return fmt.Errorf("i18n: %s: %w", p, err)
		}
	}
	if !found {
		return fmt.Errorf("i18n: language %q not found", code)
	}
	if name == "" {
		name = code
	}
	active.Store(&catalog{code: code, name: name, strings: m})
	return nil
}

// bundleFile is the on-disk bundle shape: a [meta] table and a flat
// [strings] table of English-key → translation.
type bundleFile struct {
	Meta struct {
		Name string `toml:"name"`
	} `toml:"meta"`
	Strings map[string]string `toml:"strings"`
}

// mergeBundle parses one bundle and overlays its strings onto dst per-key.
// A translation whose format verbs don't match its key's is skipped — that
// one string falls back to English while the rest of the bundle loads (the
// ValidateToolCommand inert-at-load convention). Returns [meta] name.
func mergeBundle(dst map[string]string, data []byte) (string, error) {
	var b bundleFile
	if err := toml.Unmarshal(data, &b); err != nil {
		return "", err
	}
	for k, v := range b.Strings {
		if CheckVerbs(k, v) != nil {
			continue
		}
		dst[k] = v
	}
	return b.Meta.Name, nil
}

// Available lists the selectable languages: English first, then the embedded
// bundles, then custom-only files from customDir, each group sorted by code.
// A custom file whose code matches a built-in merges into that entry (its
// [meta] name, when set, wins). "en" custom files are ignored — English is
// the key set itself, not a bundle.
func Available(customDir string) []Lang {
	names := map[string]string{}
	var builtin []string
	if ents, err := builtinFS.ReadDir("lang"); err == nil {
		for _, e := range ents {
			code := strings.TrimSuffix(e.Name(), ".toml")
			builtin = append(builtin, code)
			if data, rerr := builtinFS.ReadFile("lang/" + e.Name()); rerr == nil {
				names[code] = bundleName(data)
			}
		}
	}
	var custom []string
	if customDir != "" {
		if ents, err := os.ReadDir(customDir); err == nil {
			for _, e := range ents {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
					continue
				}
				code := strings.TrimSuffix(e.Name(), ".toml")
				if code == "en" {
					continue
				}
				name := ""
				if data, rerr := os.ReadFile(filepath.Join(customDir, e.Name())); rerr == nil {
					name = bundleName(data)
				}
				if _, isBuiltin := names[code]; isBuiltin {
					if name != "" {
						names[code] = name // overlay renames the built-in entry
					}
					continue
				}
				custom = append(custom, code)
				names[code] = name
			}
		}
	}
	sort.Strings(builtin)
	sort.Strings(custom)
	out := []Lang{{Code: "en", Name: "English"}}
	for _, c := range builtin {
		out = append(out, Lang{Code: c, Name: orCode(names[c], c)})
	}
	for _, c := range custom {
		out = append(out, Lang{Code: c, Name: orCode(names[c], c)})
	}
	return out
}

func orCode(name, code string) string {
	if name != "" {
		return name
	}
	return code
}

// bundleName reads just [meta] name from a bundle; "" on any parse trouble.
func bundleName(data []byte) string {
	var b bundleFile
	if toml.Unmarshal(data, &b) != nil {
		return ""
	}
	return b.Meta.Name
}

// Builtins exposes the embedded bundles raw (code → key → translation), for
// the enforcement tests in internal/tui — no verb filtering, so a bad entry
// is visible to the test rather than silently dropped.
func Builtins() map[string]map[string]string {
	out := map[string]map[string]string{}
	ents, err := builtinFS.ReadDir("lang")
	if err != nil {
		return out
	}
	for _, e := range ents {
		code := strings.TrimSuffix(e.Name(), ".toml")
		data, rerr := builtinFS.ReadFile("lang/" + e.Name())
		if rerr != nil {
			continue
		}
		var b bundleFile
		if toml.Unmarshal(data, &b) != nil {
			continue
		}
		if b.Strings == nil {
			b.Strings = map[string]string{}
		}
		out[code] = b.Strings
	}
	return out
}
