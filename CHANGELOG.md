# Changelog

Released versions are described in their
[GitHub releases](https://github.com/0xble/toolkit/releases).

## Unreleased

### Added

- Input fields tagged `toolkit:"cli-only"` are accepted only on the command
  line, for flags and arguments that name local files. `op.Add` records them
  in `op.Entry.CLIOnlyInputs` and rejects one that is required or has a
  default. HTTP and MCP calls that set one are refused in `op` with the usage
  error `cli_only` before confirmation, authorization or the handler. The
  OpenAPI and MCP input schemas and the metadata `input` omit them, and the
  metadata lists them in the optional `cli_only_inputs` array
  (`metadata.schema.json` gains the field). `toolkittest` leaves a case's
  cli-only inputs out of HTTP and MCP calls and checks that both surfaces
  refuse them. The sample gains `notes export --out FILE`.
- `op.Op.Paged` (and `op.Entry.Paged`) declares an output that is a page: an
  object with an `items` array next to envelope keys such as `next_cursor`
  and `has_more`. The CLI's `--fields` then keeps the named keys of each item
  and every envelope key, so `meetings --fields id,title` no longer prints
  `{}`. Without `Paged`, `--fields` still keeps top-level keys, so no existing
  output changes. `op.Add` rejects `Paged` on an output without an `items`
  array.
- `op.Error` carries optional provider details: `Retryable` (`*bool`),
  `HTTPStatus`, `RetryAfterSeconds` and `RequestID`, printed only when set
  as `retryable`, `http_status`, `retry_after_seconds` and `request_id` in
  the CLI JSON envelope, the HTTP error body and the MCP error text.
  `Retryable` overrides the CLI's kind-derived value. HTTP also sends a
  `Retry-After` header, and the OpenAPI error schema lists the four keys.
  Envelopes of errors without details are byte-identical to v0.1.4.

### Changed

- `cli.Validate`, and with it the `toolkittest` command-tree check, rejects a
  command flag that kong would silently parse as a root flag: one reusing the
  name, an alias or the short form of `--json`/`-j`, `--agent`, `--fields`,
  `--yes`/`-y`, `--version`, `--help`/`-h` or one of the tool's `Globals`,
  and an operation input flag named `--apply` or `--dry-run`. The error names
  the operation or command, the flag and what it collides with. `cli.Run` is
  unchanged, so an existing binary keeps running until its tests catch it.

### Fixed

- A command group whose only child is its default command is listed in root
  `--help` (as `account show`). The default was hidden, and kong's help lists
  only leaf commands, so the whole group disappeared. A default command with
  siblings stays hidden.
