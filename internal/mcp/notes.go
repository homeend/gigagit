package mcp

// Review notes over MCP: the same surface `gg note …` exposes on the CLI, so an
// agent driving gg through either door leaves identical records. Reads are
// readOnly; the three mutators carry mutatingAnnotations() and are gated by the
// client's consent prompt like gg_write_to_worktree.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notebatch"
)

// wireNoteOutputSchema is a hand-supplied, permissive object schema for the
// three tools whose reply embeds domain.WireNote. WireNote nests its own
// Replies []WireNote for a thread's children, and the SDK's reflection-based
// schema inference (google/jsonschema-go) flatly refuses any recursive Go
// type ("cycle detected"). Supplying OutputSchema ourselves — exactly the
// escape hatch sdk.Tool documents — skips that inference; the actual JSON on
// the wire is unaffected, still produced by encoding/json from the real
// domain.WireNote value.
var wireNoteOutputSchema = &jsonschema.Schema{Type: "object"}

// noteTargetIn is the shared target trio: none = the unstaged working tree
// (untracked when git says so), cached = HEAD → index, rev = one commit.
type noteTargetIn struct {
	File   string `json:"file,omitempty"`
	Cached bool   `json:"cached,omitempty"`
	Rev    string `json:"rev,omitempty"`
	// Preview addresses a MERGE PREVIEW instead of a commit:
	// "<target>...<source>", or a saved preview's id or label. The note is
	// stored on the source TIP and anchors on the new side only — the
	// preview's old side is the merge base, which no stored address names.
	Preview string `json:"preview,omitempty"`
}

type notesListIn struct {
	noteTargetIn
	Type string `json:"type,omitempty"` // user | agent | all (default all)
}

type notesOut struct {
	Repo  RepoInfo          `json:"repo"`
	Notes []domain.WireNote `json:"notes"`
	// Contexts carries the batch's unanchored prose (agent-context v1's
	// top-level and per-file summaries) — gg_notes_apply's only user; a
	// gg_notes_list reply never sets it. The CLI has nowhere to hang this
	// text either, so it just echoes it to stderr as "context: …" — the MCP
	// reply hands it back structured instead.
	Contexts []string `json:"contexts,omitempty"`
	// Skipped and Warning are gg_notes_apply's only: a preview target skips
	// old-side items (its old side is the merge base, which no stored
	// address names) rather than refusing the whole batch — parity with the
	// CLI's `gg note apply --preview`, which warns to stderr and keeps
	// going. MCP has no stderr, so the same warning rides the reply instead.
	Skipped int    `json:"skipped,omitempty"`
	Warning string `json:"warning,omitempty"`
}

type noteAddIn struct {
	noteTargetIn
	Hunk      int    `json:"hunk,omitempty"`
	NewLine   int    `json:"new_line,omitempty"`
	OldLine   int    `json:"old_line,omitempty"`
	Summary   string `json:"summary"`
	Rationale string `json:"rationale,omitempty"`
	Author    string `json:"author,omitempty"`
}

type noteOut struct {
	Repo RepoInfo        `json:"repo"`
	Note domain.WireNote `json:"note"`
}

type notesApplyIn struct {
	noteTargetIn
	// Batch is typed any, not json.RawMessage: the SDK's reflection-based
	// input-schema inference has no special case for json.RawMessage (a named
	// []byte), so it infers "array of integers" and rejects the JSON object
	// every real batch actually is. any infers as an unrestricted schema;
	// the handler re-marshals it to bytes for notebatch.Parse.
	Batch  any    `json:"batch"`
	Author string `json:"author,omitempty"`
}

type noteRmIn struct {
	ID string `json:"id"`
}

type okOut struct {
	Repo RepoInfo `json:"repo"`
	OK   bool     `json:"ok"`
}

func (s *Server) registerNoteTools(srv *sdk.Server) {
	sdk.AddTool(srv, &sdk.Tool{
		Name: "gg_notes_list",
		Description: "List gg review notes, resolved against the current content. Omit file to list " +
			"every note this checkout can see. Target: none = the unstaged working tree, cached=true " +
			"= the staged diff, rev = one commit's own change. type filters user/agent/all. " +
			`preview = "<target>...<source>" (or a saved preview's id/label) addresses a MERGE PREVIEW ` +
			"instead: the note is stored on the source tip, new side only; status reports \"outdated\" " +
			"for a note whose lines a later commit changed.",
		Annotations:  readOnlyAnnotations(),
		OutputSchema: wireNoteOutputSchema,
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in notesListIn) (*sdk.CallToolResult, notesOut, error) {
		out := notesOut{Repo: s.repoInfo(), Notes: []domain.WireNote{}}
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		if in.Type != "" && in.Type != "all" && in.Type != "user" && in.Type != "agent" {
			return nil, out, fmt.Errorf("type must be user, agent or all")
		}
		if in.Preview != "" {
			set, perr := s.previewSet(ctx, in.noteTargetIn)
			if perr != nil {
				return nil, out, perr
			}
			res, rerr := s.previewNotesFor(ctx, set, in.File)
			if rerr != nil {
				return nil, out, rerr
			}
			for _, r := range res {
				if !noteTypeMatches(in.Type, r.Note.Source) {
					continue
				}
				out.Notes = append(out.Notes, domain.ToWireNotePreview(r, true))
			}
			return nil, out, nil
		}
		res, err := s.notesFor(ctx, in.noteTargetIn)
		if err != nil {
			return nil, out, err
		}
		for _, r := range res {
			if !noteTypeMatches(in.Type, r.Note.Source) {
				continue
			}
			out.Notes = append(out.Notes, domain.ToWireNote(r))
		}
		return nil, out, nil
	})

	sdk.AddTool(srv, &sdk.Tool{
		Name: "gg_note_add",
		Description: "Leave one anchored review note. Pass file plus exactly one of hunk (see " +
			"gg diff --hunks), new_line or old_line (1-based). Target: none = the unstaged working " +
			"tree, cached=true = the staged diff, rev = one commit (a range is refused). " +
			`preview = "<target>...<source>" (or a saved preview's id/label) addresses a MERGE PREVIEW ` +
			"instead: the note is stored on the source tip, new side only. MUTATES gg's note store.",
		Annotations:  mutatingAnnotations(),
		OutputSchema: wireNoteOutputSchema,
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in noteAddIn) (*sdk.CallToolResult, noteOut, error) {
		out := noteOut{Repo: s.repoInfo()}
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		if in.Summary == "" {
			return nil, out, fmt.Errorf("summary is required")
		}
		var addr model.FileAddress
		var side model.NoteSide
		var rng [2]int
		if in.Preview != "" {
			set, perr := s.previewSet(ctx, in.noteTargetIn)
			if perr != nil {
				return nil, out, perr
			}
			if in.File == "" {
				return nil, out, fmt.Errorf("preview needs file")
			}
			if in.OldLine != 0 {
				return nil, out, fmt.Errorf("notes in a preview anchor on the new side (drop old_line)")
			}
			// Exactly one anchor, counted BEFORE anything resolves: the
			// ordinary arm (noteAnchor) and the CLI both refuse two, so the
			// preview arm must not silently prefer new_line over hunk and
			// store a note at a line the caller did not mean.
			anchors := 0
			for _, v := range []int{in.Hunk, in.NewLine} {
				if v != 0 {
					anchors++
				}
			}
			if anchors != 1 {
				return nil, out, fmt.Errorf("pass exactly one of hunk or new_line")
			}
			// The address is an ORDINARY committed one on the tip, so it is
			// built by NoteTarget like every other target: that is what
			// normalises the path ("./a.txt", a Windows "sub\a.txt") and
			// refuses one escaping the repo — mirrors the CLI's `note add
			// --preview` (internal/cli/note.go).
			a, aerr := s.svc.NoteTarget(ctx, in.File, false, set.Tip)
			if aerr != nil {
				return nil, out, aerr
			}
			addr = a
			switch {
			case in.NewLine != 0:
				side, rng = model.NoteSideNew, [2]int{in.NewLine, in.NewLine}
			case in.Hunk != 0:
				s2, r2, herr := s.svc.PreviewHunkAnchor(ctx, set, addr.Path, in.Hunk)
				if herr != nil {
					return nil, out, herr
				}
				side, rng = s2, r2
			default:
				return nil, out, fmt.Errorf("pass exactly one of hunk or new_line")
			}
		} else {
			a, aerr := s.svc.NoteTarget(ctx, in.File, in.Cached, in.Rev)
			if aerr != nil {
				return nil, out, aerr
			}
			sd, rg, nerr := s.noteAnchor(ctx, a, in)
			if nerr != nil {
				return nil, out, nerr
			}
			addr, side, rng = a, sd, rg
		}
		author := domain.NoteAuthorDefault(in.Author)
		stored, err := s.svc.NoteAdd(ctx, model.Note{
			Source: model.NoteSourceAgent, Author: author, Address: addr,
			Side: side, Range: rng, Summary: in.Summary, Rationale: in.Rationale,
		})
		if err != nil {
			return nil, out, err
		}
		out.Note = domain.ToWireNote(domain.ResolvedNote{Note: stored, Status: model.NoteActive, Range: stored.Range})
		s.notifyNotesChanged()
		return nil, out, nil
	})

	sdk.AddTool(srv, &sdk.Tool{
		Name: "gg_notes_apply",
		Description: "Import a batch of anchored notes in one call. batch is either hunk's " +
			`agent-context v1 ({"version":1,"files":[{"path":…,"annotations":[{"newRange":[a,b],"summary":…}]}]}) ` +
			`or a comment batch ({"comments":[{"filePath":…,"newLine":N,"summary":…}]}). ` +
			"The whole batch is validated first: one bad item stores nothing. Unanchored top-level/file " +
			"summaries come back as contexts. " +
			`preview = "<target>...<source>" (or a saved preview's id/label) addresses a MERGE PREVIEW ` +
			"instead: the note is stored on the source tip, new side only; an old-side item is skipped " +
			"(reported in `skipped`/`warning`), not stored. MUTATES gg's note store.",
		Annotations:  mutatingAnnotations(),
		OutputSchema: wireNoteOutputSchema,
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in notesApplyIn) (*sdk.CallToolResult, notesOut, error) {
		out := notesOut{Repo: s.repoInfo(), Notes: []domain.WireNote{}}
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		raw, err := json.Marshal(in.Batch)
		if err != nil {
			return nil, out, fmt.Errorf("batch: %v", err)
		}
		batch, err := notebatch.Parse(raw)
		if err != nil {
			return nil, out, err
		}
		out.Contexts = batch.Contexts
		author := domain.NoteAuthorDefault(in.Author)
		// gg_notes_apply is the MCP door onto the same import gg note apply
		// --stdin uses: cached/rev pick one target for the whole batch, both
		// of whose sides are real (unlike a review's range/working target,
		// which is NoteSideNewOnly), so PlanNoteBatch/ApplyNoteBatch — the
		// shared, all-or-nothing planner/applier domain now owns — run with
		// NoteSideBoth. A preview's old side is the merge base, which no
		// stored address names, so it switches to NoteSideNewOnly, which
		// SKIPS old-side items rather than refusing the batch (spec §1.5,
		// parity with the CLI's `gg note apply --preview`, which warns to
		// stderr and keeps going). MCP has no stderr: the same warning text
		// (warnSkippedOldSide's preview wording) rides the reply instead.
		target := domain.NoteBatchTarget{Cached: in.Cached, Rev: in.Rev}
		rule := domain.NoteSideBoth
		if in.Preview != "" {
			set, perr := s.previewSet(ctx, in.noteTargetIn)
			if perr != nil {
				return nil, out, perr
			}
			spec := set.DiffSpec()
			target = domain.NoteBatchTarget{Rev: set.Tip, Hunks: &spec}
			rule = domain.NoteSideNewOnly
		}
		planned, skipped, err := s.svc.PlanNoteBatchIn(ctx, batch, target, author, rule)
		if err != nil {
			return nil, out, err
		}
		stored, err := s.svc.ApplyNoteBatch(ctx, planned)
		if err != nil {
			return nil, out, err
		}
		for _, n := range stored {
			out.Notes = append(out.Notes, domain.ToWireNote(domain.ResolvedNote{Note: n, Status: model.NoteActive, Range: n.Range}))
		}
		if skipped > 0 {
			out.Skipped = skipped
			out.Warning = fmt.Sprintf("skipped %d old-side annotation(s) — notes in a preview anchor on the new side", skipped)
		}
		// Only wake the window when there is something new to show: a batch
		// that stored nothing (contexts only) would otherwise leave a stray
		// wire command.
		if len(stored) > 0 {
			s.notifyNotesChanged()
		}
		return nil, out, nil
	})

	sdk.AddTool(srv, &sdk.Tool{
		Name:        "gg_note_rm",
		Description: "Remove one gg review note by id; a thread root takes its replies with it. MUTATES gg's note store.",
		Annotations: mutatingAnnotations(),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in noteRmIn) (*sdk.CallToolResult, okOut, error) {
		out := okOut{Repo: s.repoInfo()}
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		if in.ID == "" {
			return nil, out, fmt.Errorf("id is required")
		}
		if err := s.svc.NoteRemove(ctx, in.ID); err != nil {
			return nil, out, err
		}
		out.OK = true
		s.notifyNotesChanged()
		return nil, out, nil
	})
}

// previewSet resolves a request's preview target. It is mutually exclusive
// with cached/rev: two targets in one call is a caller error, never a silent
// precedence rule. A non-OK pair names its state word (missing source,
// missing target, merged, no base) exactly as the CLI's resolvePreviewTarget
// (internal/cli/previewflag.go) does, rather than one coarse "not
// previewable" message.
func (s *Server) previewSet(ctx context.Context, t noteTargetIn) (domain.PreviewNoteSet, error) {
	if t.Cached || t.Rev != "" {
		return domain.PreviewNoteSet{}, fmt.Errorf("one target only: preview cannot be combined with rev or cached")
	}
	source, target, err := s.svc.PreviewResolve(ctx, t.Preview)
	if err != nil {
		return domain.PreviewNoteSet{}, err
	}
	sum, err := s.svc.PreviewSummary(ctx, source, target)
	if err != nil {
		return domain.PreviewNoteSet{}, err
	}
	switch sum.State {
	case domain.PreviewOK:
	case domain.PreviewMissingSource:
		return domain.PreviewNoteSet{}, fmt.Errorf("preview: missing: %s", source)
	case domain.PreviewMissingTarget:
		return domain.PreviewNoteSet{}, fmt.Errorf("preview: missing: %s", target)
	default:
		return domain.PreviewNoteSet{}, fmt.Errorf("preview: %s → %s: %s", source, target, sum.State)
	}
	set, err := s.svc.PreviewNotes(ctx, source, target)
	if err != nil {
		return domain.PreviewNoteSet{}, err
	}
	if !set.OK() {
		return domain.PreviewNoteSet{}, fmt.Errorf("preview %s → %s is not previewable", source, target)
	}
	return set, nil
}

// previewNotesFor gathers a preview's notes for one path, or — with no path —
// for every path the preview carries notes on, in ONE store load
// (domain.PreviewNotesAll) rather than one PreviewNotesAt call per path. It
// mirrors the CLI's previewResolvedNotes (internal/cli/note.go): PreviewNotesAt
// needs a path to resolve against (it reads that file's content at the tip),
// so the no-path case walks PreviewNotesAll's map in PreviewNotePaths's
// stable (sorted) order.
func (s *Server) previewNotesFor(ctx context.Context, set domain.PreviewNoteSet, path string) ([]domain.ResolvedNote, error) {
	if path != "" {
		return s.svc.PreviewNotesAt(ctx, set, path)
	}
	byPath, err := s.svc.PreviewNotesAll(ctx, set)
	if err != nil {
		return nil, err
	}
	var out []domain.ResolvedNote
	for _, p := range domain.PreviewNotePaths(byPath) {
		out = append(out, byPath[p]...)
	}
	return out, nil
}

// noteTypeMatches applies the type filter. "all" (the default) keeps
// everything. A local copy of internal/cli's helper of the same name:
// internal/mcp must reach git/domain state only through internal/domain
// (archtest's layering DAG forbids mcp importing cli), and the check is four
// lines — not worth a domain round-trip just to share it.
func noteTypeMatches(want string, src model.NoteSource) bool {
	switch want {
	case "", "all":
		return true
	case "user":
		return src == model.NoteSourceUser
	case "agent":
		return src == model.NoteSourceAgent
	}
	return false
}

// notesFor resolves the notes a list request covers: one address when file is
// given, else every address this checkout can see. cached/rev only make sense
// against a single file target, so either without file is rejected rather
// than silently falling back to "every address" (a different question than
// the one asked).
func (s *Server) notesFor(ctx context.Context, t noteTargetIn) ([]domain.ResolvedNote, error) {
	if t.File != "" {
		addr, err := s.svc.NoteTarget(ctx, t.File, t.Cached, t.Rev)
		if err != nil {
			return nil, err
		}
		return s.svc.NotesAt(ctx, addr)
	}
	if t.Cached || t.Rev != "" {
		return nil, fmt.Errorf("cached/rev need file")
	}
	addrs, err := s.svc.NoteAddresses(ctx)
	if err != nil {
		return nil, err
	}
	var out []domain.ResolvedNote
	for _, a := range addrs {
		got, gerr := s.svc.NotesAt(ctx, a)
		if gerr != nil {
			return nil, gerr
		}
		out = append(out, got...)
	}
	return out, nil
}

// noteAnchor turns the three mutually exclusive anchor inputs into a side and
// range, resolving a hunk number over the same patch gg diff --hunks numbers.
func (s *Server) noteAnchor(ctx context.Context, addr model.FileAddress, in noteAddIn) (model.NoteSide, [2]int, error) {
	set := 0
	for _, v := range []int{in.Hunk, in.NewLine, in.OldLine} {
		if v != 0 {
			set++
		}
	}
	if set != 1 {
		return "", [2]int{}, fmt.Errorf("pass exactly one of hunk, new_line or old_line")
	}
	switch {
	case in.NewLine != 0:
		return model.NoteSideNew, [2]int{in.NewLine, in.NewLine}, nil
	case in.OldLine != 0:
		return model.NoteSideOld, [2]int{in.OldLine, in.OldLine}, nil
	default:
		spec, err := s.svc.HunkDiffSpec(ctx, in.Cached, in.Rev, []string{addr.Path})
		if err != nil {
			return "", [2]int{}, err
		}
		return s.svc.HunkRange(ctx, spec, addr.Path, in.Hunk)
	}
}
