#!/usr/bin/env bash
# Decide whether a scheduled nightly is redundant.
# The API path is deliberately fail-open: an ambiguous or unavailable lookup
# must run the nightly rather than silently remove drift coverage.
set -u

WEEKLY_FLOOR_SECONDS=$((7 * 24 * 60 * 60))

set_output() {
  local value=$1
  if [ -n "${GITHUB_OUTPUT:-}" ]; then
    printf 'run-nightly=%s\n' "$value" >>"$GITHUB_OUTPUT"
  fi
}

# Pure decision function used by the unit tests and the API-backed path.
# Arguments: current_sha default_sha last_success_sha execution_epoch now_epoch
nightly_guard_decision() {
  local current_sha=${1:-}
  local default_sha=${2:-}
  local last_success_sha=${3:-}
  local execution_epoch=${4:-}
  local now_epoch=${5:-}
  if [[ "$current_sha" =~ ^[0-9a-f]{40}$ &&
        "$default_sha" =~ ^[0-9a-f]{40}$ &&
        "$last_success_sha" =~ ^[0-9a-f]{40}$ &&
        "$execution_epoch" =~ ^[0-9]+$ &&
        "$now_epoch" =~ ^[0-9]+$ &&
        "$current_sha" == "$default_sha" &&
        "$last_success_sha" == "$current_sha" ]]; then
    local age=$((now_epoch - execution_epoch))
    if (( age >= 0 && age < WEEKLY_FLOOR_SECONDS )); then
      printf 'skip\n'
      return 0
    fi
  fi
  printf 'run\n'
}

# Test entry point: keep the decision deterministic and independent of GitHub.
if [ "${1:-}" = decision ]; then
  shift
  nightly_guard_decision "$@"
  exit 0
fi

# Manual dispatch is an explicit override and never consults the API.
if [ "${GITHUB_EVENT_NAME:-}" != schedule ]; then
  printf 'manual override: running nightly (%s)\n' "${GITHUB_EVENT_NAME:-unknown}"
  set_output true
  exit 0
fi

fail_open() {
  printf 'nightly guard unavailable (%s); running nightly\n' "$1" >&2
  set_output true
  exit 0
}

repo=${GITHUB_REPOSITORY:-}
workflow_ref=${GITHUB_WORKFLOW_REF:-}
current_sha=${GITHUB_SHA:-}
[ -n "$repo" ] || fail_open 'GITHUB_REPOSITORY is missing'
[ -n "$workflow_ref" ] || fail_open 'GITHUB_WORKFLOW_REF is missing'
[ -n "$current_sha" ] || fail_open 'GITHUB_SHA is missing'

# GITHUB_WORKFLOW_REF points at the caller's workflow even when this action is
# reached through the reusable toolkit workflow.
case "$workflow_ref" in
  */.github/workflows/*@*)
    workflow_file=${workflow_ref#*/.github/workflows/}
    workflow_file=${workflow_file%@*}
    ;;
  *) fail_open "cannot derive workflow path from $workflow_ref" ;;
esac
[ -n "$workflow_file" ] || fail_open 'workflow path is empty'

if ! command -v gh >/dev/null 2>&1; then
  fail_open 'gh is not installed'
fi
if ! command -v jq >/dev/null 2>&1; then
  fail_open 'jq is not installed'
fi

# Every command in this block is read-only. Keep each response separate so a
# malformed response cannot be mistaken for a successful empty result.
if ! default_branch=$(gh api --method GET "repos/$repo" --jq '.default_branch' 2>/dev/null); then
  fail_open 'repository lookup failed'
fi
[ -n "$default_branch" ] || fail_open 'repository returned no default branch'
if ! default_sha=$(gh api --method GET "repos/$repo/commits/$default_branch" --jq '.sha' 2>/dev/null); then
  fail_open 'default branch SHA lookup failed'
fi
if ! runs_json=$(gh api --method GET "repos/$repo/actions/workflows/$workflow_file/runs" \
    -f "branch=$default_branch" -f 'event=schedule' -f 'status=success' -f 'per_page=100' 2>/dev/null); then
  fail_open 'scheduled run lookup failed'
fi

# Runs have run_started_at and updated_at, not completed_at. Start time is
# conservative: metadata updates and reruns must not postpone the weekly floor.
if ! runs=$(printf '%s' "$runs_json" | jq -er --arg branch "$default_branch" '
  if (.workflow_runs | type) != "array" then error("missing runs") else . end |
  .workflow_runs |
  if all(.[];
    .status == "completed" and .conclusion == "success" and .event == "schedule" and
    .head_branch == $branch and
    (.id | type == "number" and . > 0 and . == floor) and
    (.run_attempt | type == "number" and . > 0 and . == floor) and
    (.head_sha | type == "string" and test("^[0-9a-f]{40}$")) and
    (.run_started_at | type == "string" and test("^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$")))
  then sort_by(.run_started_at) | reverse[] |
    [.id, .run_attempt, .head_sha, .run_started_at] | @tsv
  else error("unknown run shape") end
' 2>/dev/null); then
  fail_open 'no known successful scheduled run response'
fi

# GNU date is present on GitHub's Ubuntu runners; the BSD fallback keeps the
# pure script testable on macOS developer machines.
to_epoch() {
  local timestamp=$1
  if date -u -d "$timestamp" +%s 2>/dev/null; then
    return 0
  fi
  date -u -j -f '%Y-%m-%dT%H:%M:%SZ' "$timestamp" +%s 2>/dev/null
}
now_epoch=$(date -u +%s) || fail_open 'current time was not readable'
[[ "$now_epoch" =~ ^[0-9]+$ ]] || fail_open 'current time was not numeric'
last_success_sha=
execution_epoch=
while IFS=$'\t' read -r run_id run_attempt run_sha run_started; do
  run_epoch=$(to_epoch "$run_started") || fail_open 'run start time was not parseable'
  [[ "$run_epoch" =~ ^[0-9]+$ ]] || fail_open 'run start time was not numeric'
  (( now_epoch >= run_epoch )) || fail_open 'run start time is in the future'
  # All remaining runs are older. None can satisfy the weekly floor.
  (( now_epoch - run_epoch < WEEKLY_FLOOR_SECONDS )) || break
  # Inspect the exact successful attempt, not a later attempt of the same run.
  if ! jobs_json=$(gh api --method GET "repos/$repo/actions/runs/$run_id/attempts/$run_attempt/jobs" \
      -f 'per_page=100' 2>/dev/null); then
    fail_open 'scheduled run jobs lookup failed'
  fi
  if ! execution=$(printf '%s' "$jobs_json" | jq -er '
    def lane: .name | test("(^| / )(source|nightly)$");
    def full_step: .name == "Run ./bin/ci nightly on the exact SHA" or
      .name == "Run ./bin/ci nightly on the exact SHA in the image";
    if (.jobs | type) != "array" or .total_count != (.jobs | length) or
      (.jobs | length) == 0 then error("incomplete jobs") else . end |
    if all(.jobs[];
      (.name | type == "string") and .status == "completed" and
      (.conclusion == "success" or .conclusion == "skipped") and
      (.steps | type == "array") and
      all(.steps[]; (.name | type == "string") and .status == "completed" and
        (.conclusion == "success" or .conclusion == "skipped")))
    then . else error("unknown job shape") end |
    [.jobs[] | select(lane)] as $lanes |
    if ($lanes | length) == 0 then error("no known nightly lane")
    elif all($lanes[]; .conclusion == "success" and
      any(.steps[]; full_step and .status == "completed" and .conclusion == "success"))
    then "executed"
    elif all($lanes[]; .conclusion == "skipped") and
      any(.jobs[]; (.name | test("(^| / )nightly guard$")) and .conclusion == "success")
    then "noop"
    else error("unknown execution evidence") end
  ' 2>/dev/null); then
    fail_open 'scheduled run execution evidence is unknown or incomplete'
  fi
  if [ "$execution" = executed ]; then
    last_success_sha=$run_sha
    execution_epoch=$run_epoch
    break
  fi
  printf 'ignoring intentional no-op scheduled run %s for weekly floor\n' "$run_id"
done <<<"$runs"

result=$(nightly_guard_decision "$current_sha" "$default_sha" "$last_success_sha" \
  "$execution_epoch" "$now_epoch")
if [ "$result" = skip ]; then
  printf 'scheduled nightly skipped: executed run %s covers default-branch SHA %s and is less than seven days old\n' \
    "$run_id" "$current_sha"
  set_output false
else
  printf 'scheduled nightly running: no recent executed successful run covers default-branch SHA %s\n' "$current_sha"
  set_output true
fi
