package api_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/0xble/toolkit/api"
	"github.com/0xble/toolkit/op"
	"github.com/0xble/toolkit/toolkittest"
)

type in struct {
	Name string `json:"name"`
}

type out struct {
	Applied bool `json:"applied"`
}

func registry() *op.Registry {
	r := op.New("t", "v")
	op.Add(r, op.Op[in, out]{Name: "thing.make", Effect: op.Write, Handler: func(_ context.Context, req op.Request, _ in) (out, error) {
		return out{Applied: req.Apply}, nil
	}})
	return r
}

func call(t *testing.T, h http.Handler, body string) (int, string) {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/ops/thing.make", strings.NewReader(body))
	r.Header.Set("X-Caller", "alice")
	h.ServeHTTP(w, r)
	return w.Code, w.Body.String()
}

func TestPluggableAuthorizerSeesTheRequest(t *testing.T) {
	auth := op.AuthorizerFunc(func(_ context.Context, _ *op.Entry, req op.Request) error {
		if req.Surface != op.SurfaceHTTP || req.HTTP == nil || req.HTTP.Header.Get("X-Caller") != "alice" {
			return errors.New("unexpected request")
		}
		return nil
	})
	code, body := call(t, api.Handler(registry(), api.Options{Authorizer: auth}), `{"name":"x","apply":true}`)
	if code != 200 || strings.TrimSpace(body) != `{"applied":true}` {
		t.Errorf("custom authorizer: %d %s", code, body)
	}
	code, body = call(t, api.Handler(registry(), api.Options{}), `{"name":"x","apply":true}`)
	if code != 403 || toolkittest.ErrorCode([]byte(body)) != "write_not_authorized" {
		t.Errorf("default authorizer: %d %s", code, body)
	}
	code, body = call(t, api.Handler(registry(), api.Options{Authorizer: op.AuthorizerFunc(func(context.Context, *op.Entry, op.Request) error {
		return errors.New("nope")
	})}), `{"name":"x"}`)
	if code != 403 || toolkittest.ErrorCode([]byte(body)) != "auth" {
		t.Errorf("a plain authorizer error is an auth error: %d %s", code, body)
	}
}

func TestBadBodies(t *testing.T) {
	h := api.Handler(registry(), api.Options{})
	for body, want := range map[string]string{
		`[1]`:                    "invalid_input",
		`{"name":1}`:             "invalid_input",
		`{"name":"x","extra":1}`: "invalid_input",
		`{"name":"` + strings.Repeat("a", api.MaxBodyBytes) + `"}`: "input_too_large",
	} {
		if code, got := call(t, h, body); code != 400 || toolkittest.ErrorCode([]byte(got)) != want {
			t.Errorf("%.40s: %d %.200s; want 400 %s", body, code, got, want)
		}
	}
}

func TestOpenAPIMatchesRegistry(t *testing.T) {
	r := registry()
	w := httptest.NewRecorder()
	api.Handler(r, api.Options{}).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	toolkittest.CheckOpenAPI(t, r, w.Body.Bytes())
}

func TestUnknownRouteIsJSON(t *testing.T) {
	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/nope", nil),
		httptest.NewRequest(http.MethodGet, "/ops/thing.make", nil),
	} {
		w := httptest.NewRecorder()
		api.Handler(registry(), api.Options{}).ServeHTTP(w, req)
		if w.Code != 404 || toolkittest.ErrorCode(w.Body.Bytes()) != "unknown_route" {
			t.Errorf("%s %s: %d %s", req.Method, req.URL, w.Code, w.Body)
		}
	}
}

func TestErrorBodyCarriesTheResult(t *testing.T) {
	r := op.New("t", "v")
	op.Add(r, op.Op[in, out]{Name: "thing.make", Effect: op.Read, Handler: func(context.Context, op.Request, in) (out, error) {
		return out{}, &op.Error{Kind: op.KindPartial, Code: "stopped", Message: "stopped", Result: out{Applied: true}}
	}})
	code, body := call(t, api.Handler(r, api.Options{}), `{"name":"x"}`)
	if code != 207 || strings.TrimSpace(body) != `{"error":{"code":"stopped","message":"stopped"},"result":{"applied":true}}` {
		t.Errorf("partial result: %d %s", code, body)
	}
	code, body = call(t, api.Handler(registry(), api.Options{}), `{"name":1}`)
	if code != 400 || strings.Contains(body, "result") {
		t.Errorf("an error without a result has no result key: %d %s", code, body)
	}
}

func TestAnyJSONOutput(t *testing.T) {
	r := op.New("t", "v")
	op.Add(r, op.Op[in, any]{Name: "thing.make", Effect: op.Read, Handler: func(context.Context, op.Request, in) (any, error) {
		return []any{1, "x", nil}, nil
	}})
	h := api.Handler(r, api.Options{})
	if code, body := call(t, h, `{"name":"x"}`); code != 200 || strings.TrimSpace(body) != `[1,"x",null]` {
		t.Errorf("any output: %d %s", code, body)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	toolkittest.CheckOpenAPI(t, r, w.Body.Bytes())
	if !strings.Contains(w.Body.String(), `"schema":{"type":["object","array","string","number","boolean","null"]}`) {
		t.Errorf("OpenAPI response schema is not the any-JSON schema: %s", w.Body)
	}
}

func TestAliasesAreNotRoutes(t *testing.T) {
	r := op.New("t", "v")
	op.Add(r, op.Op[in, out]{Name: "thing.make", Effect: op.Read, Aliases: []string{"mk"}, Handler: func(context.Context, op.Request, in) (out, error) {
		return out{}, nil
	}})
	h := api.Handler(r, api.Options{})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/ops/thing.mk", strings.NewReader(`{"name":"x"}`)))
	if w.Code != 404 {
		t.Errorf("an alias is not an HTTP route: %d %s", w.Code, w.Body)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	toolkittest.CheckOpenAPI(t, r, w.Body.Bytes())
	if strings.Contains(w.Body.String(), "mk") {
		t.Errorf("the OpenAPI document mentions the alias: %s", w.Body)
	}
}
