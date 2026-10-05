package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/0xble/toolkit/op"
	"github.com/0xble/toolkit/output"
)

// Globals are the sample's root flags. Each json name fills the operation
// input field with the same name, so `sample --limit 1 notes list` and
// `sample notes list --limit 1` are the same call.
type Globals struct {
	Limit  int    `json:"limit" help:"Maximum results per page"`
	Cursor string `json:"cursor" help:"Page cursor from a previous result"`
}

// Page is embedded by list inputs. Its fields are bound to the root flags.
type Page struct {
	Limit  int    `json:"limit,omitempty" default:"20" help:"Maximum results per page"`
	Cursor string `json:"cursor,omitempty" help:"Page cursor from a previous result"`
}

type ListInput struct {
	Page
	Tag string `json:"tag,omitempty" help:"Only notes with this tag"`
}

// NotesPage is one page of notes. notes.list declares it Paged, so
// `--fields id,title` keeps those keys of each item and the whole envelope.
type NotesPage struct {
	Items      []Note `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
}

type SyncInput struct {
	Fail int `json:"fail,omitempty" help:"Make the fake upstream answer with this HTTP status, such as 429 or 503"`
}

// Synced is the result of a sync with the upstream service.
type Synced struct {
	Pulled int `json:"pulled"`
}

// ExportInput's Out names a local file, so it is cli-only: HTTP and MCP
// neither show nor accept it, and their callers get the notes in the result.
type ExportInput struct {
	Out string `json:"out,omitempty" help:"Write the notes to this file instead (command line only)" toolkit:"cli-only"`
}

// Exported is the result of an export: the notes, or the file they went to.
type Exported struct {
	Count int    `json:"count"`
	Notes []Note `json:"notes,omitempty"`
	Path  string `json:"path,omitempty"`
}

type IDInput struct {
	ID string `json:"id" help:"Note ID"`
}

type CreateInput struct {
	Title string   `json:"title" arg:"" help:"Note title"`
	Body  string   `json:"body,omitempty" help:"Note body"`
	Tags  []string `json:"tags,omitempty" help:"Tags"`
}

type RenameInput struct {
	ID    string `json:"id" help:"Note ID"`
	Title string `json:"title" arg:"" help:"New title"`
}

// Change is the result of a write. Applied is false for a preview.
type Change struct {
	Applied bool `json:"applied"`
	Note    Note `json:"note"`
}

// Register declares the sample's operations on reg.
func Register(reg *op.Registry, s *Store) {
	op.Add(reg, op.Op[ListInput, NotesPage]{
		Name: "notes.list", Summary: "List notes", Effect: op.Read, MCP: true, Paged: true,
		Handler: func(_ context.Context, _ op.Request, in ListInput) (NotesPage, error) {
			start := 0
			if in.Cursor != "" {
				n, err := strconv.Atoi(in.Cursor)
				if err != nil || n < 0 {
					return NotesPage{}, op.Errorf(op.KindUsage, "invalid_cursor", "cursor %q is not valid", in.Cursor)
				}
				start = n
			}
			notes, next := s.List(in.Tag, start, in.Limit)
			p := NotesPage{Items: notes}
			if next > 0 {
				p.NextCursor, p.HasMore = strconv.Itoa(next), true
			}
			return p, nil
		},
		// The next-page hint repeats the caller's filters, so the render hook
		// reads them from the input rather than from hidden result fields.
		RenderWithInput: func(w io.Writer, in ListInput, p NotesPage) error {
			rows := make([][]string, 0, len(p.Items))
			for _, n := range p.Items {
				rows = append(rows, []string{n.ID, n.Title, strings.Join(n.Tags, ",")})
			}
			output.PrintTable(w, []string{"ID", "TITLE", "TAGS"}, rows)
			if !p.HasMore {
				return nil
			}
			hint := fmt.Sprintf("--limit %d --cursor %s", in.Limit, p.NextCursor)
			if in.Tag != "" {
				hint = "--tag " + in.Tag + " " + hint
			}
			_, err := fmt.Fprintf(w, "next page: sample notes list %s\n", hint)
			return err
		},
	})
	op.Add(reg, op.Op[SyncInput, Synced]{
		// A provider call: its failures carry the provider's status, request
		// ID, Retry-After and whether to retry, on every surface.
		Name: "notes.sync", Summary: "Pull notes from the upstream service", Effect: op.Read, MCP: true,
		Handler: func(_ context.Context, _ op.Request, in SyncInput) (Synced, error) {
			if in.Fail != 0 {
				return Synced{}, upstreamError(in.Fail)
			}
			return Synced{}, nil
		},
	})
	op.Add(reg, op.Op[ExportInput, Exported]{
		Name: "notes.export", Summary: "Export every note", Effect: op.Read, MCP: true,
		Handler: func(_ context.Context, _ op.Request, in ExportInput) (Exported, error) {
			notes := s.Snapshot()
			if in.Out == "" {
				return Exported{Count: len(notes), Notes: notes}, nil
			}
			b, err := json.MarshalIndent(notes, "", "  ")
			if err == nil {
				err = os.WriteFile(in.Out, append(b, '\n'), 0o600)
			}
			if err != nil {
				return Exported{}, op.Errorf(op.KindError, "export_failed", "write %s: %v", in.Out, err)
			}
			return Exported{Count: len(notes), Path: in.Out}, nil
		},
	})
	op.Add(reg, op.Op[IDInput, Note]{
		Name: "note.get", CLI: "note <id> show", Summary: "Show one note", Effect: op.Read, MCP: true,
		Handler: func(_ context.Context, _ op.Request, in IDInput) (Note, error) {
			return s.Get(in.ID)
		},
		Render: func(w io.Writer, n Note) error {
			_, err := fmt.Fprintf(w, "%s  %s\n%s\n", n.ID, n.Title, n.Body)
			return err
		},
	})
	op.Add(reg, op.Op[CreateInput, Change]{
		// A clearly scoped write: the CLI creates without --apply and
		// previews with --dry-run. HTTP and MCP still need "apply": true.
		Name: "note.create", CLI: "notes create", Summary: "Create a note", Effect: op.Write, MCP: true, CLIImmediate: true,
		Handler: func(_ context.Context, req op.Request, in CreateInput) (Change, error) {
			if strings.TrimSpace(in.Title) == "" {
				return Change{}, op.Errorf(op.KindUsage, "invalid_title", "title must not be empty")
			}
			n := Note{Title: in.Title, Body: in.Body, Tags: in.Tags}
			if !req.Apply {
				return Change{Note: n}, nil
			}
			return Change{Applied: true, Note: s.Create(n)}, nil
		},
		Render: renderChange("create", "drop --dry-run to create"),
	})
	op.Add(reg, op.Op[RenameInput, Change]{
		// Not an MCP tool: shows that MCP exposure is opt-in per operation.
		Name: "note.rename", CLI: "note <id> rename", Summary: "Rename a note", Effect: op.Write,
		Handler: func(_ context.Context, req op.Request, in RenameInput) (Change, error) {
			n, err := s.Get(in.ID)
			if err != nil {
				return Change{}, err
			}
			n.Title = in.Title
			if !req.Apply {
				return Change{Note: n}, nil
			}
			n, err = s.Rename(in.ID, in.Title)
			return Change{Applied: err == nil, Note: n}, err
		},
		Render: renderChange("rename", "pass --apply to rename"),
	})
	op.Add(reg, op.Op[IDInput, Change]{
		Name: "note.delete", CLI: "note <id> delete", Summary: "Delete a note permanently", Effect: op.Destructive, MCP: true,
		Handler: func(_ context.Context, req op.Request, in IDInput) (Change, error) {
			n, err := s.Get(in.ID)
			if err != nil || !req.Apply {
				return Change{Note: n}, err
			}
			n, err = s.Delete(in.ID)
			return Change{Applied: err == nil, Note: n}, err
		},
		Render: renderChange("delete", "pass --apply --yes to delete"),
	})
}

func renderChange(verb, hint string) func(io.Writer, Change) error {
	return func(w io.Writer, c Change) error {
		what := strings.TrimSpace(c.Note.ID + " " + strconv.Quote(c.Note.Title))
		if !c.Applied {
			_, err := fmt.Fprintf(w, "would %s %s (preview; %s)\n", verb, what, hint)
			return err
		}
		_, err := fmt.Fprintf(w, "%sd %s\n", verb, what)
		return err
	}
}
