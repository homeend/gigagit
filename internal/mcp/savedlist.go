package mcp

import (
	"context"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// savedRow is one saved comparison. Kind says what the entry IS, because the
// three are opened differently: a "preview" (a merge preview, target...source)
// and a "pair" (a frozen commit pair, @a..b) are SET entries with one link —
// their id or label is what the note tools' `preview` argument takes — while a
// "comparison" holds two links, the two gg_compare_links takes.
type savedRow struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	Kind      string    `json:"kind"`
	Left      string    `json:"left"`
	LeftDesc  string    `json:"left_desc"`
	Right     string    `json:"right,omitempty"`
	RightDesc string    `json:"right_desc,omitempty"`
	Created   time.Time `json:"created"`
}

type savedListOut struct {
	Repo  RepoInfo   `json:"repo"`
	Saved []savedRow `json:"saved"`
}

// savedKind names a saved entry. A set entry's kind is read off its one
// link's grammar; a set link that is neither (none is written today) is
// reported as the plain "set" it is rather than guessed at.
func savedKind(c domain.SavedCompare, left model.Link) string {
	switch {
	case !c.IsSet():
		return "comparison"
	case left.Target.Preview != nil:
		return "preview"
	case left.Target.Pair != nil:
		return "pair"
	}
	return "set"
}

// describeSaved is the description a saved link reads as. A stored link always
// parses (SavedCompareAdd parses both halves), so the fallback is the text.
func (s *Server) describeSaved(ctx context.Context, text string) (model.Link, string) {
	l, err := model.ParseLink(text)
	if err != nil {
		return model.Link{}, text
	}
	return l, s.svc.DescribeLink(ctx, l)
}

func (s *Server) registerSavedListTools(srv *sdk.Server) {
	sdk.AddTool(srv, &sdk.Tool{
		Name: "gg_compare_list",
		Description: "This repository's SAVED comparisons (what `gg compare --list` prints): merge previews, frozen commit pairs and two-link comparisons, " +
			"each with its id, label, kind and gg:// link(s). A preview's or pair's id (or label) is what the note tools' `preview` argument takes; " +
			"a comparison's left and right are what gg_compare_links takes.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, savedListOut, error) {
		// Pre-seeded so a JSON consumer never sees null (gg_link_list's rule).
		out := savedListOut{Repo: s.repoInfo(), Saved: []savedRow{}}
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		cs, err := s.svc.SavedCompareList(ctx)
		if err != nil {
			return nil, out, err
		}
		for _, c := range cs {
			left, ldesc := s.describeSaved(ctx, c.Left)
			row := savedRow{ID: c.ID, Label: c.Label, Kind: savedKind(c, left), Left: c.Left, LeftDesc: ldesc, Created: c.Created}
			if !c.IsSet() {
				row.Right = c.Right
				_, row.RightDesc = s.describeSaved(ctx, c.Right)
			}
			out.Saved = append(out.Saved, row)
		}
		return nil, out, nil
	})
}
