package main

import (
	"slices"
	"strconv"
	"sync"

	"github.com/0xble/toolkit/op"
)

// Note is one note in the in-memory backend.
type Note struct {
	ID    string   `json:"id"`
	Title string   `json:"title"`
	Body  string   `json:"body,omitempty"`
	Tags  []string `json:"tags,omitempty"`
}

// Store is a fake backend. It starts with three notes and lives as long as
// the process, so state persists across calls to one `sample serve` but not
// across CLI runs.
type Store struct {
	mu    sync.Mutex
	notes []Note
	next  int
}

// NewStore returns a store with the seed notes.
func NewStore() *Store {
	return &Store{next: 4, notes: []Note{
		{ID: "n1", Title: "Groceries", Body: "eggs, rice", Tags: []string{"home"}},
		{ID: "n2", Title: "Standup", Body: "ship the release", Tags: []string{"work"}},
		{ID: "n3", Title: "Ideas"},
	}}
}

// Snapshot returns a copy of every note, for tests.
func (s *Store) Snapshot() []Note {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.notes)
}

// List returns up to limit notes with the tag from offset start, and the
// offset of the next page or 0.
func (s *Store) List(tag string, start, limit int) ([]Note, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var match []Note
	for _, n := range s.notes {
		if tag == "" || slices.Contains(n.Tags, tag) {
			match = append(match, n)
		}
	}
	if start > len(match) {
		start = len(match)
	}
	end := len(match)
	if limit > 0 && start+limit < end {
		end = start + limit
	}
	out := append([]Note{}, match[start:end]...)
	if end < len(match) {
		return out, end
	}
	return out, 0
}

func (s *Store) Get(id string) (Note, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.find(id)
	if i < 0 {
		return Note{}, notFound(id)
	}
	return s.notes[i], nil
}

func (s *Store) Create(n Note) Note {
	s.mu.Lock()
	defer s.mu.Unlock()
	n.ID = "n" + strconv.Itoa(s.next)
	s.next++
	s.notes = append(s.notes, n)
	return n
}

func (s *Store) Rename(id, title string) (Note, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.find(id)
	if i < 0 {
		return Note{}, notFound(id)
	}
	s.notes[i].Title = title
	return s.notes[i], nil
}

func (s *Store) Delete(id string) (Note, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.find(id)
	if i < 0 {
		return Note{}, notFound(id)
	}
	n := s.notes[i]
	s.notes = slices.Delete(s.notes, i, i+1)
	return n, nil
}

func (s *Store) find(id string) int {
	return slices.IndexFunc(s.notes, func(n Note) bool { return n.ID == id })
}

func notFound(id string) error {
	return &op.Error{Kind: op.KindNotFound, Code: "note_not_found", Message: "no note " + id,
		Suggestions: []string{"run: sample notes list"}}
}
