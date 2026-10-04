# toolkit

Declare each operation of a command-line tool once, as typed Go, and get a
[kong](https://github.com/alecthomas/kong) CLI, an HTTP API with an OpenAPI 3.1
document, and [MCP](https://modelcontextprotocol.io) tools from that one
declaration, all with the same validation, safety rules and errors.

```text
op.Add(reg, op.Op[In, Out]{...})
├→ tool note <id> delete --apply --yes     CLI
├→ POST /ops/note.delete                   HTTP on a Unix socket
├→ GET  /openapi.json                      OpenAPI 3.1
├→ tools/call note_delete                  MCP, stdio or HTTP
└→ tool metadata --json                    toolkit.metadata.v1
```

## Packages

| Package | Purpose |
| --- | --- |
| `op` | The operation registry, JSON Schemas, error kinds and authorization |
| `cli` | The kong adapter |
| `api` | HTTP routes and the OpenAPI document |
| `mcp` | MCP tools over stdio and streamable HTTP |
| `output` | JSON and table output, error envelope and exit codes |
| `toolkit` | `toolkit.Main`, plus the built-in `serve`, `mcp` and `metadata` commands |
| `toolkittest` | Conformance checks a tool runs from its own tests |

## The Operation Model

An operation has a dotted name, a typed input struct, a typed output, an
effect and a handler. Input fields carry both their wire shape (`json` tags)
and their CLI shape (kong tags such as `arg`, `help` and `default`), so a
field is declared once.

- **Effect.** `read`, `write` or `destructive`. Write and destructive
  operations get a standard `apply` input: without it the handler must only
  preview (`--apply` on the CLI, `"apply": true` on HTTP and MCP).
  Destructive operations also need `confirm` to apply (`--yes` or an
  interactive prompt on the CLI, `"confirm": true` on HTTP and MCP). A preview
  never needs `confirm`. The registry enforces this before any handler runs.
- **CLI path.** `note <id> delete` mounts the command under `note`, with `<id>`
  a positional argument that fills the input field whose json name is `id`.
- **Root flags.** A tool can declare root flags (for example `--limit` or
  `--account`) whose json tag names an input field. They fill that field in
  every operation that has it, so `tool --limit 5 notes list` and
  `tool notes list --limit 5` are the same call.
- **Output.** `--json` or `--agent` prints JSON. Otherwise the CLI uses the
  operation's `Render` hook if it has one, and JSON if not.
- **Errors.** Handlers return `*op.Error` with a kind. The kind maps to the
  exit code, the HTTP status and an MCP `isError` result, each carrying the
  same `{code, message, suggestions}` payload.

| Kind | Exit | HTTP |
| --- | --- | --- |
| (success) | 0 | 200 |
| `error` | 1 | 500 |
| `usage` | 2 | 400 |
| `not_found` | 3 | 404 |
| `conflict` | 4 | 409 |
| `auth` | 5 | 403 |
| `rate` | 6 | 429 |
| `timeout` | 7 | 504 |
| `stale_index` | 8 | 503 |
| `model_unavailable` | 9 | 503 |
| `partial` | 10 | 207 |

- **Serving.** `tool serve --socket PATH` listens on a Unix socket created
  with mode `0600` and serves `GET /ops`, `POST /ops/{name}`,
  `GET /openapi.json` and `/mcp`. `tool mcp` serves MCP over stdio. Nothing
  needs root.
- **Authorization.** Served surfaces consult an `op.Authorizer`. The default,
  `op.DenyWrites`, allows reads and previews and refuses applied writes. The
  CLI and stdio MCP run as the local user and allow writes.
- **MCP exposure.** Opt in per operation with `MCP: true`. Tool names replace
  `.` with `_`.

## Example

```go
package main

import (
	"context"

	"github.com/0xble/toolkit"
	"github.com/0xble/toolkit/op"
)

type DeleteInput struct {
	ID string `json:"id" help:"Note ID"`
}

type Result struct {
	Applied bool `json:"applied"`
}

func main() {
	reg := op.New("notes", "v0.1.0")
	op.Add(reg, op.Op[DeleteInput, Result]{
		Name:    "note.delete",
		CLI:     "note <id> delete",
		Summary: "Delete a note",
		Effect:  op.Destructive,
		MCP:     true,
		Handler: func(ctx context.Context, req op.Request, in DeleteInput) (Result, error) {
			if !req.Apply {
				return Result{}, nil // preview
			}
			// delete in.ID here
			return Result{Applied: true}, nil
		},
	})
	toolkit.Main(reg, toolkit.Options{Description: "Notes."})
}
```

```text
notes note n1 delete                    # preview
notes note n1 delete --apply --yes      # delete
notes serve --socket ~/.local/state/notes.sock
notes mcp
notes metadata --json
```

[`examples/sample`](examples/sample) is a complete tool with read, write and
destructive operations, root flags and render hooks, plus the conformance and
end-to-end tests that drive its real binary over every surface.

## Conformance

```go
func TestConformance(t *testing.T) {
	toolkittest.Run(t, toolkittest.Suite{
		New: func(testing.TB) toolkittest.Fixture {
			reg, store := newRegistry()
			return toolkittest.Fixture{Registry: reg, State: func() any { return store.Snapshot() }}
		},
		Cases: map[string]toolkittest.Case{
			"note.delete": {Input: map[string]any{"id": "n1"}, Args: []string{"note", "n1", "delete"}},
		},
	})
}
```

It checks that the metadata is valid `toolkit.metadata.v1`, that MCP
`tools/list` and the OpenAPI document match the registry, that the CLI, HTTP
and MCP return the same output, that destructive operations refuse to apply
without `confirm` on every surface, and that writes change nothing without
`apply`.

## Stability

toolkit is pre-1.0. Minor versions may break the API. Pin a tag
(`go get github.com/0xble/toolkit@v0.1.0`) and upgrade deliberately.

## Development

```sh
./bin/ci preflight   # quick local checks
./bin/ci gate        # what the required qualification check runs
```

## License

[MIT](LICENSE)
