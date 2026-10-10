package op_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/0xble/toolkit/op"
	"github.com/0xble/toolkit/output"
	"github.com/0xble/toolkit/toolkittest"
)

type idIn struct {
	ID string `json:"id" help:"Item ID"`
}

type listIn struct {
	Limit int    `json:"limit,omitempty" default:"20" help:"Page size"`
	Tag   string `json:"tag,omitempty"`
}

type nestedStringIn struct {
	Nested nestedStringFields `json:"nested"`
}

type nestedStringFields struct {
	Count   int  `json:"count,string" default:"5"`
	Enabled bool `json:"enabled,omitempty,string" default:"true"`
}

type sectionOpts struct {
	N int `json:"n,omitempty" default:"3"`
}

type sectionIn struct {
	Sub *sectionOpts `json:"sub,omitempty"`
}

// badPage has an items key that is not an array.
type badPage struct {
	Items string `json:"items"`
}

type res struct {
	Applied bool `json:"applied"`
}

func handler[In any](calls *int) func(context.Context, op.Request, In) (res, error) {
	return func(_ context.Context, req op.Request, _ In) (res, error) {
		*calls++
		return res{Applied: req.Apply}, nil
	}
}

func TestAddRejectsBadDeclarations(t *testing.T) {
	var n int
	ok := op.Op[idIn, res]{Name: "item.get", CLI: "item <id> get", Effect: op.Read, Handler: handler[idIn](&n)}
	cases := map[string]func(r *op.Registry){
		"bad name":            func(r *op.Registry) { o := ok; o.Name = "Item.Get"; op.Add(r, o) },
		"no handler":          func(r *op.Registry) { o := ok; o.Handler = nil; op.Add(r, o) },
		"bad effect":          func(r *op.Registry) { o := ok; o.Effect = "maybe"; op.Add(r, o) },
		"duplicate":           func(r *op.Registry) { op.Add(r, ok); op.Add(r, ok) },
		"placeholder field":   func(r *op.Registry) { o := ok; o.CLI = "item <key> get"; op.Add(r, o) },
		"trailing arg":        func(r *op.Registry) { o := ok; o.CLI = "item get <id>"; op.Add(r, o) },
		"leading arg":         func(r *op.Registry) { o := ok; o.CLI = "<id> get"; op.Add(r, o) },
		"same command words":  func(r *op.Registry) { op.Add(r, ok); o := ok; o.Name = "item.show"; o.CLI = "item get"; op.Add(r, o) },
		"prefix of another":   func(r *op.Registry) { op.Add(r, ok); o := ok; o.Name = "item.x"; o.CLI = "item"; op.Add(r, o) },
		"immediate read":      func(r *op.Registry) { o := ok; o.CLIImmediate = true; op.Add(r, o) },
		"paged without items": func(r *op.Registry) { o := ok; o.Paged = true; op.Add(r, o) },
		"paged non-array items": func(r *op.Registry) {
			op.Add(r, op.Op[idIn, badPage]{Name: "a", Effect: op.Read, Paged: true,
				Handler: func(context.Context, op.Request, idIn) (badPage, error) { return badPage{}, nil }})
		},
		"paged any output": func(r *op.Registry) {
			op.Add(r, op.Op[idIn, any]{Name: "a", Effect: op.Read, Paged: true,
				Handler: func(context.Context, op.Request, idIn) (any, error) { return nil, nil }})
		},
		"two render hooks": func(r *op.Registry) {
			o := ok
			o.Render = func(io.Writer, res) error { return nil }
			o.RenderWithInput = func(io.Writer, idIn, res) error { return nil }
			op.Add(r, o)
		},
		"reserved apply": func(r *op.Registry) {
			op.Add(r, op.Op[struct {
				Apply bool `json:"apply"`
			}, res]{Name: "a", Effect: op.Read, Handler: handler[struct {
				Apply bool `json:"apply"`
			}](&n)})
		},
		"non-struct input": func(r *op.Registry) {
			op.Add(r, op.Op[string, res]{Name: "a", Effect: op.Read, Handler: handler[string](&n)})
		},
		"bad default": func(r *op.Registry) {
			op.Add(r, op.Op[struct {
				N int `json:"n" default:"x"`
			}, res]{Name: "a", Effect: op.Read, Handler: handler[struct {
				N int `json:"n" default:"x"`
			}](&n)})
		},
	}
	for name, add := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("Add did not panic")
				}
			}()
			add(op.New("t", "v"))
		})
	}
}

func TestWireSchemaAddsControlsByEffect(t *testing.T) {
	var n int
	r := op.New("t", "v")
	op.Add(r, op.Op[idIn, res]{Name: "r", Effect: op.Read, Handler: handler[idIn](&n)})
	op.Add(r, op.Op[idIn, res]{Name: "w", Effect: op.Write, Handler: handler[idIn](&n)})
	op.Add(r, op.Op[idIn, res]{Name: "d", Effect: op.Destructive, Handler: handler[idIn](&n)})
	for name, want := range map[string][]string{"r": nil, "w": {"apply"}, "d": {"apply", "confirm"}} {
		s := r.Lookup(name).InputSchema()
		for _, f := range []string{"apply", "confirm"} {
			_, has := s.Properties[f]
			if has != strings.Contains(strings.Join(want, ","), f) {
				t.Errorf("%s: has %s = %v", name, f, has)
			}
		}
		if s.Properties["id"].Description != "Item ID" {
			t.Errorf("%s: help tag not copied into the description", name)
		}
	}
}

func TestDecodeValidatesAndAppliesDefaults(t *testing.T) {
	var n int
	r := op.New("t", "v")
	op.Add(r, op.Op[listIn, res]{Name: "l", Effect: op.Read, Handler: handler[listIn](&n)})
	e := r.Lookup("l")
	in, _, _, err := e.Decode(json.RawMessage(``))
	if err != nil || in.(*listIn).Limit != 20 {
		t.Errorf("empty body: %+v, %v; want the default limit", in, err)
	}
	for _, bad := range []string{`[]`, `{"limit":"x"}`, `{"nope":1}`, `{"apply":true}`, `nul`} {
		if _, _, _, err := e.Decode(json.RawMessage(bad)); op.AsError(err, op.KindError).Kind != op.KindUsage {
			t.Errorf("%s: err %v, want a usage error", bad, err)
		}
	}
	if s := e.InputSchema(); string(s.Properties["limit"].Default) != "20" || len(s.Required) != 0 {
		t.Errorf("default not in schema: %+v", s)
	}
}

func TestSchemaAnnotatesJSONStringDefaultsAndNestedFields(t *testing.T) {
	var n int
	r := op.New("t", "v")
	op.Add(r, op.Op[nestedStringIn, res]{Name: "nested", Effect: op.Read, Handler: handler[nestedStringIn](&n)})
	e := r.Lookup("nested")
	s := e.InputSchema()
	nested := s.Properties["nested"]
	if nested == nil {
		t.Fatal("nested property missing")
	}
	for name, want := range map[string]string{"count": `"5"`, "enabled": `"true"`} {
		p := nested.Properties[name]
		if p == nil || p.Type != "string" || string(p.Default) != want {
			t.Errorf("nested.%s: schema=%+v, want string default %s", name, p, want)
		}
	}
	if len(nested.Required) != 0 {
		t.Errorf("nested defaults remain required: %v", nested.Required)
	}
	if _, _, _, err := e.Decode(json.RawMessage(`{"nested":{"count":"6"}}`)); err != nil {
		t.Fatalf("string-tagged nested input should decode: %v", err)
	}
	in, _, _, err := e.Decode(json.RawMessage(`{"nested":{}}`))
	if err != nil || in.(*nestedStringIn).Nested.Count != 5 || !in.(*nestedStringIn).Nested.Enabled {
		t.Errorf("nested defaults: input=%+v err=%v", in, err)
	}
}

func TestPointerSectionsStayNilUnlessSent(t *testing.T) {
	var n int
	r := op.New("t", "v")
	op.Add(r, op.Op[sectionIn, res]{Name: "section", Effect: op.Read, Handler: handler[sectionIn](&n)})
	e := r.Lookup("section")
	in, _, _, err := e.Decode(json.RawMessage(`{}`))
	if err != nil || in.(*sectionIn).Sub != nil {
		t.Errorf("an omitted section stays nil: %+v %v", in, err)
	}
	for body, want := range map[string]int{`{"sub":{}}`: 3, `{"sub":{"n":7}}`: 7} {
		in, _, _, err := e.Decode(json.RawMessage(body))
		if err != nil || in.(*sectionIn).Sub == nil || in.(*sectionIn).Sub.N != want {
			t.Errorf("%s: %+v %v, want n=%d", body, in, err, want)
		}
	}
}

func TestCallEnforcesApplyAndConfirm(t *testing.T) {
	var n int
	r := op.New("t", "v")
	op.Add(r, op.Op[idIn, res]{Name: "r", Effect: op.Read, Handler: handler[idIn](&n)})
	op.Add(r, op.Op[idIn, res]{Name: "d", Effect: op.Destructive, Handler: handler[idIn](&n)})
	ctx := context.Background()

	out, err := r.Lookup("r").Call(ctx, op.Request{Apply: true, Confirm: true}, &idIn{})
	if err != nil || out.(res).Applied {
		t.Errorf("a read must never see apply: %+v, %v", out, err)
	}
	d := r.Lookup("d")
	n = 0
	if out, err := d.Call(ctx, op.Request{}, &idIn{}); err != nil || out.(res).Applied {
		t.Errorf("destructive preview: %+v, %v", out, err)
	}
	_, err = d.Call(ctx, op.Request{Apply: true}, &idIn{})
	if oe := op.AsError(err, op.KindError); oe.Code != "confirmation_required" || oe.Kind.ExitCode() != 2 || n != 1 {
		t.Errorf("destructive apply without confirm: %v, handler calls %d", err, n)
	}
	if out, err := d.Call(ctx, op.Request{Apply: true, Confirm: true}, &idIn{}); err != nil || !out.(res).Applied {
		t.Errorf("confirmed apply: %+v, %v", out, err)
	}

	// The remote path refuses before authorizing, and authorizes before running.
	n = 0
	var authorized int
	auth := op.AuthorizerFunc(func(context.Context, *op.Entry, op.Request) error { authorized++; return nil })
	if _, err := d.CallJSON(ctx, op.Request{}, json.RawMessage(`{"id":"x","apply":true}`), auth); op.AsError(err, op.KindError).Code != "confirmation_required" || authorized+n != 0 {
		t.Errorf("CallJSON without confirm: %v, authorized %d, calls %d", err, authorized, n)
	}
	if _, err := d.CallJSON(ctx, op.Request{}, json.RawMessage(`{"id":"x","apply":true,"confirm":true}`), nil); op.AsError(err, op.KindError).Code != "write_not_authorized" || n != 0 {
		t.Errorf("default authorizer: %v, calls %d", err, n)
	}
	if out, err := d.CallJSON(ctx, op.Request{}, json.RawMessage(`{"id":"x"}`), nil); err != nil || out.(res).Applied {
		t.Errorf("default authorizer preview: %+v, %v", out, err)
	}

	n, authorized = 0, 0
	deny := op.AuthorizerFunc(func(context.Context, *op.Entry, op.Request) error {
		authorized++
		return &op.Error{Kind: op.KindAuth, Code: "custom_denied", Message: "denied by custom authorizer"}
	})
	out, err = d.CallJSON(ctx, op.Request{}, json.RawMessage(`{"id":"x","apply":true,"confirm":true}`), deny)
	if oe := op.AsError(err, op.KindError); out != nil || oe == nil || oe.Kind != op.KindAuth || oe.Code != "custom_denied" || oe.Message != "denied by custom authorizer" || authorized != 1 || n != 0 {
		t.Errorf("custom authorizer denial: out=%+v err=%v, authorized %d, handler calls %d", out, err, authorized, n)
	}

	n, authorized = 0, 0
	allow := op.AuthorizerFunc(func(context.Context, *op.Entry, op.Request) error {
		authorized++
		if n != 0 {
			t.Errorf("custom authorizer ran after %d handler calls", n)
		}
		return nil
	})
	out, err = d.CallJSON(ctx, op.Request{}, json.RawMessage(`{"id":"x","apply":true,"confirm":true}`), allow)
	if got, ok := out.(res); err != nil || !ok || !got.Applied || authorized != 1 || n != 1 {
		t.Errorf("custom authorizer apply: out=%+v err=%v, authorized %d, handler calls %d", out, err, authorized, n)
	}
}

func TestKindsMapToEverySurface(t *testing.T) {
	want := map[op.Kind][2]int{
		op.KindError: {1, 500}, op.KindUsage: {2, 400}, op.KindNotFound: {3, 404}, op.KindConflict: {4, 409},
		op.KindAuth: {5, 403}, op.KindRate: {6, 429}, op.KindTimeout: {7, 504}, op.KindStaleIndex: {8, 503},
		op.KindModelUnavail: {9, 503}, op.KindPartial: {10, 207}, "unknown": {1, 500},
	}
	for k, w := range want {
		if k.ExitCode() != w[0] || k.HTTPStatus() != w[1] {
			t.Errorf("%s: exit %d status %d, want %v", k, k.ExitCode(), k.HTTPStatus(), w)
		}
	}
	e := (&op.Error{Kind: op.KindRate, Code: "slow", Message: "m", Suggestions: []string{"wait"}}).CLIError()
	if e.ExitCode != output.ExitRate || !e.Retryable || e.Code != "slow" {
		t.Errorf("CLIError: %+v", e)
	}
	b, _ := json.Marshal(&op.Error{Kind: op.KindAuth, Code: "c", Message: "m", Suggestions: []string{"s"}})
	if string(b) != `{"code":"c","message":"m","suggestions":["s"]}` {
		t.Errorf("wire form: %s", b)
	}
}

func TestAsError(t *testing.T) {
	if e := op.AsError(fmt.Errorf("wrap: %w", context.DeadlineExceeded), op.KindError); e.Kind != op.KindTimeout {
		t.Errorf("deadline: %+v", e)
	}
	if e := op.AsError(output.ErrWithExit("gone", "m", output.ExitNotFound), op.KindError); e.Kind != op.KindNotFound || e.Code != "gone" {
		t.Errorf("CLIError: %+v", e)
	}
	if e := op.AsError(errors.New("boom"), op.KindAuth); e.Kind != op.KindAuth || e.Code != "auth" {
		t.Errorf("fallback: %+v", e)
	}
	if op.AsError(nil, op.KindError) != nil {
		t.Error("nil error")
	}
}

func TestMetadataIsSchemaValid(t *testing.T) {
	var n int
	r := op.New("t", "v1")
	op.Add(r, op.Op[listIn, []res]{Name: "items.list", Effect: op.Read, MCP: true, Handler: func(context.Context, op.Request, listIn) ([]res, error) { return nil, nil }})
	op.Add(r, op.Op[idIn, res]{Name: "item.delete", CLI: "item <id> delete", Effect: op.Destructive, Handler: handler[idIn](&n)})
	b, err := json.Marshal(r.Metadata())
	if err != nil {
		t.Fatal(err)
	}
	toolkittest.CheckMetadata(t, b)
	m := r.Metadata()
	if m.Schema != op.MetadataSchemaID || len(m.Operations) != 2 || m.Operations[0].Name != "item.delete" || m.Operations[0].MCPName != "item_delete" {
		t.Errorf("metadata: %+v", m)
	}
}

func TestDenyWrites(t *testing.T) {
	var n int
	r := op.New("t", "v")
	op.Add(r, op.Op[idIn, res]{Name: "w", Effect: op.Write, Handler: handler[idIn](&n)})
	op.Add(r, op.Op[idIn, res]{Name: "r", Effect: op.Read, Handler: handler[idIn](&n)})
	ctx := context.Background()
	hr, _ := http.NewRequest(http.MethodPost, "/", nil)
	if err := op.DenyWrites.Authorize(ctx, r.Lookup("w"), op.Request{Apply: true, HTTP: hr}); op.AsError(err, op.KindError).Kind != op.KindAuth {
		t.Errorf("apply: %v", err)
	}
	if err := op.DenyWrites.Authorize(ctx, r.Lookup("w"), op.Request{}); err != nil {
		t.Errorf("preview: %v", err)
	}
	if err := op.DenyWrites.Authorize(ctx, r.Lookup("r"), op.Request{}); err != nil {
		t.Errorf("read: %v", err)
	}
}

func TestDefaultCommandNeedsAParent(t *testing.T) {
	defer func() {
		if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), "parent") {
			t.Errorf("a one-word default command must be rejected: %v", r)
		}
	}()
	op.Add(op.New("t", "v"), op.Op[struct{}, int]{Name: "solo", Effect: op.Read, DefaultCommand: true,
		Handler: func(context.Context, op.Request, struct{}) (int, error) { return 0, nil }})
}

func TestErrorResultMustHaveTheOutputType(t *testing.T) {
	r := op.New("t", "v")
	op.Add(r, op.Op[struct{}, int]{Name: "bad", Effect: op.Read,
		Handler: func(context.Context, op.Request, struct{}) (int, error) {
			return 0, &op.Error{Kind: op.KindPartial, Code: "p", Message: "p", Result: "not an int"}
		}})
	e := r.Lookup("bad")
	_, err := e.Call(context.Background(), op.Request{}, e.NewInput())
	if err == nil || !strings.Contains(err.Error(), "error result is string") {
		t.Errorf("err = %v", err)
	}
}
