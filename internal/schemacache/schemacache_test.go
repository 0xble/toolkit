package schemacache_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/0xble/toolkit/api"
	"github.com/0xble/toolkit/internal/schemacache"
	"github.com/0xble/toolkit/mcp"
	"github.com/0xble/toolkit/op"
)

// Leave every registry, server, handler, and entry reference in this helper's
// frame. The test also checks eviction, not only entry collection: a weak key
// with no cleanup must fail rather than leaking the schema values forever.
func populateTransportCache(t *testing.T) {
	t.Helper()
	type input struct {
		Name string `json:"name"`
	}
	type output struct {
		OK bool `json:"ok"`
	}
	r := op.New("cache-lifetime", "v1")
	op.Add(r, op.Op[input, output]{
		Name: "test.read", Effect: op.Read, MCP: true,
		Handler: func(context.Context, op.Request, input) (output, error) {
			return output{OK: true}, nil
		},
	})
	server := mcp.NewServer(r, op.AllowAll, nil)
	handler := api.Handler(r, api.Options{})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /openapi.json: status %d", w.Code)
	}
	if got := schemacache.SizeForTest(); got != 1 {
		t.Fatalf("populated cache size = %d, want 1", got)
	}
	runtime.KeepAlive(server)
	runtime.KeepAlive(handler)
	runtime.KeepAlive(r)
}

func TestTransportCacheReleasesFreedEntry(t *testing.T) {
	populateTransportCache(t)
	for range 100 {
		runtime.GC()
		if schemacache.SizeForTest() == 0 {
			return
		}
		// GC schedules cleanup asynchronously; yield between bounded attempts.
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("per-entry cache still has %d entries after their registry and transports were freed", schemacache.SizeForTest())
}
