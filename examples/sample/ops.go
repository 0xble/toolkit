package main

import (
	"context"
	"fmt"
	"io"
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

type NotesPage struct {
	Notes []Note `json:"notes"`
	Next  string `json:"next,omitempty"`
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
		Name: "notes.list", Summary: "List notes", Effect: op.Read, MCP: true,
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
			p := NotesPage{Notes: notes}
			if next > 0 {
				p.Next = strconv.Itoa(next)
			}
			return p, nil
		},
		Render: func(w io.Writer, p NotesPage) error {
			rows := make([][]string, 0, len(p.Notes))
			for _, n := range p.Notes {
				rows = append(rows, []string{n.ID, n.Title, strings.Join(n.Tags, ",")})
			}
			output.PrintTable(w, []string{"ID", "TITLE", "TAGS"}, rows)
			if p.Next != "" {
				_, err := fmt.Fprintf(w, "next page: --cursor %s\n", p.Next)
				return err
			}
			return nil
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
		Name: "note.create", CLI: "notes create", Summary: "Create a note", Effect: op.Write, MCP: true,
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
		Render: renderChange("create"),
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
		Render: renderChange("rename"),
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
		Render: renderChange("delete"),
	})
}

func renderChange(verb string) func(io.Writer, Change) error {
	return func(w io.Writer, c Change) error {
		what := strings.TrimSpace(c.Note.ID + " " + strconv.Quote(c.Note.Title))
		if !c.Applied {
			_, err := fmt.Fprintf(w, "would %s %s (preview; pass --apply to %s)\n", verb, what, verb)
			return err
		}
		_, err := fmt.Fprintf(w, "%sd %s\n", verb, what)
		return err
	}
}
