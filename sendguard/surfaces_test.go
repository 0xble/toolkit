package sendguard_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/0xble/toolkit/api"
	"github.com/0xble/toolkit/cli"
	"github.com/0xble/toolkit/op"
	"github.com/0xble/toolkit/output"
	"github.com/0xble/toolkit/sendguard"
	"github.com/0xble/toolkit/toolkittest"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestDuplicateAcrossSurfaces(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	type input struct {
		Body string `json:"body"`
	}
	type result struct {
		Sent bool `json:"sent"`
	}
	reg := op.New("messenger", "test")
	sends := 0
	op.Add(reg, op.Op[input, result]{Name: "message.send", Effect: op.Write, MCP: true,
		Handler: func(ctx context.Context, req op.Request, in input) (result, error) {
			if !req.Apply {
				return result{}, nil
			}
			claim, err := sendguard.Claim(ctx, sendguard.Key{Tool: "messenger", Account: "account", Target: "target", Body: in.Body}, time.Minute)
			if err != nil {
				return result{}, err
			}
			sends++
			return result{Sent: true}, claim.Sent()
		},
	})
	ctx := context.Background()
	var stdout, stderr bytes.Buffer
	code := cli.Run(ctx, reg, cli.Options{Stdout: &stdout, Stderr: &stderr}, []string{"--json", "message", "send", "--body", "hello world", "--apply"})
	if code != 0 || sends != 1 {
		t.Fatalf("first send: code=%d sends=%d stderr=%s", code, sends, &stderr)
	}
	stdout.Reset()
	stderr.Reset()
	code = cli.Run(ctx, reg, cli.Options{Stdout: &stdout, Stderr: &stderr}, []string{"--json", "message", "send", "--body", "hello\tworld", "--apply"})
	if code != output.ExitConflict || toolkittest.ErrorCode(stderr.Bytes()) != "duplicate_send" {
		t.Fatalf("CLI duplicate: code=%d stderr=%s", code, &stderr)
	}
	w := httptest.NewRecorder()
	api.Handler(reg, api.Options{Authorizer: op.AllowAll}).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/ops/message.send", strings.NewReader(`{"body":"hello world","apply":true}`)))
	if w.Code != http.StatusConflict || toolkittest.ErrorCode(w.Body.Bytes()) != "duplicate_send" {
		t.Fatalf("HTTP duplicate: status=%d body=%s", w.Code, w.Body)
	}
	res, err := toolkittest.MCPClient(t, reg, op.AllowAll).CallTool(ctx, &sdk.CallToolParams{Name: "message_send", Arguments: map[string]any{"body": "hello world", "apply": true}})
	if err != nil || !res.IsError || toolkittest.ErrorCode([]byte(res.Content[0].(*sdk.TextContent).Text)) != "duplicate_send" {
		t.Fatalf("MCP duplicate: result=%+v error=%v", res, err)
	}
	if sends != 1 {
		t.Fatalf("duplicate reached provider: sends=%d", sends)
	}
}

func TestExistingCLIConflictAndDuplicateRoundtrip(t *testing.T) {
	for _, test := range []struct {
		code string
		kind op.Kind
	}{{"already_exists", op.KindConflict}, {"duplicate_send", op.KindDuplicateSend}} {
		for range 100 {
			mapped := op.AsError(output.ErrWithExit(test.code, "blocked", output.ExitConflict), op.KindError)
			if mapped.Kind != test.kind {
				t.Fatalf("code=%q mapped=%s want=%s", test.code, mapped.Kind, test.kind)
			}
		}
	}
}
