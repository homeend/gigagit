package web

import (
	"context"
	"fmt"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	pf "github.com/homeend/gigagit/internal/preflight"
)

// noticeNoteEdits is the TUI's own feature-disabled notice id for note-edits
// (internal/tui/notify.go: noticeFeatureDisabledPrefix + the feature id), so
// a dismissal in either frontend silences both. PERMANENT.
const noticeNoteEdits = "feature_disabled_" + domain.FeatureNoteEdits

func init() { RegisterNotice(noteEditsNotice) }

// noteEditsNotice says a notes file a newer gg wrote keeps every note
// readable but refuses writes (spec 2026-10-12-note-meta §10) — the web's
// half of the TUI's standing notice.
func noteEditsNotice(ctx context.Context, _ *Server, svc *domain.Service, _ model.RepoHealth, _ map[string]bool) []noticeWire {
	vs, err := svc.Preflight(ctx)
	if err != nil {
		return nil
	}
	for _, v := range vs {
		if v.Feature.ID != domain.FeatureNoteEdits || v.State == pf.Satisfied {
			continue
		}
		detail := []string{fmt.Sprintf(v.Reason.Format, v.Reason.Args...)}
		if v.Remedy.Format != "" {
			detail = append(detail, fmt.Sprintf(v.Remedy.Format, v.Remedy.Args...))
		}
		return []noticeWire{{
			ID: noticeNoteEdits, Title: domain.FeatureNoteEdits + " is unavailable in this repository", Detail: detail,
			Actions: notNowNever(noticeNoteEdits),
		}}
	}
	return nil
}
