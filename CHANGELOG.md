# Changelog

Released versions are described in their
[GitHub releases](https://github.com/0xble/toolkit/releases).

## Unreleased

- Pin the reusable nightly workflow and execution image to `govulncheck`
  `v1.7.0`, whose module requires Go 1.25.0 and therefore supports callers
  using Go 1.25.x. Runner mode now checks the selected `govulncheck` module's
  Go requirement against the configured toolchain before installation.

