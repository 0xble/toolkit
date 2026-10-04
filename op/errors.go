package op

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/0xble/toolkit/output"
)

// Kind is the surface-neutral error class. The CLI maps it to the family exit
// code, HTTP to a status, and MCP to an isError result.
type Kind string

const (
	KindError        Kind = "error"
	KindUsage        Kind = "usage"
	KindNotFound     Kind = "not_found"
	KindConflict     Kind = "conflict"
	KindAuth         Kind = "auth"
	KindRate         Kind = "rate"
	KindTimeout      Kind = "timeout"
	KindStaleIndex   Kind = "stale_index"
	KindModelUnavail Kind = "model_unavailable"
	KindPartial      Kind = "partial"
)

var kinds = map[Kind]struct {
	exit   int
	status int
}{
	KindError:        {output.ExitError, http.StatusInternalServerError},
	KindUsage:        {output.ExitUsage, http.StatusBadRequest},
	KindNotFound:     {output.ExitNotFound, http.StatusNotFound},
	KindConflict:     {output.ExitConflict, http.StatusConflict},
	KindAuth:         {output.ExitAuth, http.StatusForbidden},
	KindRate:         {output.ExitRate, http.StatusTooManyRequests},
	KindTimeout:      {output.ExitTimeout, http.StatusGatewayTimeout},
	KindStaleIndex:   {output.ExitStaleIndex, http.StatusServiceUnavailable},
	KindModelUnavail: {output.ExitModelUnavail, http.StatusServiceUnavailable},
	KindPartial:      {output.ExitPartial, http.StatusMultiStatus},
}

// ExitCode is the CLI exit code for the kind. Unknown kinds map to 1.
func (k Kind) ExitCode() int {
	if v, ok := kinds[k]; ok {
		return v.exit
	}
	return output.ExitError
}

// HTTPStatus is the HTTP status for the kind. Unknown kinds map to 500.
func (k Kind) HTTPStatus() int {
	if v, ok := kinds[k]; ok {
		return v.status
	}
	return http.StatusInternalServerError
}

// Error is the one error type operations return. Its wire form,
// {code, message, suggestions}, is the same on every surface.
type Error struct {
	Kind        Kind     `json:"-"`
	Code        string   `json:"code"`
	Message     string   `json:"message"`
	Suggestions []string `json:"suggestions,omitempty"`
}

func (e *Error) Error() string { return e.Message }

// Errorf builds an Error with a formatted message.
func Errorf(kind Kind, code, format string, args ...any) *Error {
	return &Error{Kind: kind, Code: code, Message: fmt.Sprintf(format, args...)}
}

// AsError returns err as an *Error. Errors that are not already an *Error
// get the fallback kind, and context deadlines map to KindTimeout.
func AsError(err error, fallback Kind) *Error {
	if err == nil {
		return nil
	}
	var oe *Error
	if errors.As(err, &oe) {
		return oe
	}
	var ce *output.CLIError
	if errors.As(err, &ce) {
		return &Error{Kind: kindForExit(ce.ExitCode), Code: ce.Code, Message: ce.Message, Suggestions: ce.Suggestions}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &Error{Kind: KindTimeout, Code: "timeout", Message: err.Error()}
	}
	code := "error"
	if fallback != KindError {
		code = string(fallback)
	}
	return &Error{Kind: fallback, Code: code, Message: err.Error()}
}

func kindForExit(code int) Kind {
	for k, v := range kinds {
		if v.exit == code {
			return k
		}
	}
	return KindError
}

// CLIError converts the error to the shared output package's CLI error, so a
// tool's JSON error envelope and exit code stay as they are today.
func (e *Error) CLIError() *output.CLIError {
	return &output.CLIError{Code: e.Code, Message: e.Message, Suggestions: e.Suggestions, ExitCode: e.Kind.ExitCode(),
		Retryable: e.Kind == KindRate || e.Kind == KindTimeout}
}

// Authorizer decides whether a remote caller may run an operation. The HTTP
// and HTTP-MCP surfaces consult it after input validation and confirmation.
type Authorizer interface {
	Authorize(ctx context.Context, e *Entry, req Request) error
}

// AuthorizerFunc adapts a function to Authorizer.
type AuthorizerFunc func(ctx context.Context, e *Entry, req Request) error

func (f AuthorizerFunc) Authorize(ctx context.Context, e *Entry, req Request) error { return f(ctx, e, req) }

// DenyWrites allows reads and previews, and refuses any call that would apply
// a write or destructive change. It is the default for served surfaces.
var DenyWrites Authorizer = AuthorizerFunc(func(_ context.Context, e *Entry, req Request) error {
	if req.Apply && e.Effect.Mutates() {
		return &Error{Kind: KindAuth, Code: "write_not_authorized",
			Message:     e.Name + " would change state and this caller is not authorized to apply writes",
			Suggestions: []string{"preview without apply, or run the CLI on the host"}}
	}
	return nil
})

// AllowAll allows every call. Local surfaces (the CLI and stdio MCP) use it:
// their caller already runs the binary with the user's credentials.
var AllowAll Authorizer = AuthorizerFunc(func(context.Context, *Entry, Request) error { return nil })
