package main

import (
	"path/filepath"
	"testing"

	"github.com/0xble/toolkit/toolkittest"
)

func TestConformance(t *testing.T) {
	out := filepath.Join(t.TempDir(), "notes.json")
	toolkittest.Run(t, toolkittest.Suite{
		New: func(testing.TB) toolkittest.Fixture {
			reg, s := New()
			return toolkittest.Fixture{Registry: reg, State: func() any { return s.Snapshot() }}
		},
		Options: Options(),
		Cases: map[string]toolkittest.Case{
			"notes.list": {Input: map[string]any{"limit": 2, "tag": "work"}, Args: []string{"notes", "list", "--limit", "2", "--tag", "work"}},
			"notes.sync": {Input: map[string]any{}, Args: []string{"notes", "sync"}},
			// --out is cli-only: the kit checks that HTTP and MCP refuse it.
			"notes.export": {Input: map[string]any{"out": out}, Args: []string{"notes", "export", "--out", out}},
			"note.get":     {Input: map[string]any{"id": "n2"}, Args: []string{"note", "n2", "show"}},
			"note.create":  {Input: map[string]any{"title": "Draft", "tags": []string{"a", "b"}}, Args: []string{"notes", "create", "Draft", "--tags", "a,b"}},
			"note.rename":  {Input: map[string]any{"id": "n1", "title": "Shopping"}, Args: []string{"note", "n1", "rename", "Shopping"}},
			"note.delete":  {Input: map[string]any{"id": "n3"}, Args: []string{"note", "n3", "delete"}},
		},
	})
}
