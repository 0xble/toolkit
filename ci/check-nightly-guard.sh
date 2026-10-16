#!/usr/bin/env bash
set -euo pipefail

bash .github/actions/nightly-guard/nightly-guard-test.sh

# The reusable workflow runs in the caller's workspace: a local `./` action
# reference would not exist there. The nightly must also fail open when the
# guard job fails, so it may skip only on an explicit `false`.
wf=.github/workflows/tool-nightly.yml
if grep -nE 'uses: \./\.github/actions/' "$wf"; then
  echo "check-nightly-guard: $wf must not use a local ./ action path" >&2
  exit 1
fi
grep -q "uses: 0xble/toolkit/.github/actions/nightly-guard@v" "$wf" || {
  echo "check-nightly-guard: $wf must pin the nightly-guard action by repository and tag" >&2
  exit 1
}
grep -q "needs.nightly_guard.outputs.run-nightly != 'false'" "$wf" || {
  echo "check-nightly-guard: $wf nightly must fail open (skip only on an explicit false)" >&2
  exit 1
}
