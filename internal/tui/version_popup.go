package tui

import (
	"strings"

	"github.com/homeend/gigagit/internal/buildinfo"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/mattn/go-runewidth"
)

// versionLines is the Version popup's body: which binary is running and when
// it was built. Every line is copyable.
func versionLines(in buildinfo.Info) []contentLine {
	built := in.Built
	if built == "" {
		built = i18n.T("unknown")
	} else if in.BuiltFromFile {
		built += " " + i18n.T("(binary file time)")
	}
	path := in.Path
	if path == "" {
		path = i18n.T("unknown")
	}
	rows := [][2]string{
		{i18n.T("Version"), in.Version},
		{i18n.T("Commit"), in.Commit},
		{i18n.T("Built"), built},
		{i18n.T("Binary"), path},
		{i18n.T("Platform"), in.Platform},
	}
	width := 0
	for _, r := range rows {
		width = max(width, runewidth.StringWidth(r[0]))
	}
	lines := make([]contentLine, 0, len(rows))
	for _, r := range rows {
		text := r[0] + strings.Repeat(" ", width+2-runewidth.StringWidth(r[0])) + r[1]
		lines = append(lines, contentLine{text: text, raw: text, src: true})
	}
	return lines
}
