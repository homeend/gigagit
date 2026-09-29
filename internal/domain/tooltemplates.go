package domain

import (
	"context"
	"errors"
	"os"
	"os/exec"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/exttool"
)

// NewToolBlock is the config block gg writes for a catalog template: the
// rendered command plus the template stamp (AppendToolCommands adds the
// fingerprint). Every writer of catalog blocks goes through it.
func NewToolBlock(det exttool.Detection, ct exttool.CommandTemplate) config.ToolCommand {
	return config.ToolCommand{
		Category: string(ct.Category), Name: ct.Name, Mode: string(ct.Mode),
		PerFile: ct.PerFile, WhenOp: ct.WhenOp, Frontends: ct.Frontends,
		Command:         exttool.GenerateCommand(ct, det.Bin),
		TemplateVersion: ct.Version, AgentRange: ct.Range,
	}
}

// ToolStatusKind is a catalog block's standing against the catalog.
type ToolStatusKind int

const (
	ToolCurrent         ToolStatusKind = iota // matches its template
	ToolCustomised                            // user-edited, template unchanged: left alone
	ToolUpdateAvailable                       // a newer template or a better-fitting variant
	ToolUnsupported                           // the installed agent is outside every range
)

// ToolTemplateStatus is one effective catalog block's standing.
type ToolTemplateStatus struct {
	Path         string             // the config file holding the block
	Block        config.ToolCommand // as read
	Kind         ToolStatusKind
	New          config.ToolCommand // the offered block (ToolUpdateAvailable only)
	FromVersion  int                // the block's template_version (0 = unstamped)
	ToVersion    int                // the family's current Version
	AgentVersion string             // "" = unknown
	FromRange    string
	ToRange      string
	Edited       bool
	ToolLabel    string
}

// OfferKey identifies this exact offer — the "Keep mine" memory key, so a
// later template change offers again.
func (st ToolTemplateStatus) OfferKey() string {
	return st.Block.Key() + "\x00" + config.ToolFingerprint(st.New)
}

// ToolTemplateStatuses computes the standing of every EFFECTIVE block (a
// later path's same-key block shadows an earlier one — the overlay rule)
// that belongs to a detected catalog family. Blocks of undetected tools and
// user-authored blocks get no status.
func ToolTemplateStatuses(ctx context.Context, paths []string, dets []exttool.Detection) []ToolTemplateStatus {
	type located struct {
		path string
		tc   config.ToolCommand
	}
	var order []string
	eff := map[string]located{}
	for _, p := range paths {
		blocks, err := config.ToolCommandsIn(p)
		if err != nil {
			continue
		}
		for _, tc := range blocks {
			if _, seen := eff[tc.Key()]; !seen {
				order = append(order, tc.Key())
			}
			eff[tc.Key()] = located{p, tc}
		}
	}
	var out []ToolTemplateStatus
	for _, key := range order {
		loc := eff[key]
		for _, det := range dets {
			fv := exttool.FamilyVersion(det.Tool, exttool.Category(loc.tc.Category), loc.tc.Name)
			if fv == 0 {
				continue
			}
			if st, ok := blockStatus(ctx, loc.path, loc.tc, det, fv); ok {
				out = append(out, st)
			}
			break
		}
	}
	return out
}

func blockStatus(ctx context.Context, path string, tc config.ToolCommand, det exttool.Detection, fv int) (ToolTemplateStatus, bool) {
	st := ToolTemplateStatus{Path: path, Block: tc, FromVersion: tc.TemplateVersion, ToVersion: fv,
		FromRange: tc.AgentRange, Edited: tc.Edited(), ToolLabel: det.Tool.Label}
	v, known := AgentVersion(ctx, det.Tool, det.Bin)
	if known {
		st.AgentVersion = v.String()
	}
	ct, ok := exttool.Variant(det.Tool, exttool.Category(tc.Category), tc.Name, v, known, tc.AgentRange)
	if !ok {
		if known {
			st.Kind = ToolUnsupported
			return st, true
		}
		return st, false // unknown version, no variant to name: no status
	}
	st.ToRange = ct.Range
	st.New = NewToolBlock(det, ct)
	switch {
	case !tc.Stamped():
		if config.ToolFingerprint(tc) == config.ToolFingerprint(st.New) {
			st.Kind = ToolCurrent
		} else {
			st.Kind = ToolUpdateAvailable
		}
	case tc.TemplateVersion < fv || tc.AgentRange != ct.Range:
		st.Kind = ToolUpdateAvailable
	case st.Edited:
		st.Kind = ToolCustomised
	default:
		st.Kind = ToolCurrent
	}
	return st, true
}

// ErrToolBlockChanged: the block changed on disk since its status was computed.
var ErrToolBlockChanged = errors.New("the block changed since the offer was made — reopen to see the current offer")

// ApplyToolUpdate writes st.New over st.Block in st.Path, after checking the
// file still holds that block (same meaning — a formatting-only edit is fine).
func ApplyToolUpdate(st ToolTemplateStatus) error {
	blocks, err := config.ToolCommandsIn(st.Path)
	if err != nil {
		return err
	}
	for _, tc := range blocks {
		if tc.Key() != st.Block.Key() {
			continue
		}
		if config.ToolFingerprint(tc) != config.ToolFingerprint(st.Block) {
			return ErrToolBlockChanged
		}
		_, err := config.ReplaceToolCommand(st.Path, tc.Key(), st.New)
		return err
	}
	return ErrToolBlockChanged
}

// ToolTemplateStatuses is the frontends' entry: the global file then the
// active repo file, against the tools detected on this machine.
func (s *Service) ToolTemplateStatuses(ctx context.Context) []ToolTemplateStatus {
	home, _ := os.UserHomeDir()
	return ToolTemplateStatuses(ctx, s.toolConfigPaths(ctx), exttool.Detect(exec.LookPath, os.Stat, home))
}
