# Changelog

Released versions are described in their
[GitHub releases](https://github.com/0xble/toolkit/releases).

## Unreleased

- Pin the reusable nightly workflow and execution image to `govulncheck`
  `v1.7.0`, whose module requires Go 1.25.0 and therefore supports callers
  using Go 1.25.x. Runner mode now checks the selected `govulncheck` module's
  Go requirement against the configured toolchain before installation.
- The CLI accepts a negative number as a flag value or positional argument:
  `--min -5`, `temp -40 --side right`, `--ratio -0.5`, `-1e3`. Tools no longer
  need `--` or argument rewriting for it. A digit declared as a short flag
  stays a flag, and `-j`, `-y` and other flags are unchanged. A stray negative
  number now fails with `unexpected argument -40` rather than
  `unknown flag -4`.
- An explicit CLI value equal to the zero value now reaches the operation
  when its input field has a `default` tag and `omitempty`: `--level 0`,
  `--enabled=false`, `--label ""`, a root flag such as `--limit 0`, and a
  flag's env var set to `0`. Before, the default replaced it. HTTP and MCP
  already kept an explicit `0`, `false` or `""`.
- An `int64` or `uint64` input above 2^53, such as a Telegram message, chat,
  document or custom emoji ID, now reaches the operation exactly on the CLI,
  HTTP and MCP. `op.(*Entry).Decode` used to read every number as `float64`,
  so `5312241539987020022` arrived as `5312241539987019776` and
  `18446744073709551615` failed to decode into a `uint64`. `--fields` output
  likewise keeps such integers exact, paged or not. Numbers written with a
  fraction or exponent still decode as before, so `1.0` and `1e3` still fill
  an integer field.
- Input `default` tags now apply in nested struct fields, not only in
  embedded ones, on the CLI, HTTP and MCP. A pointer-to-struct section gets
  its defaults only when the caller sends it. An omitted section stays nil,
  so a handler can still tell absent from present. A tool whose input nests
  a struct with `default` tags will now see those defaults where it saw zero
  values.
- A `json:",string"` tag now follows encoding/json everywhere. The input
  schema types such a field as a string, and a `default` on it is written
  as a JSON string (`"7"`, not `7`). The tag is ignored on slices, maps,
  structs and pointers to pointers, as encoding/json ignores it. An explicit
  zero CLI value (`--count 0`, `--enabled=false`, an empty `<id>`) is sent in
  its quoted form. Each operation under a shared `<id>` path placeholder uses
  its own field's tag.

