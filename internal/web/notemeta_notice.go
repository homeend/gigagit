package web

import "github.com/homeend/gigagit/internal/domain"

// noticeNoteEdits is the TUI's own feature-disabled notice id for note-edits
// (internal/tui/notify.go: noticeFeatureDisabledPrefix + the feature id), so
// a dismissal in either frontend silences both. PERMANENT. The notice itself
// is one of featureDisabledNotices' (notice_builders.go).
const noticeNoteEdits = noticeFeatureDisabledPrefix + domain.FeatureNoteEdits
