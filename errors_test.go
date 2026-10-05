package toolkit_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/0xble/toolkit"
	"github.com/0xble/toolkit/cli"
	"github.com/0xble/toolkit/op"
	"github.com/0xble/toolkit/toolkittest"
)

func boolPtr(v bool) *bool { return &v }

type surfaces struct {
	cliJSON, cliText string
	status           int
	retryAfter       string
	http, mcp        string
}

// errorOn runs one failing read operation on the CLI (--agent and human),
// HTTP and MCP, and returns what each surface printed.
func errorOn(t *testing.T, err *op.Error) surfaces {
	t.Helper()
	r := op.New("t", "v1")
	op.Add(r, op.Op[struct{}, map[string]any]{Name: "fail", Effect: op.Read, MCP: true,
		Handler: func(context.Context, op.Request, struct{}) (map[string]any, error) { return nil, err }})
	var s surfaces
	for _, agent := range []bool{true, false} {
		var stdout, stderr bytes.Buffer
		o := toolkit.CLIOptions(toolkit.Options{})
		o.Stdin, o.Stdout, o.Stderr = strings.NewReader(""), &stdout, &stderr
		args := []string{"fail"}
		if agent {
			args = append(args, "--agent")
		}
		if code := cli.Run(context.Background(), r, o, args); code != err.Kind.ExitCode() {
			t.Errorf("CLI %v: exit %d, want %d", args, code, err.Kind.ExitCode())
		}
		if agent {
			s.cliJSON = stderr.String()
		} else {
			s.cliText = stderr.String()
		}
	}
	w := httptest.NewRecorder()
	toolkit.Handler(r, nil).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/ops/fail", strings.NewReader(`{}`)))
	s.status, s.retryAfter, s.http = w.Code, w.Header().Get("Retry-After"), strings.TrimSuffix(w.Body.String(), "\n")
	res, cerr := toolkittest.MCPClient(t, r, nil).CallTool(context.Background(), &sdk.CallToolParams{Name: "fail", Arguments: map[string]any{}})
	if cerr != nil || !res.IsError {
		t.Fatalf("MCP: %+v %v", res, cerr)
	}
	s.mcp = res.Content[0].(*sdk.TextContent).Text
	return s
}

// TestErrorEnvelopesWithoutDetailsAreUnchanged pins the bytes every surface
// printed before op.Error gained provider details.
func TestErrorEnvelopesWithoutDetailsAreUnchanged(t *testing.T) {
	for _, c := range []struct {
		err  *op.Error
		want surfaces
	}{
		{&op.Error{Kind: op.KindRate, Code: "rate_limited", Message: "slow down", Suggestions: []string{"wait"}}, surfaces{
			cliJSON: `{"error":{"code":"rate_limited","message":"slow down","suggestions":["wait"],"exit_code":6,"retryable":true}}` + "\n",
			cliText: "error: slow down\n  hint: wait\n",
			status:  429,
			http:    `{"error":{"code":"rate_limited","message":"slow down","suggestions":["wait"]}}`,
			mcp:     `{"error":{"code":"rate_limited","message":"slow down","suggestions":["wait"]}}`,
		}},
		{&op.Error{Kind: op.KindError, Code: "boom", Message: "failed"}, surfaces{
			cliJSON: `{"error":{"code":"boom","message":"failed","exit_code":1}}` + "\n",
			cliText: "error: failed\n",
			status:  500,
			http:    `{"error":{"code":"boom","message":"failed"}}`,
			mcp:     `{"error":{"code":"boom","message":"failed"}}`,
		}},
	} {
		if got := errorOn(t, c.err); got != c.want {
			t.Errorf("%s:\ngot  %+v\nwant %+v", c.err.Code, got, c.want)
		}
	}
}

// TestProviderErrorDetails checks that each detail is printed when set, under
// the same key and value on the CLI, HTTP and MCP.
func TestProviderErrorDetails(t *testing.T) {
	for _, c := range []struct {
		err  *op.Error
		want surfaces
	}{
		// A provider 503 reported as a plain error kind that the
		// operation marks retryable.
		{&op.Error{Kind: op.KindError, Code: "provider_unavailable", Message: "provider is down",
			Retryable: boolPtr(true), HTTPStatus: 503, RequestID: "req_1"}, surfaces{
			cliJSON: `{"error":{"code":"provider_unavailable","message":"provider is down","exit_code":1,"retryable":true,"http_status":503,"request_id":"req_1"}}` + "\n",
			cliText: "error: provider is down\n",
			status:  500,
			http:    `{"error":{"code":"provider_unavailable","message":"provider is down","retryable":true,"http_status":503,"request_id":"req_1"}}`,
			mcp:     `{"error":{"code":"provider_unavailable","message":"provider is down","retryable":true,"http_status":503,"request_id":"req_1"}}`,
		}},
		// A provider 429: the kind already says retryable, so only the CLI,
		// which always derived it, prints it.
		{&op.Error{Kind: op.KindRate, Code: "rate_limited", Message: "retry after 30s",
			HTTPStatus: 429, RetryAfterSeconds: 30}, surfaces{
			cliJSON:    `{"error":{"code":"rate_limited","message":"retry after 30s","exit_code":6,"retryable":true,"http_status":429,"retry_after_seconds":30}}` + "\n",
			cliText:    "error: retry after 30s\n",
			status:     429,
			retryAfter: "30",
			http:       `{"error":{"code":"rate_limited","message":"retry after 30s","http_status":429,"retry_after_seconds":30}}`,
			mcp:        `{"error":{"code":"rate_limited","message":"retry after 30s","http_status":429,"retry_after_seconds":30}}`,
		}},
		// An explicit false overrides the rate kind and is printed everywhere.
		{&op.Error{Kind: op.KindRate, Code: "quota_exhausted", Message: "monthly quota used", Retryable: boolPtr(false)}, surfaces{
			cliJSON: `{"error":{"code":"quota_exhausted","message":"monthly quota used","exit_code":6,"retryable":false}}` + "\n",
			cliText: "error: monthly quota used\n",
			status:  429,
			http:    `{"error":{"code":"quota_exhausted","message":"monthly quota used","retryable":false}}`,
			mcp:     `{"error":{"code":"quota_exhausted","message":"monthly quota used","retryable":false}}`,
		}},
	} {
		if got := errorOn(t, c.err); got != c.want {
			t.Errorf("%s:\ngot  %+v\nwant %+v", c.err.Code, got, c.want)
		}
	}
}

func TestCLIErrorUsesTheRetryableOverride(t *testing.T) {
	for _, c := range []struct {
		err  *op.Error
		want bool
	}{
		{&op.Error{Kind: op.KindTimeout}, true},
		{&op.Error{Kind: op.KindTimeout, Retryable: boolPtr(false)}, false},
		{&op.Error{Kind: op.KindError}, false},
		{&op.Error{Kind: op.KindError, Retryable: boolPtr(true)}, true},
	} {
		if got := c.err.CLIError().Retryable; got != c.want {
			t.Errorf("%+v: Retryable %v, want %v", c.err, got, c.want)
		}
	}
}
