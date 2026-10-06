package op

import (
	_ "embed"
	"slices"

	"github.com/google/jsonschema-go/jsonschema"
)

// MetadataSchemaID is the schema identifier of the metadata document.
const MetadataSchemaID = "toolkit.metadata.v1"

//go:embed metadata.schema.json
var metadataSchema []byte

// MetadataSchema returns the JSON Schema that every metadata document
// validates against.
func MetadataSchema() []byte { return append([]byte(nil), metadataSchema...) }

// Metadata is the registry dump that `<tool> metadata --json` prints and
// `GET /ops` serves.
type Metadata struct {
	Schema     string          `json:"schema"`
	Tool       string          `json:"tool"`
	Version    string          `json:"version"`
	Operations []OperationInfo `json:"operations"`
}

// OperationInfo describes one operation in the metadata document.
type OperationInfo struct {
	Name    string   `json:"name"`
	CLI     string   `json:"cli"`
	Aliases []string `json:"aliases,omitempty"`
	Summary string   `json:"summary"`
	Effect  Effect   `json:"effect"`
	MCP     bool     `json:"mcp"`
	MCPName string   `json:"mcp_name"`
	// CLIImmediate is Op.CLIImmediate: the CLI applies without --apply and
	// previews with --dry-run. HTTP and MCP still need apply.
	CLIImmediate bool `json:"cli_immediate,omitempty"`
	// CLIConfirmed is Op.CLIConfirmed: the CLI applies a destructive
	// operation without --yes. HTTP and MCP still need apply and confirm.
	CLIConfirmed bool `json:"cli_confirmed,omitempty"`
	// CLIOnlyInputs is Entry.CLIOnlyInputs: input fields accepted only on
	// the CLI. Input omits them, as the HTTP and MCP schemas do.
	CLIOnlyInputs []string           `json:"cli_only_inputs,omitempty"`
	Input         *jsonschema.Schema `json:"input"`
	Output        *jsonschema.Schema `json:"output"`
}

// Metadata describes every operation, sorted by name.
func (r *Registry) Metadata() Metadata {
	m := Metadata{Schema: MetadataSchemaID, Tool: r.Tool, Version: r.Version, Operations: []OperationInfo{}}
	for _, e := range r.Entries() {
		m.Operations = append(m.Operations, OperationInfo{
			Name: e.Name, CLI: e.CLI(), Aliases: slices.Clone(e.Aliases), Summary: e.Summary, Effect: e.Effect,
			MCP: e.MCP, MCPName: e.MCPName(), CLIImmediate: e.CLIImmediate, CLIConfirmed: e.CLIConfirmed, CLIOnlyInputs: slices.Clone(e.CLIOnlyInputs),
			Input: e.InputSchema(), Output: e.OutputSchema(),
		})
	}
	return m
}
