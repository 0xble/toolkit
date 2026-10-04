package op

import (
	_ "embed"

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
	Name    string             `json:"name"`
	CLI     string             `json:"cli"`
	Summary string             `json:"summary"`
	Effect  Effect             `json:"effect"`
	MCP     bool               `json:"mcp"`
	MCPName string             `json:"mcp_name"`
	Input   *jsonschema.Schema `json:"input"`
	Output  *jsonschema.Schema `json:"output"`
}

// Metadata describes every operation, sorted by name.
func (r *Registry) Metadata() Metadata {
	m := Metadata{Schema: MetadataSchemaID, Tool: r.Tool, Version: r.Version, Operations: []OperationInfo{}}
	for _, e := range r.Entries() {
		m.Operations = append(m.Operations, OperationInfo{
			Name: e.Name, CLI: joinPath(e.CLIPath), Summary: e.Summary, Effect: e.Effect,
			MCP: e.MCP, MCPName: e.MCPName(), Input: e.InputSchema(), Output: e.OutputSchema(),
		})
	}
	return m
}

func joinPath(p []string) string {
	s := ""
	for i, w := range p {
		if i > 0 {
			s += " "
		}
		s += w
	}
	return s
}
