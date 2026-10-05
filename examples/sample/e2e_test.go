package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/0xble/toolkit/toolkittest"
)

// TestEndToEnd builds the sample as a real binary and drives every surface:
// the CLI, HTTP on a Unix socket, MCP over that socket, and MCP over stdio.
// Everything is loopback against the in-memory backend.
func TestEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary")
	}
	bin := filepath.Join(t.TempDir(), "sample")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Env = append(os.Environ(), "GOWORK=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	reg, _ := New()

	// CLI.
	listJSON := run(t, bin, 0, "--agent", "--limit", "1", "notes", "list")
	same(t, "root flag vs command flag", listJSON, run(t, bin, 0, "notes", "list", "--limit", "1", "--json"))
	same(t, "root flag vs command flag env-free", listJSON, run(t, bin, 0, "--json", "notes", "--limit", "1", "list"))
	if !strings.Contains(string(run(t, bin, 0, "notes", "list")), "Groceries") {
		t.Error("human list output does not use the render hook")
	}
	if out := string(run(t, bin, 0, "--limit", "1", "notes", "list", "--tag", "home")); !strings.Contains(out, "Groceries") || strings.Contains(out, "next page") {
		t.Errorf("one home note fits one page: %s", out)
	}
	if out := string(run(t, bin, 0, "--limit", "2", "notes", "list")); !strings.Contains(out, "next page: sample notes list --limit 2 --cursor 2") {
		t.Errorf("the render hook repeats the caller's --limit from the input: %s", out)
	}
	same(t, "--fields on a Paged operation projects each item and keeps the envelope",
		[]byte(`{"items":[{"id":"n1","title":"Groceries"},{"id":"n2","title":"Standup"}],"next_cursor":"2","has_more":true}`),
		run(t, bin, 0, "--agent", "--limit", "2", "--fields", "id,title", "notes", "list"))
	if out := run(t, bin, 0, "--agent", "notes", "create", "Draft"); !strings.Contains(string(out), `"applied": true`) {
		t.Errorf("CLIImmediate create applies without --apply: %s", out)
	}
	if out := run(t, bin, 0, "--agent", "notes", "create", "Draft", "--apply"); !strings.Contains(string(out), `"applied": true`) {
		t.Errorf("--apply is still accepted: %s", out)
	}
	if out := string(run(t, bin, 0, "notes", "create", "Draft", "--dry-run")); !strings.Contains(out, "would create") {
		t.Errorf("--dry-run previews: %s", out)
	}
	runErr(t, bin, 2, "notes", "create", "Draft", "--dry-run", "--apply")
	if out := run(t, bin, 0, "--agent", "note", "n1", "rename", "Shopping"); !strings.Contains(string(out), `"applied": false`) {
		t.Errorf("a write without CLIImmediate still previews: %s", out)
	}
	meta := run(t, bin, 0, "metadata", "--json")
	toolkittest.CheckMetadata(t, meta)
	preview := run(t, bin, 0, "--agent", "note", "n1", "delete")
	if !strings.Contains(string(preview), `"applied": false`) {
		t.Errorf("delete without --apply should preview: %s", preview)
	}
	errCode(t, "CLI destructive apply without --yes", runErr(t, bin, 2, "--agent", "note", "n1", "delete", "--apply"), "confirmation_required")
	errCode(t, "CLI unknown note", runErr(t, bin, 3, "--agent", "note", "n9", "show"), "note_not_found")
	runErr(t, bin, 2, "notes", "list", "--no-such-flag")
	exported := filepath.Join(t.TempDir(), "notes.json")
	if out := string(run(t, bin, 0, "--agent", "notes", "export", "--out", exported)); !strings.Contains(out, `"path": "`+exported+`"`) {
		t.Errorf("CLI export --out: %s", out)
	}
	if b, err := os.ReadFile(exported); err != nil || !strings.Contains(string(b), "Groceries") {
		t.Errorf("CLI export --out wrote %q, %v", b, err)
	}
	unavailable := `{"error":{"code":"provider_unavailable","message":"upstream answered 503","retryable":true,"http_status":503,"request_id":"req_sample"}}`
	if got := string(runErr(t, bin, 1, "--agent", "notes", "sync", "--fail", "503")); got !=
		`{"error":{"code":"provider_unavailable","message":"upstream answered 503","exit_code":1,"retryable":true,"http_status":503,"request_id":"req_sample"}}`+"\n" {
		t.Errorf("CLI provider error details: %s", got)
	}
	if got := string(runErr(t, bin, 6, "--agent", "notes", "sync", "--fail", "429")); !strings.Contains(got, `"exit_code":6,"retryable":true,"http_status":429,"retry_after_seconds":30,`) {
		t.Errorf("CLI rate limit details: %s", got)
	}
	if out := run(t, bin, 0, "--agent", "note", "n1", "delete", "--apply", "--yes"); !strings.Contains(string(out), `"applied": true`) {
		t.Errorf("delete --apply --yes should apply: %s", out)
	}

	// HTTP on a Unix socket.
	dir, err := os.MkdirTemp("", "tk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "sample.sock")
	serve := exec.Command(bin, "serve", "--socket", sock)
	var serveErr bytes.Buffer
	serve.Stderr = &serveErr
	if err := serve.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serve.Process.Kill(); _ = serve.Wait() })
	hc := waitForSocket(t, sock, &serveErr)
	fi, err := os.Stat(sock)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("socket mode is %v, want 0600", fi.Mode().Perm())
	}
	runErr(t, bin, 1, "serve", "--socket", sock)

	same(t, "GET /ops vs metadata --json", meta, get(t, hc, "/ops", 200))
	toolkittest.CheckOpenAPI(t, reg, get(t, hc, "/openapi.json", 200))
	same(t, "HTTP vs CLI", listJSON, post(t, hc, "notes.list", `{"limit":1}`, 200))
	errCode(t, "HTTP destructive apply without confirm", post(t, hc, "note.delete", `{"id":"n1","apply":true}`, 400), "confirmation_required")
	errCode(t, "HTTP apply under the default authorizer", post(t, hc, "note.delete", `{"id":"n1","apply":true,"confirm":true}`, 403), "write_not_authorized")
	errCode(t, "HTTP write apply under the default authorizer", post(t, hc, "note.create", `{"title":"x","apply":true}`, 403), "write_not_authorized")
	if out := post(t, hc, "note.create", `{"title":"x"}`, 200); !strings.Contains(string(out), `"applied":false`) {
		t.Errorf("HTTP create without apply previews despite CLIImmediate: %s", out)
	}
	if got := strings.TrimSpace(string(post(t, hc, "notes.sync", `{"fail":503}`, 500))); got != unavailable {
		t.Errorf("HTTP provider error details: %s", got)
	}
	resp, err := hc.Post("http://sample/ops/notes.sync", "application/json", strings.NewReader(`{"fail":429}`))
	if err != nil {
		t.Fatal(err)
	}
	if b := body(t, resp, "POST notes.sync 429", 429); resp.Header.Get("Retry-After") != "30" || !strings.Contains(string(b), `"retry_after_seconds":30`) {
		t.Errorf("HTTP rate limit: Retry-After %q, %s", resp.Header.Get("Retry-After"), b)
	}
	errCode(t, "HTTP bad input", post(t, hc, "notes.list", `{"limit":"many"}`, 400), "invalid_input")
	refused := filepath.Join(dir, "refused.json")
	errCode(t, "HTTP cli-only input", post(t, hc, "notes.export", `{"out":"`+refused+`"}`, 400), "cli_only")
	if out := post(t, hc, "notes.export", `{}`, 200); !strings.Contains(string(out), `"notes":[`) {
		t.Errorf("HTTP export returns the notes: %s", out)
	}
	errCode(t, "HTTP unknown operation", post(t, hc, "nope", `{}`, 404), "unknown_operation")
	if out := post(t, hc, "note.delete", `{"id":"n1"}`, 200); !strings.Contains(string(out), `"applied":false`) {
		t.Errorf("HTTP preview: %s", out)
	}
	same(t, "HTTP state after refused writes", listJSON, post(t, hc, "notes.list", `{"limit":1}`, 200))

	// MCP over the socket.
	ctx := context.Background()
	httpMCP := connect(t, &sdk.StreamableClientTransport{Endpoint: "http://sample/mcp", HTTPClient: hc, MaxRetries: -1})
	checkTools(t, httpMCP)
	same(t, "HTTP MCP vs CLI", listJSON, toolText(t, httpMCP, "notes_list", map[string]any{"limit": 1}, false))
	errCode(t, "HTTP MCP apply under the default authorizer",
		toolText(t, httpMCP, "note_delete", map[string]any{"id": "n1", "apply": true, "confirm": true}, true), "write_not_authorized")

	// MCP over stdio. The process keeps its store across calls.
	stdio := connect(t, &sdk.CommandTransport{Command: exec.Command(bin, "mcp")})
	checkTools(t, stdio)
	same(t, "stdio MCP vs CLI", listJSON, toolText(t, stdio, "notes_list", map[string]any{"limit": 1}, false))
	errCode(t, "stdio MCP destructive apply without confirm",
		toolText(t, stdio, "note_delete", map[string]any{"id": "n1", "apply": true}, true), "confirmation_required")
	if got := string(toolText(t, stdio, "notes_sync", map[string]any{"fail": 503}, true)); got != unavailable {
		t.Errorf("stdio MCP provider error details: %s", got)
	}
	errCode(t, "stdio MCP cli-only input", toolText(t, stdio, "notes_export", map[string]any{"out": refused}, true), "cli_only")
	if _, err := os.Stat(refused); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a refused export wrote %s: %v", refused, err)
	}
	if out := toolText(t, stdio, "note_create", map[string]any{"title": "x"}, false); !strings.Contains(string(out), `"applied":false`) {
		t.Errorf("stdio MCP create without apply previews despite CLIImmediate: %s", out)
	}
	if out := toolText(t, stdio, "note_delete", map[string]any{"id": "n1"}, false); !strings.Contains(string(out), `"applied":false`) {
		t.Errorf("stdio MCP preview: %s", out)
	}
	if out := toolText(t, stdio, "note_delete", map[string]any{"id": "n1", "apply": true, "confirm": true}, false); !strings.Contains(string(out), `"applied":true`) {
		t.Errorf("stdio MCP apply with confirm: %s", out)
	}
	var page NotesPage
	if err := json.Unmarshal(toolText(t, stdio, "notes_list", map[string]any{}, false), &page); err != nil || len(page.Items) != 2 {
		t.Errorf("after the stdio delete: %+v, %v; want 2 notes", page, err)
	}
	if _, err := stdio.CallTool(ctx, &sdk.CallToolParams{Name: "note_rename", Arguments: map[string]any{}}); err == nil {
		t.Error("note_rename is not an MCP tool but could be called")
	}

	// Graceful shutdown removes the socket.
	if err := serve.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := serve.Wait(); err != nil {
		t.Errorf("serve exit: %v\n%s", err, serveErr.String())
	}
	if _, err := os.Stat(sock); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("socket still exists after shutdown: %v", err)
	}
}

func run(t *testing.T, bin string, want int, args ...string) []byte {
	t.Helper()
	out, _ := exec2(t, bin, want, args...)
	return out
}

func runErr(t *testing.T, bin string, want int, args ...string) []byte {
	t.Helper()
	_, stderr := exec2(t, bin, want, args...)
	return stderr
}

func exec2(t *testing.T, bin string, want int, args ...string) ([]byte, []byte) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	if code != want {
		t.Errorf("sample %s: exit %d, want %d\nstdout: %s\nstderr: %s", strings.Join(args, " "), code, want, stdout.Bytes(), stderr.Bytes())
	}
	return stdout.Bytes(), stderr.Bytes()
}

func same(t *testing.T, what string, want, got []byte) {
	t.Helper()
	if !toolkittest.SameJSON(want, got) {
		t.Errorf("%s differ:\nwant %s\ngot  %s", what, want, got)
	}
}

func errCode(t *testing.T, what string, body []byte, want string) {
	t.Helper()
	if got := toolkittest.ErrorCode(body); got != want {
		t.Errorf("%s: error code %q, want %q: %s", what, got, want, body)
	}
}

// waitForSocket polls until serve answers on sock, and returns an HTTP
// client that dials it.
func waitForSocket(t *testing.T, sock string, stderr *bytes.Buffer) *http.Client {
	t.Helper()
	hc := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		},
	}}
	deadline := time.Now().Add(15 * time.Second)
	for {
		resp, err := hc.Get("http://sample/ops")
		if err == nil {
			_ = resp.Body.Close()
			return hc
		}
		if time.Now().After(deadline) {
			t.Fatalf("serve did not start: %v\n%s", err, stderr.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func get(t *testing.T, hc *http.Client, path string, want int) []byte {
	t.Helper()
	resp, err := hc.Get("http://sample" + path)
	if err != nil {
		t.Fatal(err)
	}
	return body(t, resp, "GET "+path, want)
}

func post(t *testing.T, hc *http.Client, name, in string, want int) []byte {
	t.Helper()
	resp, err := hc.Post("http://sample/ops/"+name, "application/json", strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	return body(t, resp, "POST "+name+" "+in, want)
}

func body(t *testing.T, resp *http.Response, what string, want int) []byte {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != want {
		t.Errorf("%s: status %d, want %d: %s", what, resp.StatusCode, want, b)
	}
	return b
}

func connect(t *testing.T, tr sdk.Transport) *sdk.ClientSession {
	t.Helper()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "e2e", Version: "v0"}, nil).Connect(context.Background(), tr, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func checkTools(t *testing.T, cs *sdk.ClientSession) {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	reg, _ := New()
	toolkittest.CheckMCPTools(t, reg, res.Tools)
}

func toolText(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any, wantErr bool) []byte {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	if res.IsError != wantErr {
		t.Errorf("%s %v: isError %v, want %v: %s", name, args, res.IsError, wantErr, b.String())
	}
	return []byte(b.String())
}
