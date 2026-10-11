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
| `sendguard` | Process-safe duplicate-send claims with local and remote history checks |
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
- **Command line as confirmation.** `CLIConfirmed: true` on a destructive
  operation with `CLIImmediate` makes the CLI apply it without `--yes` or a
  prompt: typing the command is the confirmation, as for `message send`,
  which cannot be undone but whose callers never pass `--yes`. `--dry-run`
  still previews. HTTP and MCP are unchanged: they apply only with
  `"apply": true` and `"confirm": true`. `Add` rejects it on any other
  operation. The flag shows as `cli_confirmed` in the metadata,
  `x-cli-confirmed` in OpenAPI and the `toolkit/cli_confirmed` key of the MCP
  tool's `_meta`.
- **CLI-only inputs.** An input field tagged `toolkit:"cli-only"`, as in
  ``Out string `json:"out,omitempty" toolkit:"cli-only"` ``, is accepted only
  on the command line. Use it for a flag or argument that names a local file
  to read or write: a served tool must not touch arbitrary paths for a
  remote caller. The OpenAPI request schema, the MCP tool schema and the
  metadata `input` omit the field, the metadata lists it in an optional
  `cli_only_inputs` array, and the registry refuses an HTTP or MCP call that
  sets it with the usage error `cli_only` (`--out names a local path and is
  accepted only on the command line`) before confirmation, authorization or
  the handler. The CLI is unchanged. The field must be optional (`omitempty`)
  and have no `default`, or `op.Add` panics.
- **CLI path.** `note <id> delete` mounts the command under `note`, with `<id>`
  a positional argument that fills the input field whose json name is `id`.
- **Default commands.** `DefaultCommand: true` makes the last CLI word the
  default subcommand of its parent: `items list` also runs as `items`, and
  `item <id> show` as `item <id>`. The word keeps working and is hidden from
  help, unless it is its parent's only command: then help lists it, as
  `account show`, since kong's help lists only leaf commands and hiding it
  would hide the whole group. Tools that grew such commands under kong keep
  their spelling.
- **Aliases.** `Aliases: []string{"s"}` gives the last CLI word extra kong
  aliases, so `search <query>` also runs as `s <query>`. They are CLI-only:
  HTTP routes, MCP names and the operation name are unchanged. The registry
  rejects an alias that collides with another command's path, and the
  metadata lists them in an optional `aliases` field.
- **Root flags.** A tool can declare root flags (for example `--limit` or
  `--account`) whose json tag names an input field. They fill that field in
  every operation that has it, so `tool --limit 5 notes list` and
  `tool notes list --limit 5` are the same call.
- **Explicit values.** A `default` tag fills only an input the caller left
  out. `--level 0`, `--enabled=false` and `--label ""` reach the handler as
  given, as `"level": 0` does over HTTP and MCP, even on an `omitempty`
  field.
- **Negative numbers.** An argument shaped like a negative number (`-40`,
  `-0.5`, `-1e3`) is a value, not a flag, so `temp -40 --side right` and
  `--min -5` need neither `--` nor `=`. kong still decides whether it fills a
  flag or a positional argument. A digit declared as a short flag anywhere in
  the tool, such as `-1`, stays a flag. Other arguments that start with `-`
  are unchanged: a string such as `-foo` still needs `--`.
- **Reserved flags.** kong parses a command flag that reuses a root flag's
  name, alias or short form as the root flag, so the command never sees it:
  an input field named `version` would print the tool's version instead.
  `cli.Validate`, and so the conformance kit, rejects such a flag, whether
  it shadows a toolkit flag (`--json`/`-j`, `--agent`, `--fields`,
  `--yes`/`-y`, `--version`, `--help`/`-h`) or one of the tool's own root
  flags, and rejects an operation input flag named `--apply` or `--dry-run`.
  Rename it with a kong `name` tag, as in `name:"at-version"`. It is not
  overridable. `Run` does not repeat the check, so a binary that already
  ships such a flag keeps running.
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
| `duplicate_send` | 4 | 409 |
| `auth` | 5 | 403 |
| `rate` | 6 | 429 |
| `timeout` | 7 | 504 |
| `stale_index` | 8 | 503 |
| `model_unavailable` | 9 | 503 |
| `partial` | 10 | 207 |

- **Provider error details.** A failure that came from a provider response
  can say how to retry. `op.Error` has optional `Retryable` (a `*bool`),
  `HTTPStatus` (the provider's status, not the toolkit's), `RetryAfterSeconds`
  and `RequestID`. Each is printed only when set, as `retryable`,
  `http_status`, `retry_after_seconds` and `request_id`, in the CLI JSON
  envelope, the HTTP error body and the MCP error text alike, so an error
  without them prints exactly what it printed before. The CLI envelope has
  always derived `retryable: true` for the `rate` and `timeout` kinds and
  still does. `Retryable` overrides that everywhere, `false` included. HTTP
  also sends `RetryAfterSeconds` as a `Retry-After` header. Carry only
  sanitized values.

  ```json
  {"error":{"code":"provider_unavailable","message":"upstream answered 503","exit_code":1,"retryable":true,"http_status":503,"request_id":"req_sample"}}
  ```

- **Serving.** `tool serve --socket PATH` listens on a Unix socket created
  with mode `0600` and serves `GET /ops`, `POST /ops/{name}`,
  `GET /openapi.json` and `/mcp`. `tool mcp` serves MCP over stdio. Nothing
  needs root.
- **Authorization.** Served surfaces consult an `op.Authorizer`. The default,
  `op.DenyWrites`, allows reads and previews and refuses applied writes. The
  CLI and stdio MCP run as the local user and allow writes.
- **Local apply.** `tool serve --socket PATH --allow-apply=OP[,OP...]`
  (repeatable) lets a local process apply the listed write or destructive
  operations over the socket. An applied call to a listed operation is
  allowed only when all hold: it is an HTTP API call (`POST /ops/{name}`, not
  `/mcp`), the peer process on the Unix socket runs as the server's uid (read
  with `LOCAL_PEERCRED` on macOS and `SO_PEERCRED` on Linux; other platforms
  never match), and the request has no Tailscale identity header
  (`Tailscale-User-Login`, `Tailscale-User-Name`,
  `Tailscale-App-Capabilities`). A destructive operation still needs
  `"confirm": true`, checked first. Every other applied write is refused
  with `write_not_authorized` as before. The flag adds to the tool's own
  `Options.Authorizer` (or `op.DenyWrites`): a call is allowed when the flag
  allows it or that authorizer does. An unknown or read operation in the
  list fails startup with the usage error `invalid_allow_apply`. A proxy
  running as the same user, such as one forwarding requests from a tagged
  device without identity headers, looks local, so do not publish a socket
  served with this flag through a proxy.
- **MCP caller metadata.** On MCP, stdio and HTTP alike, the tool call's
  request `_meta` reaches the handler as a copy in `op.Request.Meta`, nil
  when the call has none. It is never read from `arguments`, so a model that
  fills in arguments cannot set it, and it is always nil on the CLI and the
  HTTP API. Clients may add their own protocol keys under
  `io.modelcontextprotocol/`, so read only the keys you define. It is
  caller-supplied transport context, trusted only as much as the transport:
  on stdio that is the parent process that spawned the server.
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
destructive operations, an immediate CLI write, a paged list, provider error
details, root flags and render hooks,
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

It checks that `cli.Validate` accepts the command tree, including the
reserved flags, that the metadata is valid `toolkit.metadata.v1`, that MCP
`tools/list` and the OpenAPI document match the registry, that the CLI, HTTP
and MCP return the same output, that destructive operations refuse to apply
without `confirm` on every surface, and that writes change nothing without
`apply`. For an operation with `CLIImmediate` it previews the CLI with
`--dry-run`, checks that the CLI applies without `--apply` while HTTP and MCP
still preview without `apply`, and checks that the OpenAPI and MCP documents
carry the flag. For an operation with `CLIConfirmed` it checks that the CLI
applies without `--yes` while HTTP and MCP still refuse without `confirm`.
Case `Args` never include `--apply`, `--dry-run` or `--yes`. HTTP and
MCP calls leave out a case's cli-only inputs, and when a case sets one, the
kit checks that HTTP and MCP refuse it with `cli_only` and change nothing,
even applied, while the CLI accepts it.

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
| `go-version-file` | `go.mod` | Go minor version for `actions/setup-go`; runner mode selects the newest patch in that minor |
| `runner` | `vars.CI_RUNNER`, then `ubuntu-24.04` | Gate runner. The nightly uses `vars.CI_NIGHTLY_RUNNER`, then `vars.CI_RUNNER` |
| `apt-packages` | empty | Space-separated Ubuntu APT packages installed before runner-mode `./bin/ci`; ignored with `ci-image-dir` |
| `ci-image-dir` | empty | Directory with the Dockerfile of an execution image, for example `ci` |
| `golangci-lint-version` | `v2.13.2` | Installed when no image is used |
| `govulncheck-version` | `v1.7.0` | Nightly only, pinned for Go 1.25.x when no image is used; checked against the selected Go toolchain |

`ci/govulncheck-version` is the single image pin; the toolkit gate checks that
it matches the reusable workflow default and that the release is compatible
with the module's declared Go version.

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
