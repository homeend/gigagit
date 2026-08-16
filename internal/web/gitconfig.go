package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/gitconfdocs"
	"github.com/homeend/gigagit/internal/model"
)

// The git-config explorer: the TUI's config popup, in the browser.
//
// Two catalogs meet here. domain.GitConfigRows knows what is SET (git's own
// `--list --show-scope`, joined onto `git help -c`); internal/gitconfdocs
// knows what a key MEANS — its real default, a one-line description, and the
// value kind, which is what lets a bool or an enum get a picker instead of a
// free-text field.
//
// The write side is the security boundary of this file. A loopback server that
// took `{key, value}` off the wire and handed both to `git config` would let
// any page in the browser set core.pager, or an alias, or credential.helper —
// which is arbitrary code execution the next time git runs. So the key is
// never passed through: it is RESOLVED against the curated catalog, and a key
// the catalog does not know is a 400 rather than a write. That deliberately
// narrows the surface against the TUI (which offers every `git help -c` key):
// keys outside the catalog are listed here read-only, and gg's own config
// files or a terminal remain the way to set them.
func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/gitconfig", s.handleGitConfigList)
		mux.HandleFunc("POST /api/gitconfig", writeGuard(s.handleGitConfigSet))
	})
}

// gitConfigRow is one key on the wire: what it means (catalog) merged with
// what it is set to (git). Effective/Scope are computed here rather than in
// the browser so both frontends agree on git's own precedence — local beats
// global beats the documented default.
type gitConfigRow struct {
	Key       string   `json:"key"`
	Kind      string   `json:"kind"` // bool | enum | string | int | "" (uncurated)
	Default   string   `json:"default"`
	Desc      string   `json:"desc"`
	Options   []string `json:"options,omitempty"`
	Local     string   `json:"local"`
	LocalSet  bool     `json:"local_set"`
	Global    string   `json:"global"`
	GlobalSet bool     `json:"global_set"`
	Effective string   `json:"effective"`
	Scope     string   `json:"scope"`    // repo | global | default
	Editable  bool     `json:"editable"` // in the curated catalog, so writable
	Section   string   `json:"section"`  // the key's leading segment, for grouping
}

func kindName(k gitconfdocs.Kind) string {
	switch k {
	case gitconfdocs.KindBool:
		return "bool"
	case gitconfdocs.KindEnum:
		return "enum"
	case gitconfdocs.KindInt:
		return "int"
	}
	return "string"
}

func sectionOf(key string) string {
	if i := strings.Index(key, "."); i > 0 {
		return key[:i]
	}
	return key
}

// fill computes the effective value and the scope it came from.
func (row *gitConfigRow) fill() {
	switch {
	case row.LocalSet:
		row.Effective, row.Scope = row.Local, "repo"
	case row.GlobalSet:
		row.Effective, row.Scope = row.Global, "global"
	default:
		row.Effective, row.Scope = row.Default, "default"
	}
	row.Section = sectionOf(row.Key)
}

// handleGitConfigList answers the curated catalog with each key's set values,
// plus every key set outside the catalog (alias.*, tool sections, whatever the
// user has) as read-only rows. Both lists come back so the browser can show
// the whole truth about this repo's config while only offering to edit the
// half it can write safely.
func (s *Server) handleGitConfigList(w http.ResponseWriter, r *http.Request) {
	rows, err := s.service().GitConfigRows(readCtx(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// Set values, keyed lowercase: git reports set keys lowercased while the
	// catalog is camelCase, so the join has to be case-insensitive (the same
	// rule domain.GitConfigRows applies to its own join).
	set := make(map[string]model.GitConfigRow, len(rows))
	for _, r := range rows {
		if r.LocalSet || r.GlobalSet {
			set[strings.ToLower(r.Key)] = r
		}
	}
	catalog := make([]gitConfigRow, 0, len(gitconfdocs.All()))
	seen := make(map[string]bool, len(gitconfdocs.All()))
	for _, d := range gitconfdocs.All() {
		lk := strings.ToLower(d.Key)
		seen[lk] = true
		row := gitConfigRow{
			Key:      d.Key,
			Kind:     kindName(d.Kind),
			Default:  d.Default,
			Desc:     d.Desc,
			Options:  d.Options,
			Editable: true,
		}
		if s, ok := set[lk]; ok {
			row.Local, row.LocalSet = s.LocalValue, s.LocalSet
			row.Global, row.GlobalSet = s.GlobalValue, s.GlobalSet
		}
		row.fill()
		catalog = append(catalog, row)
	}
	extra := make([]gitConfigRow, 0, 8)
	for _, r := range rows {
		if !r.LocalSet && !r.GlobalSet {
			continue // catalog-only key with nothing set: not news
		}
		if seen[strings.ToLower(r.Key)] {
			continue
		}
		row := gitConfigRow{
			Key:       r.Key,
			Local:     r.LocalValue,
			LocalSet:  r.LocalSet,
			Global:    r.GlobalValue,
			GlobalSet: r.GlobalSet,
		}
		row.fill()
		extra = append(extra, row)
	}
	writeJSON(w, map[string]any{"catalog": catalog, "extra": extra})
}

type gitConfigSetRequest struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Global bool   `json:"global"`
	Unset  bool   `json:"unset"`
}

// handleGitConfigSet writes one catalog key at one scope and returns 202
// {op_id} — the op transport's shape, so the browser follows the write through
// the same SSE lane every other operation uses.
//
// The key is resolved against the catalog and the CATALOG's spelling is what
// reaches git: a request may not smuggle a different key past the check by
// varying its case, and a key with no Doc never reaches ConfigSet at all.
func (s *Server) handleGitConfigSet(w http.ResponseWriter, r *http.Request) {
	var req gitConfigSetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad request body: %w", err))
		return
	}
	doc := gitconfdocs.Lookup(req.Key)
	if doc == nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("%q is not a key gg can set from the browser", req.Key))
		return
	}
	if !req.Unset {
		if err := validateConfigValue(*doc, req.Value); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
	}
	run, err := s.startOp(engine.SetGitConfig{
		Key:    doc.Key, // the catalog's spelling, never the wire's
		Value:  req.Value,
		Global: req.Global,
		Unset:  req.Unset,
	})
	if err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{"op_id": run.id, "key": doc.Key})
}

// validateConfigValue refuses a value outside a CLOSED set — a bool that is
// not a bool, an enum outside its options. Strings and ints are stored as
// given: git ints take k/m/g suffixes and negative values, so a digits-only
// rule would refuse legitimate settings, and the catalog resolution above is
// the security boundary here, not the value's shape.
func validateConfigValue(doc gitconfdocs.Doc, value string) error {
	switch doc.Kind {
	case gitconfdocs.KindBool:
		if value != "true" && value != "false" {
			return fmt.Errorf("%s takes true or false", doc.Key)
		}
	case gitconfdocs.KindEnum:
		if !slices.Contains(doc.Options, value) {
			return fmt.Errorf("%s takes one of: %s", doc.Key, strings.Join(doc.Options, ", "))
		}
	default:
		if strings.ContainsAny(value, "\n\r") {
			return errors.New("a config value cannot contain a newline")
		}
	}
	return nil
}
