# Changelog

Released versions are described in their
[GitHub releases](https://github.com/0xble/toolkit/releases).

## Unreleased

- Add the `sendguard` package for process-safe outbound duplicate-send claims.
  It stores only a SHA-256 digest in a flock-guarded, pruned JSONL ledger,
  preserves unverified claims, supports explicit duplicate overrides and
  caller-supplied remote-history checks, and maps duplicate claims to the
  `duplicate_send` operation kind and conflict exit/status.

- Add an optional `apt-packages` input to the reusable gate and nightly workflows
  to install Ubuntu APT dependencies before runner-mode CI. Image-mode CI is
  unchanged. Nightly runner selection now falls back to `CI_RUNNER` before
  `ubuntu-24.04`.

- Select the newest Go patch within each caller's declared minor version in
  runner-mode reusable gate and nightly workflows.
