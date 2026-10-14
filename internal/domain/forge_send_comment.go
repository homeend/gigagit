package domain

import (
	"strings"

	"github.com/homeend/gigagit/internal/forge"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notemeta"
)

// forgeComment is note n as the forge-neutral comment (spec
// 2026-10-12-note-meta §8.1): its author's meta under its effective
// declaration (a reply's own, else its root's). Meta added later — a Mark,
// a double-check — is left out (it never leaves gg), and so are the
// stored-only keys and the declaration: Resolve draws only declared places.
func forgeComment(n model.Note, root *model.Note, key string) forge.Comment {
	d := n.Display
	if len(d) == 0 && root != nil {
		d = root.Display
	}
	res := notemeta.Resolve(n.Meta, d, nil)
	c := forge.Comment{Summary: strings.TrimSpace(n.Summary), Rationale: strings.TrimSpace(n.Rationale),
		Badges: res.Badges, Lines: res.Lines, Level: res.Level, Audience: res.Audience, MarkerKey: key}
	if n.Source == model.NoteSourceAgent {
		c.Author = agentSignature(n.Author)
	}
	return c
}

// reviewBodyOf is review r's body with summary as its text, signed by its
// agent and marked with the review note's id.
func reviewBodyOf(r Review, summary string) forge.ReviewBody {
	return forge.ReviewBody{Summary: strings.TrimSpace(summary), Author: agentSignature(r.Agent), MarkerKey: r.ID}
}

func agentSignature(name string) string {
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	return "agent"
}

// sendRenderer is the active provider's renderer, else GitHub's. It never
// probes: by the time anything is planned or read back, detection ran.
func (s *Service) sendRenderer() forge.Renderer {
	s.forgeMu.Lock()
	p := s.forgeLocal
	if p == nil {
		p = s.forgeActive
	}
	s.forgeMu.Unlock()
	if p == nil {
		return forge.GitHubRenderer{}
	}
	return forge.RendererFor(p)
}

// localRoot is reply d's root among the stored notes, nil when it has none
// (a draft reply to a GitHub thread).
func localRoot(byID map[string]model.Note, d model.Note) *model.Note {
	if r, ok := byID[d.ParentID]; ok && d.ParentID != "" {
		return &r
	}
	return nil
}
