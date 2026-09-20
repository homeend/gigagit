package mcp

import (
	"context"
	"errors"
	"fmt"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// The gg:// link surface. A link is the ONE portable way to name a place in a
// repository — a file, a line, a commit, a branch tip, a change-set — so an
// agent handed one by a human (or by another gg frontend) can act on it
// without being told how to take it apart.
//
// Every tool here resolves against THIS server's repository. The server is
// rooted at one repo, like every other tool it serves, so a link naming a
// different checkout is refused rather than silently answered from somewhere
// else: opts.Cwd wins outright when it matches the link's identity, and no
// registry is consulted, so a foreign link reports ErrLinkUnknownRepo.

// linkResolveIn is one gg:// link to take apart.
type linkResolveIn struct {
	Link string `json:"link"`
}

// linkResolveOut is domain.Resolved flattened onto the wire. Optional fields
// are omitted rather than sent empty so an agent can tell "this link has no
// line" from "line 0".
type linkResolveOut struct {
	Repo     RepoInfo `json:"repo"`
	Checkout string   `json:"checkout"`
	Path     string   `json:"path,omitempty"`
	State    string   `json:"state"`
	Commit   string   `json:"commit,omitempty"`
	// Ref is the branch or tag NAME when the link named a tip (@ref:<name>).
	// The name is what travels and what a consumer re-resolves; Commit
	// carries the tip as it resolved HERE (ruling R2).
	Ref string `json:"ref,omitempty"`
	// PairA/PairB are the change-set's ends when the link named one
	// (@<a>..<b>), each a full sha on this checkout. A pair is BOUNDED — a
	// finite set of paths — which is why it can be compared but cannot be
	// used to anchor a note (ruling R4).
	PairA string `json:"pair_a,omitempty"`
	PairB string `json:"pair_b,omitempty"`
	// PreviewSource/PreviewTarget are the merge preview's branch NAMES when
	// the link named one (@<target>...<source>).
	PreviewSource string `json:"preview_source,omitempty"`
	PreviewTarget string `json:"preview_target,omitempty"`
	Line          int    `json:"line,omitempty"`
	Side          string `json:"side,omitempty"`
	Hunk          int    `json:"hunk,omitempty"`
	// HintKind/HintID name the UI surface the link was copied from
	// ("bookmark", "shelf" or "stash"). A hint never changes WHERE the link
	// points — only which row a consumer reveals there (spec §3.3).
	HintKind string `json:"hint_kind,omitempty"`
	HintID   string `json:"hint_id,omitempty"`
}

type linkListOut struct {
	Repo  RepoInfo      `json:"repo"`
	Links []linkHistRow `json:"links"`
}

type linkHistRow struct {
	Link    string `json:"link"`
	Desc    string `json:"desc,omitempty"`
	Created string `json:"created,omitempty"`
}

type compareLinksIn struct {
	Left  string `json:"left"`
	Right string `json:"right"`
}

type compareLinksOut struct {
	Repo         RepoInfo        `json:"repo"`
	LeftDisplay  string          `json:"left_display"`
	RightDisplay string          `json:"right_display"`
	Files        []commitFileRow `json:"files"`
}

// resolveLinkArg parses and resolves one link against this server's repo.
// Both failures are TOOL errors carrying the underlying refusal's own text —
// never a panic and never an opaque 500, which is the shape plan 1b had to
// fix in the web arms.
func (s *Server) resolveLinkArg(ctx context.Context, raw string) (domain.Resolved, error) {
	if raw == "" {
		return domain.Resolved{}, fmt.Errorf("link is required")
	}
	l, err := model.ParseLink(raw)
	if err != nil {
		return domain.Resolved{}, err
	}
	// Cwd wins outright when it matches the link's identity, and no registry
	// path is supplied, so a link naming another checkout cannot be answered
	// from somewhere this server was never pointed at.
	return domain.ResolveLink(ctx, l, domain.ResolveOpts{Cwd: s.svc})
}

func (s *Server) registerLinkTools(srv *sdk.Server) {
	sdk.AddTool(srv, &sdk.Tool{
		Name: "gg_link_resolve",
		Description: "Take apart a gg:// link: which checkout, path, state, commit, branch tip, change-set, line, side, hunk and landing hint it names. " +
			"Use this when a human hands you a gg:// link and you need the place it points at.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in linkResolveIn) (*sdk.CallToolResult, linkResolveOut, error) {
		out := linkResolveOut{Repo: s.repoInfo()}
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		res, err := s.resolveLinkArg(ctx, in.Link)
		if err != nil {
			return nil, out, err
		}
		out.Checkout = res.Checkout
		out.Path = res.Addr.Path
		out.State = fileStateProto(res.Addr.State)
		out.Commit = res.Commit
		out.Ref = res.Ref
		if p := res.Pair; p != nil {
			out.PairA, out.PairB = p.A, p.B
		}
		if pv := res.Preview; pv != nil {
			out.PreviewSource, out.PreviewTarget = pv.Source, pv.Target
		}
		out.Line = res.Line
		if res.Line > 0 {
			out.Side = string(res.Side)
		}
		out.Hunk = res.Hunk
		out.HintKind, out.HintID = res.Hint.Kind, res.Hint.ID
		return nil, out, nil
	})

	sdk.AddTool(srv, &sdk.Tool{
		Name: "gg_link_list",
		Description: "This repository's recently copied gg:// links, newest first, with the label captured when each was copied. " +
			"Use it to pick up a place a human just copied without asking them to paste it.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, linkListOut, error) {
		// Links is pre-seeded so a JSON consumer never sees null: a repo with
		// nothing copied yet is the state every user starts in.
		out := linkListOut{Repo: s.repoInfo(), Links: []linkHistRow{}}
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		for _, e := range s.svc.LinkHistory(ctx) {
			out.Links = append(out.Links, linkHistRow{Link: e.Link, Desc: e.Desc, Created: e.Created})
		}
		return nil, out, nil
	})

	sdk.AddTool(srv, &sdk.Tool{
		Name: "gg_compare_links",
		Description: "Compare the two places two gg:// links name: the changed-file list with status letters. " +
			"Either side may be a file, a commit, a branch tip or a change-set; the comparison is total over every combination.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in compareLinksIn) (*sdk.CallToolResult, compareLinksOut, error) {
		out := compareLinksOut{Repo: s.repoInfo(), Files: []commitFileRow{}}
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		// domain.CompareLinks is the ONE door (spec D6): it locates each link
		// before evaluating it, which is what splits a local-form link's
		// checkout from its file. This tool used to evaluate without locating,
		// so a file link compared the whole tree, and its cross-repository
		// pre-check compared two unsplit repo halves — two files of one
		// checkout read as two repositories.
		//
		// No registry is supplied, the policy resolveLinkArg documents: a link
		// naming another checkout has no candidate here and is refused as
		// unknown, so the door's cross-repository refusal is unreachable from
		// this server by design.
		c, err := s.svc.CompareLinks(ctx, in.Left, in.Right, domain.ResolveOpts{Cwd: s.svc})
		if err != nil {
			var se *domain.LinkSideError
			if errors.As(err, &se) {
				return nil, out, err // already says which side
			}
			return nil, out, fmt.Errorf("comparing: %v", err)
		}
		files := c.Files
		out.LeftDisplay, out.RightDisplay = in.Left, in.Right
		for _, f := range files {
			out.Files = append(out.Files, commitFileRowFrom(f))
		}
		return nil, out, nil
	})
}
