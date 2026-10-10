# Changelog

Released versions are described in their
[GitHub releases](https://github.com/0xble/toolkit/releases).

## Unreleased

- Add an optional `apt-packages` input to the reusable gate and nightly workflows
  to install Ubuntu APT dependencies before runner-mode CI. Image-mode CI is
  unchanged. Nightly runner selection now falls back to `CI_RUNNER` before
  `ubuntu-24.04`.

- Select the newest Go patch within each caller's declared minor version in
  runner-mode reusable gate and nightly workflows.
