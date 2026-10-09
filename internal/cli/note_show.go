package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

const noteShowUsage = "usage: gg note show [--json] <note-id|note-link>"

// noteShow prints one thread — what an agent handed a ?note= link reads.
func noteShow(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	asJSON := false
	var pos []string
	for _, a := range args {
		if a == "--json" || a == "-json" {
			asJSON = true
		} else {
			pos = append(pos, a)
		}
	}
	if len(pos) != 1 {
		fmt.Fprintln(stderr, noteShowUsage)
		return 2
	}
	ctx := context.Background()
	id := pos[0]
	if isLinkArg(id) {
		res, err := resolveLinkArg(ctx, svc, id, linkShapes{Ref: true, Pair: true}, "note show")
		if err != nil {
			return linkExit("note show", err, stderr)
		}
		if res.Hint.Kind != model.NoteHintKind {
			fmt.Fprintln(stderr, "note show: the link names no note (a note link carries ?note=<id>)")
			return 2
		}
		svc, id = openLinkTarget(res), res.Hint.ID
	}
	root, replies, resolved, err := svc.NoteThread(ctx, id)
	if err != nil {
		return noteIDExit("note show", id, svc, err, stderr)
	}
	link, _ := svc.NoteLinkText(ctx, root.ID)
	if asJSON {
		if replies == nil {
			replies = []model.Note{}
		}
		return jsonOut(stdout, stderr, map[string]any{"note": root, "replies": replies, "resolved": resolved, "link": link})
	}
	state := "open"
	if resolved != nil {
		state = "resolved"
	}
	where := root.Address.Path
	if root.Range[0] > 0 {
		where += fmt.Sprintf(":%d", root.Range[0])
		if root.Range[1] > root.Range[0] {
			where += fmt.Sprintf("-%d", root.Range[1])
		}
	}
	fmt.Fprintf(stdout, "note %s · %s · %s · %s\n", root.ID, root.Author, where, state)
	fmt.Fprintln(stdout, root.Summary)
	if t := strings.TrimSpace(root.Rationale); t != "" {
		fmt.Fprintln(stdout, "    "+strings.ReplaceAll(t, "\n", "\n    "))
	}
	if link != "" {
		fmt.Fprintln(stdout, "    "+link)
	}
	for _, r := range replies {
		fmt.Fprintf(stdout, "  ↳ %s (%s): %s\n", r.Author, r.ID, r.Summary)
		if t := strings.TrimSpace(r.Rationale); t != "" {
			fmt.Fprintln(stdout, "      "+strings.ReplaceAll(t, "\n", "\n      "))
		}
	}
	return 0
}
