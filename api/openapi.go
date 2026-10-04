package api

import (
	"github.com/google/jsonschema-go/jsonschema"

	"github.com/0xble/toolkit/op"
)

// OpenAPI returns the OpenAPI 3.1 document for reg. Request and response
// schemas are the registry's own JSON Schemas, inlined, so the document and
// the metadata cannot drift. Each operation's id is its MCP name, and its
// effect is in the x-effect extension.
func OpenAPI(reg *op.Registry) map[string]any {
	errRef := map[string]any{"$ref": "#/components/responses/Error"}
	paths := map[string]any{
		"/ops": map[string]any{"get": map[string]any{
			"operationId": "toolkit_metadata",
			"summary":     "Describe every operation (" + op.MetadataSchemaID + ")",
			"responses": map[string]any{"200": map[string]any{
				"description": "Registry metadata",
				"content":     jsonContent(map[string]any{"type": "object"}),
			}},
		}},
	}
	for _, e := range reg.Entries() {
		paths["/ops/"+e.Name] = map[string]any{"post": map[string]any{
			"operationId": e.MCPName(),
			"summary":     e.Summary,
			"x-effect":    e.Effect,
			"x-cli":       e.CLI(),
			"requestBody": map[string]any{"required": true, "content": jsonContent(e.InputSchema())},
			"responses": map[string]any{
				"200":     map[string]any{"description": "Operation output", "content": jsonContent(e.OutputSchema())},
				"default": errRef,
			},
		}}
	}
	return map[string]any{
		"openapi": "3.1.0",
		"info":    map[string]any{"title": reg.Tool, "version": reg.Version},
		"paths":   paths,
		"components": map[string]any{"responses": map[string]any{"Error": map[string]any{
			"description": "Operation error. The status follows the error kind.",
			"content":     jsonContent(errorSchema()),
		}}},
	}
}

func jsonContent(schema any) map[string]any {
	return map[string]any{"application/json": map[string]any{"schema": schema}}
}

func errorSchema() *jsonschema.Schema {
	str := &jsonschema.Schema{Type: "string"}
	return &jsonschema.Schema{
		Type:     "object",
		Required: []string{"error"},
		Properties: map[string]*jsonschema.Schema{"error": {
			Type:     "object",
			Required: []string{"code", "message"},
			Properties: map[string]*jsonschema.Schema{
				"code":        str,
				"message":     str,
				"suggestions": {Type: "array", Items: str},
			},
		}},
	}
}
