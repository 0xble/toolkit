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

# Exercise the actual API-backed entry point, not just the pure decision helper.
fixtures="$(dirname "$SCRIPT")/fixtures"
test_dir=$(mktemp -d "${TMPDIR:-/tmp}/nightly-guard.XXXXXX")
trap 'rm -f "$output_file"; rm -rf "$test_dir"' EXIT
mkdir "$test_dir/bin"
real_date=$(command -v date)
cat >"$test_dir/bin/date" <<'MOCK'
#!/usr/bin/env bash
if [ "$*" = '-u +%s' ]; then
  printf '%s\n' "$TEST_NOW"
else
  exec "$REAL_DATE" "$@"
fi
MOCK
cat >"$test_dir/bin/gh" <<'MOCK'
#!/usr/bin/env bash
for arg in "$@"; do
  case "$arg" in
    repos/test/repo) printf 'main\n'; exit ;;
    repos/test/repo/commits/main) printf '%s\n' "$TEST_SHA"; exit ;;
    repos/test/repo/actions/workflows/nightly.yml/runs) cat "$TEST_RUNS"; exit ;;
    repos/test/repo/actions/runs/999/attempts/1/jobs) cat "$TEST_NOOP_JOBS"; exit ;;
    repos/test/repo/actions/runs/*/attempts/*/jobs) cat "$TEST_JOBS"; exit ;;
  esac
done
printf 'unexpected API call: %s\n' "$*" >&2
exit 1
MOCK
chmod +x "$test_dir/bin/gh" "$test_dir/bin/date"
export REAL_DATE="$real_date" TEST_SHA TEST_NOW TEST_RUNS TEST_JOBS
TEST_SHA=$(jq -r '.workflow_runs[0].head_sha' "$fixtures/runs.json")
TEST_RUNS="$fixtures/runs.json"
TEST_JOBS="$fixtures/jobs.json"
started=$(jq -r '.workflow_runs[0].run_started_at' "$fixtures/runs.json")
start_epoch=$("$real_date" -u -d "$started" +%s 2>/dev/null ||
  "$real_date" -u -j -f '%Y-%m-%dT%H:%M:%SZ' "$started" +%s)
TEST_NOW=$((start_epoch + 86400))

assert_api() {
  local label=$1 expected=$2 actual
  : >"$output_file"
  PATH="$test_dir/bin:$PATH" GITHUB_EVENT_NAME=schedule \
    GITHUB_REPOSITORY=test/repo GITHUB_WORKFLOW_REF=test/repo/.github/workflows/nightly.yml@refs/heads/main \
    GITHUB_SHA="$TEST_SHA" GITHUB_OUTPUT="$output_file" "$SCRIPT" >"$test_dir/log" 2>&1
  actual=$(cat "$output_file")
  if [ "$actual" != "run-nightly=$expected" ]; then
    printf '%s: expected run-nightly=%s, got %s\n' "$label" "$expected" "$actual" >&2
    cat "$test_dir/log" >&2
    exit 1
  fi
}

assert_api 'real response without completed_at skips unchanged nightly' false

export TEST_NOOP_JOBS="$test_dir/noop-jobs.json"
jq '.jobs |= (map(if .name == "source" then
  .conclusion = "skipped" | .steps = [] | .started_at = null | .completed_at = null
  else . end) + [{name: "nightly guard", status: "completed", conclusion: "success", steps: []}]) |
  .total_count = (.jobs | length)' "$fixtures/jobs.json" >"$TEST_NOOP_JOBS"
iso_epoch() {
  "$real_date" -u -d "@$1" '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null ||
    "$real_date" -u -r "$1" '+%Y-%m-%dT%H:%M:%SZ'
}
make_noop_history() {
  local noop_time
  noop_time=$(iso_epoch "$1")
  jq --arg time "$noop_time" '.workflow_runs[0] as $executed |
    .workflow_runs = [($executed | .id = 999 | .run_started_at = $time | .updated_at = $time), $executed]' \
    "$fixtures/runs.json" >"$test_dir/noop-runs.json"
  TEST_RUNS="$test_dir/noop-runs.json"
}
make_noop_history "$((start_epoch + 86400))"
TEST_NOW=$((start_epoch + 2 * 86400))
assert_api 'no-op is ignored while recent execution still permits skipping' false
make_noop_history "$((start_epoch + 7 * 86400))"
TEST_NOW=$((start_epoch + 8 * 86400))
assert_api 'recent no-op must not reset weekly execution floor' true
grep -q 'ignoring intentional no-op scheduled run 999' "$test_dir/log"
jq '.workflow_runs = [.workflow_runs[0]]' "$TEST_RUNS" >"$test_dir/only-noops.json"
TEST_RUNS="$test_dir/only-noops.json"
assert_api 'no executed history forces full run' true

TEST_RUNS="$fixtures/runs.json"
TEST_NOW=$((start_epoch + 7 * 86400))
assert_api 'weekly boundary forces full run' true
TEST_NOW=$((start_epoch + 8 * 86400))
assert_api 'more than seven days since execution forces full run' true
TEST_NOW=$((start_epoch + 86400))
TEST_SHA=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
assert_api 'changed nightly inputs force full run' true
TEST_SHA=$(jq -r '.workflow_runs[0].head_sha' "$fixtures/runs.json")

# Match the nested reusable Go workflow, including its image-mode step.
jq '.jobs |= map(.name = (if .name == "source" then "nightly / nightly" else "nightly / " + .name end) |
  .steps |= map(if .name == "Run ./bin/ci nightly on the exact SHA" then
    .name += " in the image" else . end))' "$fixtures/jobs.json" >"$test_dir/image-jobs.json"
TEST_JOBS="$test_dir/image-jobs.json"
assert_api 'reusable nightly image execution permits skipping' false
jq '.jobs |= map(.name = (if .name == "source" then "nightly / nightly" else "nightly / " + .name end))' \
  "$TEST_NOOP_JOBS" >"$test_dir/nested-noop-jobs.json"
TEST_NOOP_JOBS="$test_dir/nested-noop-jobs.json"
TEST_JOBS="$fixtures/jobs.json"
make_noop_history "$((start_epoch + 86400))"
TEST_NOW=$((start_epoch + 2 * 86400))
assert_api 'reusable skipped nightly does not hide recent execution' false
TEST_RUNS="$fixtures/runs.json"
TEST_NOW=$((start_epoch + 86400))

# Unknown execution evidence, truncated results, and provider failures fail open.
TEST_JOBS="$test_dir/bad-jobs.json"
for filter in \
  '.jobs |= map(.steps = [])' \
  '.total_count += 1' \
  '.jobs = [] | .total_count = 0' \
  '.jobs[0].steps = null' \
  '.jobs[0].steps[0].status = null' \
  '.jobs[0].steps[0].conclusion = "failure"' \
  '.jobs[0].name = "unrecognized lane"'; do
  jq "$filter" "$fixtures/jobs.json" >"$TEST_JOBS"
  assert_api "unknown jobs: $filter" true
done
TEST_JOBS="$test_dir/missing-jobs.json"
assert_api 'jobs API failure runs nightly' true
TEST_JOBS="$fixtures/jobs.json"
TEST_RUNS="$test_dir/bad-runs.json"
for filter in \
  '.workflow_runs = []' \
  '.workflow_runs[0].run_started_at = null' \
  '.workflow_runs[0].head_branch = null' \
  '.workflow_runs[0].run_attempt = null' \
  '.workflow_runs[0].run_started_at = "invalid timestamp"'; do
  jq "$filter" "$fixtures/runs.json" >"$TEST_RUNS"
  assert_api "unknown runs: $filter" true
done
printf 'not JSON\n' >"$TEST_RUNS"
assert_api 'invalid JSON runs nightly' true
TEST_RUNS="$fixtures/runs.json"
TEST_NOW=$((start_epoch - 1))
assert_api 'future run timestamp fails open' true
printf 'nightly guard tests passed\n'
