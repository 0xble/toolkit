# Changelog

Released versions are described in their
[GitHub releases](https://github.com/0xble/toolkit/releases).

## Unreleased

### Added

- `op.Op.CLIImmediate`: the CLI applies a write or destructive operation
  without `--apply`, previews with a generated `--dry-run`, and keeps
  accepting `--apply` as a hidden no-op. Destructive operations still need
  `--yes` or a confirmed prompt. HTTP and MCP still need `"apply": true`, and
  `serve` still denies applied writes by default. Exposed as `cli_immediate`
  in `toolkit.metadata.v1`, `x-cli-immediate` in OpenAPI and
  `mcp.MetaCLIImmediate` (`toolkit/cli_immediate`) in MCP tool `_meta`.
- `op.Op.RenderWithInput`: a render hook that also receives the decoded
  input, and `op.Entry.RenderWithInput`. `Render` and `Entry.Render` are
  unchanged.
- `toolkittest` checks `CLIImmediate` operations: `--dry-run` previews, the
  CLI applies without `--apply`, HTTP and MCP still need `apply`, and
  destructive operations still need confirmation on every surface.

### Fixed

- The tool template's `bin/ci` runs `lane_mod` (`go mod verify` and
  `go mod tidy -diff`) in the `gate` and `nightly` profiles, not only in
  `preflight`. Tools could merge with a test-only dependency marked
  `// indirect`, and the goreleaser `go mod tidy` hook then rewrote `go.mod`
  after the release was tagged.
