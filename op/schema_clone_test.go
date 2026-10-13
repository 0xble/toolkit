package op

import (
	"bytes"
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

func schemaLeaf() *jsonschema.Schema { return &jsonschema.Schema{Type: "string"} }

func schemaWithReferences() *jsonschema.Schema {
	constValue := any(map[string]any{"const": "original"})
	multipleOf, minimum, maximum := 1.5, 2.5, 3.5
	exclusiveMinimum, exclusiveMaximum := 4.5, 5.5
	minLength, maxLength := 1, 2
	minItems, maxItems := 3, 4
	minContains, maxContains := 5, 6
	minProperties, maxProperties := 7, 8
	return &jsonschema.Schema{
		Defs:                  map[string]*jsonschema.Schema{"def": schemaLeaf()},
		Definitions:           map[string]*jsonschema.Schema{"definition": schemaLeaf()},
		DependencySchemas:     map[string]*jsonschema.Schema{"schema": schemaLeaf()},
		DependencyStrings:     map[string][]string{"strings": {"one", "two"}},
		Vocabulary:            map[string]bool{"https://example.com": true},
		Default:               json.RawMessage(`{"default":["original"]}`),
		Examples:              []any{map[string]any{"nested": []any{"original"}}},
		Types:                 []string{"object"},
		Enum:                  []any{map[string]any{"enum": "original"}},
		Const:                 &constValue,
		MultipleOf:            &multipleOf,
		Minimum:               &minimum,
		Maximum:               &maximum,
		ExclusiveMinimum:      &exclusiveMinimum,
		ExclusiveMaximum:      &exclusiveMaximum,
		MinLength:             &minLength,
		MaxLength:             &maxLength,
		PrefixItems:           []*jsonschema.Schema{schemaLeaf()},
		Items:                 schemaLeaf(),
		ItemsArray:            []*jsonschema.Schema{schemaLeaf()},
		MinItems:              &minItems,
		MaxItems:              &maxItems,
		AdditionalItems:       schemaLeaf(),
		Contains:              schemaLeaf(),
		MinContains:           &minContains,
		MaxContains:           &maxContains,
		UnevaluatedItems:      schemaLeaf(),
		MinProperties:         &minProperties,
		MaxProperties:         &maxProperties,
		Required:              []string{"required"},
		DependentRequired:     map[string][]string{"dependent": {"required"}},
		Properties:            map[string]*jsonschema.Schema{"property": schemaLeaf()},
		PatternProperties:     map[string]*jsonschema.Schema{"^x": schemaLeaf()},
		AdditionalProperties:  schemaLeaf(),
		PropertyNames:         schemaLeaf(),
		UnevaluatedProperties: schemaLeaf(),
		AllOf:                 []*jsonschema.Schema{schemaLeaf()},
		AnyOf:                 []*jsonschema.Schema{schemaLeaf()},
		OneOf:                 []*jsonschema.Schema{schemaLeaf()},
		Not:                   schemaLeaf(),
		If:                    schemaLeaf(),
		Then:                  schemaLeaf(),
		Else:                  schemaLeaf(),
		DependentSchemas:      map[string]*jsonschema.Schema{"dependent": schemaLeaf()},
		ContentSchema:         schemaLeaf(),
		Extra:                 map[string]any{"extra": map[string]any{"nested": []any{"original"}}},
		PropertyOrder:         []string{"property"},
	}
}

func mutateSchemaReferences(s *jsonschema.Schema) {
	s.Defs["def"].Type = "changed"
	s.Definitions["definition"].Type = "changed"
	s.DependencySchemas["schema"].Type = "changed"
	s.DependencyStrings["strings"][0] = "changed"
	s.Vocabulary["https://example.com"] = false
	s.Default[0] = 'x'
	s.Examples[0].(map[string]any)["nested"].([]any)[0] = "changed"
	s.Types[0] = "changed"
	s.Enum[0].(map[string]any)["enum"] = "changed"
	s.Const = func() *any { v := any("changed"); return &v }()
	*s.MultipleOf, *s.Minimum, *s.Maximum = 11, 12, 13
	*s.ExclusiveMinimum, *s.ExclusiveMaximum = 14, 15
	*s.MinLength, *s.MaxLength = 16, 17
	s.PrefixItems[0].Type = "changed"
	s.Items.Type = "changed"
	s.ItemsArray[0].Type = "changed"
	*s.MinItems, *s.MaxItems = 18, 19
	s.AdditionalItems.Type = "changed"
	s.Contains.Type = "changed"
	*s.MinContains, *s.MaxContains = 20, 21
	s.UnevaluatedItems.Type = "changed"
	*s.MinProperties, *s.MaxProperties = 22, 23
	s.Required = slices.DeleteFunc(s.Required, func(string) bool { return true })
	s.DependentRequired["dependent"][0] = "changed"
	s.Properties["property"].Type = "changed"
	s.PatternProperties["^x"].Type = "changed"
	s.AdditionalProperties.Type = "changed"
	s.PropertyNames.Type = "changed"
	s.UnevaluatedProperties.Type = "changed"
	s.AllOf[0].Type = "changed"
	s.AnyOf[0].Type = "changed"
	s.OneOf[0].Type = "changed"
	s.Not.Type = "changed"
	s.If.Type = "changed"
	s.Then.Type = "changed"
	s.Else.Type = "changed"
	s.DependentSchemas["dependent"].Type = "changed"
	s.ContentSchema.Type = "changed"
	s.Extra["extra"].(map[string]any)["nested"].([]any)[0] = "changed"
	s.PropertyOrder[0] = "changed"
}

func TestCloneSchemaDeepCopyIndependence(t *testing.T) {
	original := schemaWithReferences()
	want := schemaWithReferences()
	copy := cloneSchema(original)

	mutateSchemaReferences(copy)

	if !reflect.DeepEqual(original, want) {
		t.Fatalf("mutating a cloned schema changed the original:\n got: %#v\nwant: %#v", original, want)
	}
}

func schemaForJSONEquivalence() *jsonschema.Schema {
	multipleOf, minItems := 1.5, 2
	return &jsonschema.Schema{
		ID:                "https://example.com/schema",
		Defs:              map[string]*jsonschema.Schema{"child": {Type: "string"}},
		Vocabulary:        map[string]bool{"https://example.com/vocab": true},
		Title:             "Representative",
		Description:       "A schema with nested and non-schema JSON values",
		Default:           json.RawMessage(`{"name":"default"}`),
		Examples:          []any{map[string]any{"name": "example"}},
		Types:             []string{"object"},
		Enum:              []any{map[string]any{"name": "one"}},
		MultipleOf:        &multipleOf,
		PrefixItems:       []*jsonschema.Schema{{Type: "string"}},
		Items:             &jsonschema.Schema{Type: "integer"},
		MinItems:          &minItems,
		Required:          []string{"name"},
		DependentRequired: map[string][]string{"name": {"id"}},
		Properties: map[string]*jsonschema.Schema{
			"name": {Type: "string"},
			"id":   {Type: "integer"},
		},
		PatternProperties:    map[string]*jsonschema.Schema{"^x-": {Type: "string"}},
		AdditionalProperties: &jsonschema.Schema{Type: "boolean"},
		AllOf:                []*jsonschema.Schema{{Type: "object"}},
		AnyOf:                []*jsonschema.Schema{{Type: "null"}},
		Not:                  &jsonschema.Schema{Type: "number"},
		DependentSchemas:     map[string]*jsonschema.Schema{"name": {Type: "object"}},
		ContentSchema:        &jsonschema.Schema{Type: "string"},
		Extra:                map[string]any{"x-extra": []any{"value"}},
		PropertyOrder:        []string{"name", "id"},
	}
}

func TestCloneSchemaJSONIsByteIdentical(t *testing.T) {
	original := schemaForJSONEquivalence()
	copy := cloneSchema(original)
	want, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(copy)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("clone JSON differs:\n got: %s\nwant: %s", got, want)
	}
}
