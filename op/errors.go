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
	KindError         Kind = "error"
	KindUsage         Kind = "usage"
	KindNotFound      Kind = "not_found"
	KindConflict      Kind = "conflict"
	KindDuplicateSend Kind = "duplicate_send"
	KindAuth          Kind = "auth"
	KindRate          Kind = "rate"
	KindTimeout       Kind = "timeout"
	KindStaleIndex    Kind = "stale_index"
	KindModelUnavail  Kind = "model_unavailable"
	KindPartial       Kind = "partial"
)

var kinds = map[Kind]struct {
	exit   int
	status int
}{
	KindError:         {output.ExitError, http.StatusInternalServerError},
	KindUsage:         {output.ExitUsage, http.StatusBadRequest},
	KindNotFound:      {output.ExitNotFound, http.StatusNotFound},
	KindConflict:      {output.ExitConflict, http.StatusConflict},
	KindDuplicateSend: {output.ExitConflict, http.StatusConflict},
	KindAuth:          {output.ExitAuth, http.StatusForbidden},
	KindRate:          {output.ExitRate, http.StatusTooManyRequests},
	KindTimeout:       {output.ExitTimeout, http.StatusGatewayTimeout},
	KindStaleIndex:    {output.ExitStaleIndex, http.StatusServiceUnavailable},
	KindModelUnavail:  {output.ExitModelUnavail, http.StatusServiceUnavailable},
	KindPartial:       {output.ExitPartial, http.StatusMultiStatus},
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
//
// A failure that came from a provider response may also carry the details a
// caller needs to classify it and schedule a retry: Retryable, HTTPStatus,
// RetryAfterSeconds and RequestID. Each is added to the CLI JSON envelope,
// the HTTP error body and the MCP error result only when set, under the same
// key on every surface, so an error without them is unchanged. Carry only
// sanitized values: they reach every caller.
type Error struct {
	Kind        Kind     `json:"-"`
	Code        string   `json:"code"`
	Message     string   `json:"message"`
	Suggestions []string `json:"suggestions,omitempty"`
	// Retryable overrides whether a caller may retry. Unset, the CLI
	// envelope derives it from the kind (rate and timeout are retryable) and
	// HTTP and MCP omit it. Set, every surface prints it, false included.
	Retryable *bool `json:"retryable,omitempty"`
	// HTTPStatus is the status of the failed provider response, such as 503.
	// It is not the status of the toolkit's own HTTP response, which follows
	// the kind.
	HTTPStatus int `json:"http_status,omitempty"`
	// RetryAfterSeconds is how long the provider asked the caller to wait.
	// The HTTP surface also sends it as a Retry-After header.
	RetryAfterSeconds int `json:"retry_after_seconds,omitempty"`
	// RequestID is the provider's identifier of the failed request.
	RequestID string `json:"request_id,omitempty"`
	// Result is output the operation produced before it failed, such as the
	// per-item report of a batch that stopped part way. It must have the
	// operation's output type. Every surface returns it with the error: the
	// CLI prints it to stdout (rendered or as JSON) and the error to stderr,
	// and HTTP and MCP add it to the error body as "result".
	Result any `json:"-"`
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
		kind := kindForExit(ce.ExitCode)
		if ce.ExitCode == output.ExitConflict && ce.Code == string(KindDuplicateSend) {
			kind = KindDuplicateSend
		}
		return &Error{Kind: kind, Code: ce.Code, Message: ce.Message, Suggestions: ce.Suggestions}
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
		if v.exit == code && k != KindDuplicateSend {
			return k
		}
	}
	return KindError
}

// CLIError converts the error to the shared output package's CLI error, so a
// tool's JSON error envelope and exit code stay as they are today. Its
// Retryable is the Retryable override, or else true for the rate and timeout
// kinds. The other provider details have no place in output.CLIError: the
// cli package adds them to the envelope it prints.
func (e *Error) CLIError() *output.CLIError {
	retryable := e.Kind == KindRate || e.Kind == KindTimeout
	if e.Retryable != nil {
		retryable = *e.Retryable
	}
	return &output.CLIError{Code: e.Code, Message: e.Message, Suggestions: e.Suggestions, ExitCode: e.Kind.ExitCode(),
		Retryable: retryable}
}

// Authorizer decides whether a remote caller may run an operation. The HTTP
// and HTTP-MCP surfaces consult it after input validation and confirmation.
type Authorizer interface {
	Authorize(ctx context.Context, e *Entry, req Request) error
}

// AuthorizerFunc adapts a function to Authorizer.
type AuthorizerFunc func(ctx context.Context, e *Entry, req Request) error

func (f AuthorizerFunc) Authorize(ctx context.Context, e *Entry, req Request) error {
	return f(ctx, e, req)
}

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
