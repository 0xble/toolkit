# Changelog

Released versions are described in their
[GitHub releases](https://github.com/0xble/toolkit/releases).

## Unreleased

### Added

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
