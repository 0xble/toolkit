package toolkit

import (
	"context"
	"net"
	"os"

	"github.com/0xble/toolkit/op"
)

// tailscaleHeaders are the identity headers a Tailscale serve proxy adds. A
// request that carries any of them came through a proxy, not from a local
// process.
var tailscaleHeaders = []string{"Tailscale-User-Login", "Tailscale-User-Name", "Tailscale-App-Capabilities"}

type peerUIDKey struct{}

// withPeerUID is serve's http.Server.ConnContext: it records the uid of the
// process on the other end of a Unix socket connection. A connection whose
// peer cannot be read has no uid and is never treated as the owner.
func withPeerUID(ctx context.Context, c net.Conn) context.Context {
	uc, ok := c.(*net.UnixConn)
	if !ok {
		return ctx
	}
	uid, err := peerUID(uc)
	if err != nil {
		return ctx
	}
	return context.WithValue(ctx, peerUIDKey{}, uid)
}

// allowApply returns an authorizer that allows an applied call to one of the
// named operations from a local owner, and otherwise defers to base.
func allowApply(names map[string]bool, base op.Authorizer) op.Authorizer {
	if base == nil {
		base = op.DenyWrites
	}
	if len(names) == 0 {
		return base
	}
	uid := os.Getuid()
	return op.AuthorizerFunc(func(ctx context.Context, e *op.Entry, req op.Request) error {
		if req.Apply && names[e.Name] && localOwner(req, uid) {
			return nil
		}
		return base.Authorize(ctx, e, req)
	})
}

// localOwner reports whether req is an HTTP API call that arrived on the
// Unix socket from a process running as uid, and not through a Tailscale
// proxy.
func localOwner(req op.Request, uid int) bool {
	if req.Surface != op.SurfaceHTTP || req.HTTP == nil {
		return false
	}
	for _, h := range tailscaleHeaders {
		if len(req.HTTP.Header.Values(h)) > 0 {
			return false
		}
	}
	peer, ok := req.HTTP.Context().Value(peerUIDKey{}).(int)
	return ok && peer == uid
}

// parseAllowApply checks --allow-apply names against the registry: each must
// be a write or destructive operation.
func parseAllowApply(reg *op.Registry, names []string) (map[string]bool, error) {
	set := map[string]bool{}
	for _, name := range names {
		e := reg.Lookup(name)
		if e == nil {
			return nil, &op.Error{Kind: op.KindUsage, Code: "invalid_allow_apply",
				Message:     "--allow-apply: no operation named " + name,
				Suggestions: []string{"run " + reg.Tool + " metadata to list the operations"}}
		}
		if !e.Effect.Mutates() {
			return nil, &op.Error{Kind: op.KindUsage, Code: "invalid_allow_apply",
				Message:     "--allow-apply: " + name + " is a read operation and never applies",
				Suggestions: []string{"list only write or destructive operations"}}
		}
		set[name] = true
	}
	return set, nil
}
