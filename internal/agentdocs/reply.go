package agentdocs

import (
	"encoding/json"
	"strconv"

	"github.com/homeend/gigagit/internal/notemeta"
	"github.com/homeend/gigagit/internal/steer"
)

// span is "path:start" or "path:start-end".
func span(n Note) string {
	s := n.Path + ":" + strconv.Itoa(n.Start)
	if n.End != n.Start {
		s += "-" + strconv.Itoa(n.End)
	}
	return s
}

// NoteReference is what the user pastes to the agent about a note (r): the
// id plus path:lines, which still mean something once the note is gone.
func NoteReference(n Note) string { return "gg note " + n.ID + " " + span(n) }

// NotedDetail is a note add's answer (gg session note add); lead is
// "; closed <path> (20 files open)" when opening the file pushed another
// one out (the retired note_add steer verb).
func NotedDetail(n Note, lead string) string { return "noted " + span(n) + " as " + n.ID + lead }

// NoteWire is the note in its protocol form; text (gg session note show)
// is the lines it sits on.
func NoteWire(n Note, fileID string, text []string) steer.FileNote {
	w := steer.FileNote{ID: n.ID, FileID: fileID, Path: n.Path, Start: n.Start, End: n.End,
		Summary: n.Summary, Rationale: n.Rationale, Author: n.Author, Outdated: n.Outdated, Text: text}
	if len(n.Meta) > 0 {
		w.Meta = map[string]string{}
		for _, kv := range n.Meta {
			w.Meta[kv.Key] = kv.Value
		}
	}
	if r := notemeta.Resolve(n.Meta, n.Display, nil); !r.Empty() {
		w.Display, _ = json.Marshal(r)
	}
	return w
}

// OverviewName is how a reply names an overview: its id and title (the
// display name overview-<n>.md is the frontends' own).
func OverviewName(id, title string) string { return "overview " + id + " " + strconv.Quote(title) }

// AnchorReference is what r copies about an anchor: enough for the agent to
// know which overview and which step the user means.
func AnchorReference(o Overview, a Anchor) string {
	return "gg overview " + o.ID + " " + strconv.Quote(o.Title) + " → " + a.Dest
}

// OverviewWire is the overview in its protocol form; state is the asking
// side's ("shown" | "background"), withText adds the markdown.
func OverviewWire(o Overview, state string, withText bool) steer.Overview {
	w := steer.Overview{ID: o.ID, Title: o.Title, State: state, Anchors: len(o.Anchors)}
	for _, a := range o.Anchors {
		if a.Missing {
			w.Unresolved = append(w.Unresolved, a.Dest)
		}
	}
	if withText {
		w.Text = o.Text
	}
	return w
}
