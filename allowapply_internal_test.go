package toolkit

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/0xble/toolkit/op"
)

func TestAllowApplyAuthorizer(t *testing.T) {
	reg := op.New("t", "v")
	h := func(context.Context, op.Request, struct{}) (struct{}, error) { return struct{}{}, nil }
	op.Add(reg, op.Op[struct{}, struct{}]{Name: "thing.make", Effect: op.Write, Handler: h})
	op.Add(reg, op.Op[struct{}, struct{}]{Name: "thing.other", Effect: op.Write, Handler: h})
	listed, other := reg.Lookup("thing.make"), reg.Lookup("thing.other")

	req := func(surface op.Surface, uid *int) op.Request {
		r := httptest.NewRequest("POST", "/ops/thing.make", nil)
		if uid != nil {
			r = r.WithContext(context.WithValue(r.Context(), peerUIDKey{}, *uid))
		}
		return op.Request{Surface: surface, Apply: true, HTTP: r}
	}
	self, stranger := os.Getuid(), os.Getuid()+1
	auth := allowApply(map[string]bool{"thing.make": true}, nil)
	for what, c := range map[string]struct {
		e   *op.Entry
		req op.Request
		ok  bool
	}{
		"same uid":         {listed, req(op.SurfaceHTTP, &self), true},
		"other uid":        {listed, req(op.SurfaceHTTP, &stranger), false},
		"no peer uid":      {listed, req(op.SurfaceHTTP, nil), false},
		"HTTP MCP":         {listed, req(op.SurfaceMCP, &self), false},
		"no HTTP request":  {listed, op.Request{Surface: op.SurfaceHTTP, Apply: true}, false},
		"unlisted":         {other, req(op.SurfaceHTTP, &self), false},
		"preview of other": {other, op.Request{Surface: op.SurfaceHTTP}, true},
	} {
		if err := auth.Authorize(context.Background(), c.e, c.req); (err == nil) != c.ok {
			t.Errorf("%s: %v", what, err)
		}
	}

	// A tool's own authorizer still decides everything the flag does not allow.
	toolAuth := op.AuthorizerFunc(func(_ context.Context, e *op.Entry, _ op.Request) error {
		if e.Name == "thing.other" {
			return nil
		}
		return errors.New("tool says no")
	})
	auth = allowApply(map[string]bool{"thing.make": true}, toolAuth)
	if err := auth.Authorize(context.Background(), other, req(op.SurfaceMCP, nil)); err != nil {
		t.Errorf("the tool authorizer allows thing.other: %v", err)
	}
	if err := auth.Authorize(context.Background(), listed, req(op.SurfaceHTTP, &self)); err != nil {
		t.Errorf("the flag allows thing.make from the owner: %v", err)
	}
	if err := auth.Authorize(context.Background(), listed, req(op.SurfaceHTTP, &stranger)); err == nil {
		t.Error("a stranger falls through to the tool authorizer, which refuses")
	}
}
