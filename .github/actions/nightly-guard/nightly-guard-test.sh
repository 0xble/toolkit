#!/usr/bin/env bash
set -euo pipefail

SCRIPT="$(cd "$(dirname "$0")" && pwd)/nightly-guard.sh"
sha_a=$(printf 'a%.0s' {1..40})
sha_b=$(printf 'b%.0s' {1..40})
now=1000000

assert_decision() {
  local expected=$1
  shift
  local actual
  actual=$("$SCRIPT" decision "$@")
  if [ "$actual" != "$expected" ]; then
    printf 'expected %s, got %s\n' "$expected" "$actual" >&2
    exit 1
  fi
}

# Same default-branch SHA and a recent successful scheduled run are skipped.
assert_decision skip "$sha_a" "$sha_a" "$sha_a" $((now - 1)) "$now"
# A new default-branch commit runs even when the previous run was recent.
assert_decision run "$sha_b" "$sha_b" "$sha_a" $((now - 1)) "$now"
# A successful run for another SHA cannot suppress the current nightly.
assert_decision run "$sha_a" "$sha_a" "$sha_b" $((now - 1)) "$now"
# The weekly floor is inclusive: at seven days, run again.
assert_decision run "$sha_a" "$sha_a" "$sha_a" $((now - 7 * 24 * 60 * 60)) "$now"
# Missing or malformed evidence fails open.
assert_decision run "$sha_a" "$sha_a" '' $((now - 1)) "$now"
assert_decision run bad "$sha_a" "$sha_a" "$sha_a" $((now - 1)) "$now"

output_file="${TMPDIR:-/tmp}/nightly-guard-test.$$.out"
trap 'rm -f "$output_file"' EXIT
: >"$output_file"
GITHUB_EVENT_NAME=workflow_dispatch GITHUB_OUTPUT="$output_file" "$SCRIPT" >/dev/null
[ "$(cat "$output_file")" = 'run-nightly=true' ]
: >"$output_file"
GITHUB_EVENT_NAME=schedule GITHUB_OUTPUT="$output_file" "$SCRIPT" >/dev/null 2>&1
[ "$(cat "$output_file")" = 'run-nightly=true' ]

printf 'nightly guard tests passed\n'
