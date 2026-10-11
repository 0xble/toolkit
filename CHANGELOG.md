# Changelog

Released versions are described in their
[GitHub releases](https://github.com/0xble/toolkit/releases).

## Unreleased

- Add an opt-in `caller-qualification` input and a `lane-result` output to the
  reusable gate. A caller that sets it runs the lane check in its own required
  `qualification` job, which must require both the workflow result and
  `lane-result` to be `success`, and the reusable `gate / qualification` job is
  skipped. The default keeps existing callers unchanged.
