// Package toolkittest is the conformance kit a tool runs from its own tests:
//
//	func TestConformance(t *testing.T) {
//		toolkittest.Run(t, toolkittest.Suite{
//			New: func(t testing.TB) toolkittest.Fixture {
//				reg, store := newRegistry()
//				return toolkittest.Fixture{Registry: reg, State: store.Snapshot}
//			},
//			Options: options(),
//			Cases: map[string]toolkittest.Case{
//				"note.delete": {Input: map[string]any{"id": "n1"}, Args: []string{"note", "n1", "delete"}},
//			},
//		})
//	}
//
// It checks that the metadata is schema-valid, that MCP tools/list and the
// OpenAPI document match the registry, that every surface returns the same
// output for the same input, that destructive operations refuse to apply
// without confirm on the CLI, HTTP and MCP, and that writes only preview
// without apply. An operation with CLIImmediate is checked to apply on the
// CLI without --apply and preview with --dry-run, while HTTP and MCP still
// preview without apply. All calls are in-process, with no network or
// credentials.
package toolkittest

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/0xble/toolkit"
	"github.com/0xble/toolkit/cli"
	"github.com/0xble/toolkit/mcp"
	"github.com/0xble/toolkit/op"
)

// Fixture is one fresh registry over a fresh fake backend.
type Fixture struct {
	Registry *op.Registry
	// State returns a JSON-encodable snapshot of the backend. The kit compares
	// snapshots to prove that previews and refused calls change nothing, and
	// that an applied call does change something.
	State func() any
}

// Case is one call of an operation, expressed for every surface.
type Case struct {
	// Input is the wire input without apply and confirm.
	Input map[string]any
	// Args select the same call on the CLI, without --apply, --dry-run, --yes
	// or --json.
	Args []string
}

// Suite describes a tool for Run.
type Suite struct {
	// New returns a fresh fixture. Each check gets its own.
	New func(t testing.TB) Fixture
	// Options are the tool's toolkit options. Authorizer is ignored: the kit
	// tests the default and an allow-all authorizer explicitly.
	Options toolkit.Options
	// Cases maps operation names to a call. Every write and destructive
	// operation needs one. A read case adds a parity check.
	Cases map[string]Case
}

// Run runs every conformance check as a subtest.
func Run(t *testing.T, s Suite) {
	t.Run("cli_tree", func(t *testing.T) {
		if err := cli.Validate(s.New(t).Registry, toolkit.CLIOptions(s.Options)); err != nil {
			t.Fatalf("kong rejects the command tree: %v", err)
		}
	})
	t.Run("metadata", func(t *testing.T) {
		reg := s.New(t).Registry
		b, err := json.Marshal(reg.Metadata())
		if err != nil {
			t.Fatal(err)
		}
		CheckMetadata(t, b)
		code, out, _ := s.cli(t, reg, "metadata", "--json")
		if code != 0 {
			t.Fatalf("metadata --json exited %d", code)
		}
		assertSameJSON(t, "metadata --json", b, out)
		status, body := httpCall(t, reg, nil, http.MethodGet, "/ops", nil)
		if status != http.StatusOK {
			t.Fatalf("GET /ops: status %d", status)
		}
		assertSameJSON(t, "GET /ops", b, body)
	})
	t.Run("openapi", func(t *testing.T) {
		reg := s.New(t).Registry
		status, body := httpCall(t, reg, nil, http.MethodGet, "/openapi.json", nil)
		if status != http.StatusOK {
			t.Fatalf("GET /openapi.json: status %d", status)
		}
		CheckOpenAPI(t, reg, body)
	})
	t.Run("mcp_tools", func(t *testing.T) {
		reg := s.New(t).Registry
		cs := MCPClient(t, reg, op.AllowAll)
		res, err := cs.ListTools(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		CheckMCPTools(t, reg, res.Tools)
	})
	t.Run("cases", func(t *testing.T) { s.checkCoverage(t) })
	for _, e := range s.New(t).Registry.Entries() {
		c, ok := s.Cases[e.Name]
		if !ok {
			continue
		}
		t.Run(e.Name, func(t *testing.T) { s.checkCase(t, e.Name, c) })
	}
}

func (s Suite) checkCoverage(t *testing.T) {
	reg := s.New(t).Registry
	for name := range s.Cases {
		if reg.Lookup(name) == nil {
			t.Errorf("case %s: no such operation", name)
		}
	}
	for _, e := range reg.Entries() {
		if _, ok := s.Cases[e.Name]; e.Effect.Mutates() && !ok {
			t.Errorf("%s is %s and has no conformance case", e.Name, e.Effect)
		}
	}
}

// checkCase runs one case's preview on every surface, checks the outputs
// agree and the state is unchanged, then checks the apply rules. The CLI
// previews without --apply, or with --dry-run for an operation with
// CLIImmediate.
func (s Suite) checkCase(t *testing.T, name string, c Case) {
	fx := s.New(t)
	e := fx.Registry.Lookup(name)
	before := snapshot(t, fx)

	preview := append(slices.Clone(c.Args), "--agent")
	if e.CLIImmediate {
		preview = append(preview, "--dry-run")
	}
	code, cliOut, stderr := s.cli(t, fx.Registry, preview...)
	if code != 0 {
		t.Fatalf("CLI %v: exit %d: %s", preview, code, stderr)
	}
	status, httpOut := httpCall(t, fx.Registry, op.AllowAll, http.MethodPost, "/ops/"+name, input(c, nil))
	if status != http.StatusOK {
		t.Fatalf("HTTP %s: status %d: %s", name, status, httpOut)
	}
	assertSameJSON(t, "HTTP output vs CLI output", cliOut, httpOut)
	if e.MCP {
		res := mcpCall(t, fx.Registry, op.AllowAll, e, input(c, nil))
		if res.IsError {
			t.Fatalf("MCP %s: %s", e.MCPName(), text(res))
		}
		assertSameJSON(t, "MCP output vs CLI output", cliOut, []byte(text(res)))
	}
	if !e.Effect.Mutates() {
		assertState(t, fx, before, false, "read")
		return
	}
	what := "preview without apply"
	if e.CLIImmediate {
		what = "CLI --dry-run, and HTTP and MCP without apply,"
	}
	assertState(t, fx, before, false, what)

	if e.Effect == op.Destructive {
		s.checkConfirm(t, fx, e, c, before)
	}
	s.checkDefaultAuthorizer(t, fx, e, c, before)

	args, what := applyArgs(e, c), "CLI --apply"
	if e.CLIImmediate {
		what = "CLI without --apply (CLIImmediate)"
	}
	if e.Effect == op.Destructive {
		args = append(args, "--yes")
	}
	if code, _, stderr := s.cli(t, fx.Registry, args...); code != 0 {
		t.Fatalf("CLI %v: exit %d: %s", args, code, stderr)
	}
	assertState(t, fx, before, true, what)
}

// applyArgs are the case's CLI args that apply, without --yes: --apply by
// default, nothing more with CLIImmediate.
func applyArgs(e *op.Entry, c Case) []string {
	args := append(slices.Clone(c.Args), "--agent")
	if !e.CLIImmediate {
		args = append(args, "--apply")
	}
	return args
}

// checkConfirm checks that a destructive apply without confirm is refused,
// with the same error, on every surface.
func (s Suite) checkConfirm(t *testing.T, fx Fixture, e *op.Entry, c Case, before []byte) {
	args := applyArgs(e, c)
	code, _, stderr := s.cli(t, fx.Registry, args...)
	if code != op.KindUsage.ExitCode() || errCode(stderr) != "confirmation_required" {
		t.Errorf("CLI %v without --yes: exit %d, %s; want exit 2 and confirmation_required", args, code, stderr)
	}
	for _, extra := range []map[string]any{{"apply": true}, {"apply": true, "confirm": false}} {
		status, body := httpCall(t, fx.Registry, op.AllowAll, http.MethodPost, "/ops/"+e.Name, input(c, extra))
		if status != http.StatusBadRequest || errCode(body) != "confirmation_required" {
			t.Errorf("HTTP %v: status %d, %s; want 400 and confirmation_required", extra, status, body)
		}
		if e.MCP {
			res := mcpCall(t, fx.Registry, op.AllowAll, e, input(c, extra))
			if !res.IsError || errCode([]byte(text(res))) != "confirmation_required" {
				t.Errorf("MCP %v: %s; want isError and confirmation_required", extra, text(res))
			}
		}
	}
	assertState(t, fx, before, false, "destructive apply without confirm")
}

// checkDefaultAuthorizer checks that the served default refuses to apply.
func (s Suite) checkDefaultAuthorizer(t *testing.T, fx Fixture, e *op.Entry, c Case, before []byte) {
	extra := map[string]any{"apply": true}
	if e.Effect == op.Destructive {
		extra["confirm"] = true
	}
	status, body := httpCall(t, fx.Registry, nil, http.MethodPost, "/ops/"+e.Name, input(c, extra))
	if status != http.StatusForbidden || errCode(body) != "write_not_authorized" {
		t.Errorf("HTTP apply with the default authorizer: status %d, %s; want 403 and write_not_authorized", status, body)
	}
	if e.MCP {
		res := mcpCall(t, fx.Registry, nil, e, input(c, extra))
		if !res.IsError || errCode([]byte(text(res))) != "write_not_authorized" {
			t.Errorf("MCP apply with the default authorizer: %s; want isError and write_not_authorized", text(res))
		}
	}
	assertState(t, fx, before, false, "apply refused by the default authorizer")
}

// CheckMetadata validates a metadata document against toolkit.metadata.v1.
func CheckMetadata(t testing.TB, doc []byte) {
	t.Helper()
	var s jsonschema.Schema
	if err := json.Unmarshal(op.MetadataSchema(), &s); err != nil {
		t.Fatal(err)
	}
	rs, err := s.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(doc, &v); err != nil {
		t.Fatalf("metadata is not JSON: %v", err)
	}
	if err := rs.Validate(v); err != nil {
		t.Fatalf("metadata does not match %s: %v", op.MetadataSchemaID, err)
	}
	var m op.Metadata
	if err := json.Unmarshal(doc, &m); err != nil {
		t.Fatal(err)
	}
	for _, o := range m.Operations {
		if _, err := o.Input.Resolve(nil); err != nil {
			t.Errorf("%s: input schema does not resolve: %v", o.Name, err)
		}
		if o.Input.Type != "object" {
			t.Errorf("%s: input schema type is %q, want object", o.Name, o.Input.Type)
		}
	}
}

// CheckOpenAPI checks that an OpenAPI document has exactly one POST path
// per operation, with the registry's input and output schemas, effect and
// CLIImmediate.
func CheckOpenAPI(t testing.TB, reg *op.Registry, doc []byte) {
	t.Helper()
	var d struct {
		OpenAPI string `json:"openapi"`
		Paths   map[string]struct {
			Post *struct {
				OperationID  string          `json:"operationId"`
				Effect       op.Effect       `json:"x-effect"`
				CLIImmediate bool            `json:"x-cli-immediate"`
				RequestBody  json.RawMessage `json:"requestBody"`
				Responses    json.RawMessage `json:"responses"`
			} `json:"post"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(doc, &d); err != nil {
		t.Fatalf("OpenAPI document: %v", err)
	}
	if !strings.HasPrefix(d.OpenAPI, "3.1.") {
		t.Errorf("openapi is %q, want 3.1.x", d.OpenAPI)
	}
	ops := 0
	for path, item := range d.Paths {
		if item.Post == nil {
			continue
		}
		ops++
		e := reg.Lookup(strings.TrimPrefix(path, "/ops/"))
		if e == nil {
			t.Errorf("OpenAPI path %s is not an operation", path)
			continue
		}
		p := item.Post
		if p.OperationID != e.MCPName() || p.Effect != e.Effect || p.CLIImmediate != e.CLIImmediate {
			t.Errorf("%s: operationId %q effect %q x-cli-immediate %v, want %q %q %v", path,
				p.OperationID, p.Effect, p.CLIImmediate, e.MCPName(), e.Effect, e.CLIImmediate)
		}
		var body struct {
			Content map[string]struct {
				Schema json.RawMessage `json:"schema"`
			} `json:"content"`
		}
		_ = json.Unmarshal(p.RequestBody, &body)
		assertSameJSON(t, path+" request schema", mustJSON(t, e.InputSchema()), body.Content["application/json"].Schema)
		var resp map[string]struct {
			Content map[string]struct {
				Schema json.RawMessage `json:"schema"`
			} `json:"content"`
		}
		_ = json.Unmarshal(p.Responses, &resp)
		assertSameJSON(t, path+" response schema", mustJSON(t, e.OutputSchema()), resp["200"].Content["application/json"].Schema)
	}
	if n := len(reg.Entries()); ops != n {
		t.Errorf("OpenAPI has %d operations, registry has %d", ops, n)
	}
}

// CheckMCPTools checks that tools, from any MCP transport, are exactly the
// registry's MCP operations, with the registry's input schemas, effect
// annotations and CLIImmediate _meta.
func CheckMCPTools(t testing.TB, reg *op.Registry, tools []*sdk.Tool) {
	t.Helper()
	got := map[string]*sdk.Tool{}
	for _, tool := range tools {
		got[tool.Name] = tool
	}
	want := 0
	for _, e := range reg.Entries() {
		tool, ok := got[e.MCPName()]
		if !e.MCP {
			if ok {
				t.Errorf("%s is not an MCP operation but is listed as a tool", e.Name)
			}
			continue
		}
		want++
		if !ok {
			t.Errorf("MCP operation %s is missing from tools/list", e.Name)
			continue
		}
		assertSameJSON(t, e.Name+" MCP input schema", mustJSON(t, e.InputSchema()), mustJSON(t, tool.InputSchema))
		a := tool.Annotations
		if a == nil || a.ReadOnlyHint != (e.Effect == op.Read) || a.DestructiveHint == nil || *a.DestructiveHint != (e.Effect == op.Destructive) {
			t.Errorf("%s: annotations %+v do not match effect %s", e.Name, a, e.Effect)
		}
		if now, _ := tool.Meta[mcp.MetaCLIImmediate].(bool); now != e.CLIImmediate {
			t.Errorf("%s: _meta %s is %v, want %v", e.Name, mcp.MetaCLIImmediate, now, e.CLIImmediate)
		}
	}
	if len(tools) != want {
		t.Errorf("tools/list has %d tools, registry has %d MCP operations", len(tools), want)
	}
}

// MCPClient connects an in-memory MCP client to reg's tools. A nil auth means
// op.DenyWrites, the served default.
func MCPClient(t testing.TB, reg *op.Registry, auth op.Authorizer) *sdk.ClientSession {
	t.Helper()
	ctx := context.Background()
	ct, st := sdk.NewInMemoryTransports()
	ss, err := mcp.NewServer(reg, auth, nil).Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "toolkittest", Version: "v0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cs.Close()
		_ = ss.Wait()
	})
	return cs
}

// ErrorCode returns error.code from a CLI, HTTP or MCP error body, or "".
func ErrorCode(body []byte) string { return errCode(body) }

func (s Suite) cli(t testing.TB, reg *op.Registry, args ...string) (int, []byte, []byte) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	o := toolkit.CLIOptions(s.Options)
	o.Stdin, o.Stdout, o.Stderr = strings.NewReader(""), &stdout, &stderr
	code := cli.Run(context.Background(), reg, o, args)
	return code, stdout.Bytes(), stderr.Bytes()
}

func httpCall(t testing.TB, reg *op.Registry, auth op.Authorizer, method, path string, body any) (int, []byte) {
	t.Helper()
	var r *http.Request
	if body == nil {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, bytes.NewReader(mustJSON(t, body)))
	}
	w := httptest.NewRecorder()
	toolkit.Handler(reg, auth).ServeHTTP(w, r)
	return w.Code, w.Body.Bytes()
}

func mcpCall(t testing.TB, reg *op.Registry, auth op.Authorizer, e *op.Entry, args map[string]any) *sdk.CallToolResult {
	t.Helper()
	res, err := MCPClient(t, reg, auth).CallTool(context.Background(), &sdk.CallToolParams{Name: e.MCPName(), Arguments: args})
	if err != nil {
		t.Fatalf("MCP %s: %v", e.MCPName(), err)
	}
	return res
}

func input(c Case, extra map[string]any) map[string]any {
	m := map[string]any{}
	for k, v := range c.Input {
		m[k] = v
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func text(res *sdk.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func errCode(body []byte) string {
	var v struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &v)
	return v.Error.Code
}

func snapshot(t testing.TB, fx Fixture) []byte {
	t.Helper()
	if fx.State == nil {
		t.Fatal("Fixture.State is nil")
	}
	return mustJSON(t, fx.State())
}

func assertState(t testing.TB, fx Fixture, before []byte, changed bool, what string) {
	t.Helper()
	after := snapshot(t, fx)
	if eq := bytes.Equal(before, after); eq == changed {
		if changed {
			t.Errorf("%s did not change the state", what)
		} else {
			t.Errorf("%s changed the state:\nbefore %s\nafter  %s", what, before, after)
		}
	}
}

func mustJSON(t testing.TB, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// assertSameJSON compares two JSON documents by value, ignoring formatting.
func assertSameJSON(t testing.TB, what string, want, got []byte) {
	t.Helper()
	var w, g any
	if err := json.Unmarshal(want, &w); err != nil {
		t.Errorf("%s: want is not JSON: %v", what, err)
		return
	}
	if err := json.Unmarshal(got, &g); err != nil {
		t.Errorf("%s: got is not JSON: %v: %s", what, err, got)
		return
	}
	if !reflect.DeepEqual(w, g) {
		t.Errorf("%s differ:\nwant %s\ngot  %s", what, want, got)
	}
}

// SameJSON reports whether two JSON documents are equal by value.
func SameJSON(a, b []byte) bool {
	var x, y any
	return json.Unmarshal(a, &x) == nil && json.Unmarshal(b, &y) == nil && reflect.DeepEqual(x, y)
}
