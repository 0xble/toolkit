package toolkit_test

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/0xble/toolkit"
	"github.com/0xble/toolkit/cli"
	"github.com/0xble/toolkit/op"
	"github.com/0xble/toolkit/toolkittest"
)

type applied struct {
	Applied bool `json:"applied"`
}

func allowRegistry() *op.Registry {
	r := op.New("t", "v")
	h := func(_ context.Context, req op.Request, _ struct{}) (applied, error) { return applied{req.Apply}, nil }
	op.Add(r, op.Op[struct{}, applied]{Name: "thing.make", Effect: op.Write, MCP: true, Handler: h})
	op.Add(r, op.Op[struct{}, applied]{Name: "thing.drop", Effect: op.Destructive, MCP: true, Handler: h})
	op.Add(r, op.Op[struct{}, applied]{Name: "thing.other", Effect: op.Write, MCP: true, Handler: h})
	op.Add(r, op.Op[struct{}, applied]{Name: "thing.get", Effect: op.Read, MCP: true, Handler: h})
	return r
}

func runServe(ctx context.Context, args ...string) (int, string) {
	var stderr bytes.Buffer
	o := toolkit.CLIOptions(toolkit.Options{})
	o.Stdin, o.Stdout, o.Stderr = strings.NewReader(""), io.Discard, &stderr
	code := cli.Run(ctx, allowRegistry(), o, append([]string{"--agent", "serve"}, args...))
	return code, stderr.String()
}

func TestAllowApplyStartupErrors(t *testing.T) {
	sock := filepath.Join(shortDir(t), "s.sock")
	for _, names := range []string{"thing.nope", "thing.get", "thing.make,thing.get"} {
		code, stderr := runServe(context.Background(), "--socket", sock, "--allow-apply", names)
		if code != 2 || toolkittest.ErrorCode([]byte(stderr)) != "invalid_allow_apply" {
			t.Errorf("--allow-apply %s: exit %d %s", names, code, stderr)
		}
	}
}

// TestAllowApply serves on a real socket and calls it from this process,
// which runs as the server's user.
func TestAllowApply(t *testing.T) {
	sock := filepath.Join(shortDir(t), "s.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() {
		code, _ := runServe(ctx, "--socket", sock, "--allow-apply", "thing.make", "--allow-apply=thing.drop")
		done <- code
	}()
	t.Cleanup(func() {
		cancel()
		if code := <-done; code != 0 {
			t.Errorf("serve exit %d", code)
		}
	})
	hc := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		},
	}}
	post := func(name, body string, header ...string) (int, string) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, "http://t/ops/"+name, strings.NewReader(body))
		for i := 0; i+1 < len(header); i += 2 {
			req.Header.Set(header[i], header[i+1])
		}
		var resp *http.Response
		var err error
		for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
			if resp, err = hc.Do(req.Clone(ctx)); err == nil || time.Now().After(deadline) {
				break
			}
			req.Body = io.NopCloser(strings.NewReader(body))
		}
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, strings.TrimSpace(string(b))
	}
	for _, c := range []struct {
		what, name, body string
		header           []string
		status           int
		want             string
	}{
		{"listed write", "thing.make", `{"apply":true}`, nil, 200, `{"applied":true}`},
		{"listed destructive", "thing.drop", `{"apply":true,"confirm":true}`, nil, 200, `{"applied":true}`},
		{"listed destructive without confirm", "thing.drop", `{"apply":true}`, nil, 400, "confirmation_required"},
		{"unlisted write", "thing.other", `{"apply":true}`, nil, 403, "write_not_authorized"},
		{"preview", "thing.other", `{}`, nil, 200, `{"applied":false}`},
		{"Tailscale login", "thing.make", `{"apply":true}`, []string{"Tailscale-User-Login", "a@example.com"}, 403, "write_not_authorized"},
		{"Tailscale name", "thing.make", `{"apply":true}`, []string{"Tailscale-User-Name", "A"}, 403, "write_not_authorized"},
		{"Tailscale capabilities", "thing.make", `{"apply":true}`, []string{"Tailscale-App-Capabilities", "{}"}, 403, "write_not_authorized"},
	} {
		status, body := post(c.name, c.body, c.header...)
		got := body
		if status != 200 {
			got = toolkittest.ErrorCode([]byte(body))
		}
		if status != c.status || got != c.want {
			t.Errorf("%s: %d %s; want %d %s", c.what, status, body, c.status, c.want)
		}
	}

	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "v0"}, nil).Connect(ctx,
		&sdk.StreamableClientTransport{Endpoint: "http://t/mcp", HTTPClient: hc, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cs.Close() }()
	res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "thing_make", Arguments: map[string]any{"apply": true}})
	if err != nil || !res.IsError || toolkittest.ErrorCode([]byte(res.Content[0].(*sdk.TextContent).Text)) != "write_not_authorized" {
		t.Errorf("listed write over /mcp: %+v %v", res, err)
	}
}
