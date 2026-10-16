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
# Arguments: current_sha default_sha last_success_sha completion_epoch now_epoch
nightly_guard_decision() {
  local current_sha=${1:-}
  local default_sha=${2:-}
  local last_success_sha=${3:-}
  local completion_epoch=${4:-}
  local now_epoch=${5:-}
  if [[ "$current_sha" =~ ^[0-9a-f]{40}$ &&
        "$default_sha" =~ ^[0-9a-f]{40}$ &&
        "$last_success_sha" =~ ^[0-9a-f]{40}$ &&
        "$completion_epoch" =~ ^[0-9]+$ &&
        "$now_epoch" =~ ^[0-9]+$ &&
        "$current_sha" == "$default_sha" &&
        "$last_success_sha" == "$current_sha" ]]; then
    local age=$((now_epoch - completion_epoch))
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

if ! last_run=$(printf '%s' "$runs_json" | jq -e --arg branch "$default_branch" '
  [.workflow_runs[]? |
    select(.status == "completed" and .conclusion == "success" and .event == "schedule" and
           (.head_branch == $branch or .head_branch == null))]
  | if length == 0 then {} else max_by(.completed_at // "") end
' 2>/dev/null); then
  fail_open 'scheduled run response was not valid JSON'
fi
last_success_sha=$(printf '%s' "$last_run" | jq -r '.head_sha // empty' 2>/dev/null) || fail_open 'scheduled run SHA missing'
last_completed=$(printf '%s' "$last_run" | jq -r '.completed_at // empty' 2>/dev/null) || fail_open 'scheduled completion missing'

# GNU date is present on GitHub's Ubuntu runners; the BSD fallback keeps the
# pure script testable on macOS developer machines.
to_epoch() {
  local timestamp=$1
  if date -u -d "$timestamp" +%s 2>/dev/null; then
    return 0
  fi
  date -u -j -f '%Y-%m-%dT%H:%M:%SZ' "$timestamp" +%s 2>/dev/null
}
[ -n "$last_success_sha" ] || fail_open 'no successful scheduled run exists'
[ -n "$last_completed" ] || fail_open 'successful run has no completion time'
completion_epoch=$(to_epoch "$last_completed") || fail_open 'completion time was not parseable'
now_epoch=$(date -u +%s) || fail_open 'current time was not readable'

result=$(nightly_guard_decision "$current_sha" "$default_sha" "$last_success_sha" \
  "$completion_epoch" "$now_epoch")
if [ "$result" = skip ]; then
  printf 'scheduled nightly skipped: successful run %s already covers default-branch SHA %s and is less than seven days old\n' \
    "$last_success_sha" "$current_sha"
  set_output false
else
  printf 'scheduled nightly running: no recent successful run covers default-branch SHA %s\n' "$current_sha"
  set_output true
fi
