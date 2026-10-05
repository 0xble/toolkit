// Command sample is a tiny fake tool built on toolkit. It keeps notes in
// memory and declares one read, write and destructive operation of each
// shape, so it doubles as the toolkit's end-to-end fixture.
//
//	sample notes list --limit 2
//	sample notes create Draft          # creates: CLIImmediate
//	sample notes create Draft --dry-run
//	sample note n1 delete              # preview
//	sample note n1 delete --apply --yes
//	sample serve --socket /tmp/sample.sock
//	sample mcp
//	sample metadata --json
package main

import (
	"github.com/0xble/toolkit"
	"github.com/0xble/toolkit/op"
)

var version = "dev"

// New returns the sample's registry over a fresh store.
func New() (*op.Registry, *Store) {
	s := NewStore()
	reg := op.New("sample", version)
	Register(reg, s)
	return reg, s
}

// Options are the sample's toolkit options.
func Options() toolkit.Options {
	return toolkit.Options{Description: "A sample tool that keeps notes in memory.", Globals: &Globals{}}
}

func main() {
	reg, _ := New()
	toolkit.Main(reg, Options())
}
