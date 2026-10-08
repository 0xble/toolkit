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

