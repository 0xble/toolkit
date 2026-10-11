# Changelog

Released versions are described in their
[GitHub releases](https://github.com/0xble/toolkit/releases).

## v0.1.14

- Add the `sendguard` package for process-safe outbound duplicate-send claims.
  It stores only a SHA-256 digest in a flock-guarded, pruned JSONL ledger,
  preserves unverified claims, supports explicit duplicate overrides and
  caller-supplied remote-history checks, and maps duplicate claims to the
  `duplicate_send` operation kind and conflict exit/status.

## v0.1.13

- Add an opt-in `caller-qualification` input and a `lane-result` output to the
  reusable gate. A caller that sets it runs the lane check in its own required
  `qualification` job, which must require both the workflow result and
  `lane-result` to be `success`, and the reusable `gate / qualification` job is
  skipped. The default keeps existing callers unchanged.
