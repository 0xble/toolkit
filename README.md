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
- **Immediate CLI apply.** `CLIImmediate: true` on a write or destructive
  operation makes the CLI apply without `--apply`, for clearly scoped
  mutations such as `player pause` or `message send`. The command gains
  `--dry-run` to preview instead, and still accepts `--apply` as a hidden
  no-op, so existing callers keep working. Passing both is a usage error. A
  destructive operation still needs `--yes` or a confirmed prompt. HTTP and
  MCP are unchanged: they apply only with `"apply": true`, plus
  `"confirm": true` when destructive, and `serve`'s default authorizer still
  refuses applied writes. The flag shows as `cli_immediate` in the metadata,
  `x-cli-immediate` in OpenAPI and the `toolkit/cli_immediate` key of the MCP
  tool's `_meta`. Without it nothing changes.
- **CLI path.** `note <id> delete` mounts the command under `note`, with `<id>`
  a positional argument that fills the input field whose json name is `id`.
- **Default commands.** `DefaultCommand: true` makes the last CLI word the
  default subcommand of its parent: `items list` also runs as `items`, and
  `item <id> show` as `item <id>`. The word keeps working and is hidden from
  help. Tools that grew such commands under kong keep their spelling.
- **Aliases.** `Aliases: []string{"s"}` gives the last CLI word extra kong
  aliases, so `search <query>` also runs as `s <query>`. They are CLI-only:
  HTTP routes, MCP names and the operation name are unchanged. The registry
  rejects an alias that collides with another command's path, and the
  metadata lists them in an optional `aliases` field.
- **Root flags.** A tool can declare root flags (for example `--limit` or
  `--account`) whose json tag names an input field. They fill that field in
  every operation that has it, so `tool --limit 5 notes list` and
  `tool notes list --limit 5` are the same call.
- **Output.** `--json` or `--agent` prints JSON. Otherwise the CLI uses the
  operation's `Render` hook if it has one, and JSON if not. A hook whose text
  depends on the call, such as a next-page hint that repeats `--limit`, can
  be `RenderWithInput: func(w io.Writer, in In, out Out) error` instead, and
  receives the decoded input. An operation sets at most one of the two.
- **Pages and `--fields`.** `--fields a,b` keeps those top-level keys of a
  JSON result, or of each element of an array result. An operation whose
  output is a page, an object with an `items` array next to envelope keys
  such as `account`, `next_cursor` and `has_more`, sets `Paged: true`:
  `--fields` then keeps the named keys of each item and every envelope key,
  so `meetings --fields id,title` lists each meeting's id and title and still
  says whether more pages exist. `Paged` is opt-in so that a tool whose
  callers already name top-level keys, such as `--fields items,has_more`,
  keeps its output. It is CLI-only: HTTP and MCP return the whole result.
- **Warnings.** An operation that carries warnings in its result can declare
  `Warnings: func(out Out) []string`. In human output the CLI prints each to
  stderr as `warning: <text>` before the result (or the error, for an error
  result). `--json`, `--agent`, HTTP and MCP print nothing extra: callers read
  the warnings in the result.
- **Any JSON output.** An operation whose output is `any` or
  `json.RawMessage` (a raw API passthrough, say) may return any JSON value.
  Its output schema lists every JSON type,
  `{"type": ["object", "array", "string", "number", "boolean", "null"]}`, and
  a `json.RawMessage` field gets the same schema at any depth. HTTP returns
  the value as the body. MCP structured content must be an object, so such a
  tool has no `outputSchema` and its result is the JSON text alone. An output
  of `map[string]any` is always an object and keeps structured content.
- **Errors.** Handlers return `*op.Error` with a kind. The kind maps to the
  exit code, the HTTP status and an MCP `isError` result, each carrying the
  same `{code, message, suggestions}` payload. An error may carry the output
  produced before the failure in `Error.Result` (for example the report of a
  batch that stopped part way): the CLI prints it to stdout and the error to
  stderr, and HTTP and MCP add it to the error body as `"result"`.

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
destructive operations, an immediate CLI write, root flags and render hooks,
plus the conformance and end-to-end tests that drive its real binary over
every surface.

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
`apply`. For an operation with `CLIImmediate` it previews the CLI with
`--dry-run`, checks that the CLI applies without `--apply` while HTTP and MCP
still preview without `apply`, and checks that the OpenAPI and MCP documents
carry the flag. Case `Args` never include `--apply` or `--dry-run`.

## Tool CI

toolkit publishes the CI a tool repository runs, as two reusable workflows
and a template:

```text
templates/tool/
├── bin/ci                      preflight | gate [sha] | nightly [sha]
└── .github/workflows/
    ├── gate.yml                calls tool-gate.yml@<tag>, plus `qualification`
    └── nightly.yml             calls tool-nightly.yml@<tag>
.github/workflows/
├── tool-gate.yml               exact-SHA gate and its qualification job
└── tool-nightly.yml            ./bin/ci nightly on the scheduled commit
```

Copy `templates/tool` into the tool's root and pin the workflows to a toolkit
tag. The tool's `bin/ci gate` runs `./bin/check` (the rendered fleet policy),
`go test -race ./...` and `golangci-lint`, with `GOWORK=off`. `nightly` adds
five repeated race runs and `govulncheck`. `preflight` is quick local feedback.

`tool-gate.yml` checks out the pull request's head SHA, never a merge commit,
asserts it, runs `./bin/ci gate <sha>` and asserts the tree is unchanged after.
Its `qualification` job fails when the gate lane did not succeed. A draft pull
request skips the lane and fails with `draft: gate not run`, except a Mergify
merge-queue batch draft from a `mergify/merge-queue/` branch, which runs the
full gate. GitHub reports a called workflow's jobs as `gate / qualification`,
so the caller keeps its own `qualification` job, the one branch protection
requires.

| Input | Default | Purpose |
| --- | --- | --- |
| `go-version-file` | `go.mod` | Go version for `actions/setup-go` |
| `runner` | `vars.CI_RUNNER`, then `ubuntu-24.04` | Gate runner. The nightly uses `vars.CI_NIGHTLY_RUNNER` |
| `ci-image-dir` | empty | Directory with the Dockerfile of an execution image, for example `ci` |
| `golangci-lint-version` | `v2.13.2` | Installed when no image is used |
| `govulncheck-version` | `v1.8.0` | Nightly only, installed when no image is used |

Without `ci-image-dir`, `bin/ci` runs on the runner after `actions/setup-go`.
With it, `bin/ci` runs inside an image built from that directory. A pull
request builds the base SHA's copy, never its own, so a Dockerfile change takes
effect once it lands, and the directory must land before a caller sets
`ci-image-dir`. The image is tagged with the directory's Git tree SHA. On
Namespace runners it is pulled from the workspace registry and built only on a
miss, and a push to the default branch publishes it. toolkit's own gate and
nightly call these workflows by local path, with `ci-image-dir: ci`.

`bin/ci gate <sha>` writes an optional local readiness receipt unless `CI=true`
or `CI_SKIP_LOCAL_RECEIPT=true`, so a wrapper that writes its own receipt can
call it.

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
