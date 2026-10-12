// Package api serves an operation registry over HTTP:
//
//	GET  /ops           the registry metadata (toolkit.metadata.v1)
//	POST /ops/{name}    run one operation with a JSON input
//	GET  /openapi.json  an OpenAPI 3.1 document of the routes
//
// Every operation is a POST so structured input keeps its body; the effect,
// not the HTTP verb, says whether it changes state. A success returns the
// operation output as the body. A failure returns {"error": {code, message,
// suggestions}} with the status of the error kind, plus the provider details
// of op.Error that are set.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/0xble/toolkit/op"
)

// MaxBodyBytes bounds a request body.
const MaxBodyBytes = 1 << 20

// Options configures the HTTP surface.
type Options struct {
	// Authorizer decides whether a caller may run an operation. Nil means
	// op.DenyWrites: reads and previews are allowed, applied writes are not.
	Authorizer op.Authorizer
}

// currentJSON encodes the document from the registry's current entries. Each
// entry's schema JSON is cached, so this marshals metadata, not schemas.
func currentJSON(reg *op.Registry) []byte {
	b, err := json.Marshal(buildOpenAPI(reg, true))
	if err != nil {
		panic(fmt.Sprintf("encode OpenAPI document: %v", err))
	}
	return b
}

// Handler returns the HTTP handler for reg. Mount MCP at /mcp next to it if
// the tool serves MCP over HTTP.
func Handler(reg *op.Registry, opts Options) http.Handler {
	auth := opts.Authorizer
	if auth == nil {
		auth = op.DenyWrites
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ops", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, reg.Metadata())
	})
	// The document is built on request from current registry metadata. Schema
	// copies are cached per entry, but mutable operation metadata is fresh.
	mux.HandleFunc("GET /openapi.json", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONBytes(w, http.StatusOK, currentJSON(reg))
	})
	mux.HandleFunc("POST /ops/{name}", func(w http.ResponseWriter, r *http.Request) {
		e := reg.Lookup(r.PathValue("name"))
		if e == nil {
			WriteError(w, op.Errorf(op.KindNotFound, "unknown_operation", "no operation named %q", r.PathValue("name")))
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
		if err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				WriteError(w, op.Errorf(op.KindUsage, "input_too_large", "request body exceeds %d bytes", MaxBodyBytes))
				return
			}
			WriteError(w, op.Errorf(op.KindUsage, "invalid_input", "read request body: %v", err))
			return
		}
		out, err := e.CallJSON(r.Context(), op.Request{Surface: op.SurfaceHTTP, HTTP: r}, raw, auth)
		if err != nil {
			WriteError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, op.Errorf(op.KindNotFound, "unknown_route", "no route %s %s", r.Method, r.URL.Path))
	})
	return mux
}

// ErrorBody is the JSON body of a failed call. Result is the operation's
// partial output when the error carries one (op.Error.Result).
type ErrorBody struct {
	Error  *op.Error `json:"error"`
	Result any       `json:"result,omitempty"`
}

// WriteError writes err as an ErrorBody with the status of its kind. An
// error with RetryAfterSeconds also gets a Retry-After header.
func WriteError(w http.ResponseWriter, err error) {
	oe := op.AsError(err, op.KindError)
	if oe.RetryAfterSeconds > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(oe.RetryAfterSeconds))
	}
	writeJSON(w, oe.Kind.HTTPStatus(), ErrorBody{Error: oe, Result: oe.Result})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		status = http.StatusInternalServerError
		b, _ = json.Marshal(ErrorBody{Error: op.Errorf(op.KindError, "encode_failed", "encode response: %v", err)})
	}
	writeJSONBytes(w, status, b)
}

func writeJSONBytes(w http.ResponseWriter, status int, b []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(b)
	_, _ = w.Write([]byte{'\n'})
}
