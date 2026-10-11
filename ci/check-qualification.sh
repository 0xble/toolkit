#!/usr/bin/env bash
# Execute the real aggregation shells: only an exact-SHA lane success qualifies.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
reusable="$root/.github/workflows/tool-gate.yml"
caller="$root/.github/workflows/gate.yml"

# Pin the data flow that supplies the shells, not a synthetic success receipt.
grep -Fq 'value: ${{ jobs.gate.outputs.lane-result }}' "$reusable"
grep -Fq 'lane-result: ${{ job.status }}' "$reusable"
grep -A6 '^      caller-qualification:' "$reusable" | grep -Fq 'default: false'
grep -Fq 'if: ${{ always() && !inputs.caller-qualification }}' "$reusable"
grep -Fq 'caller-qualification: true' "$caller"
grep -Fq 'LANE_RESULT: ${{ needs.gate.outputs.lane-result }}' "$caller"
grep -A6 '^  qualification:' "$caller" | grep -Fq 'if: always()'
grep -A6 '^  qualification:' "$caller" | grep -Fq 'runs-on: ubuntu-slim'

aggregate_shell() {
  awk '
    /^  qualification:/ { in_job = 1; next }
    in_job && /run: \|/ { in_run = 1; next }
    in_run { sub(/^          /, ""); print }
  ' "$1"
}
legacy="$(aggregate_shell "$reusable")"
folded="$(aggregate_shell "$caller")"
[ -n "$legacy" ] && [ -n "$folded" ]

# Reusable-workflow success alone must not turn an all-skipped workflow green.
# Test all result pairs, including an empty/missing workflow output.
cases=0
for workflow_result in success failure cancelled skipped ''; do
  for lane_result in success failure cancelled skipped ''; do
    expected=1
    if [ "$workflow_result" = success ] && [ "$lane_result" = success ]; then
      expected=0
    fi
    status=0
    GATE_RESULT="$workflow_result" LANE_RESULT="$lane_result" bash -c "$folded" >/dev/null 2>&1 || status=$?
    if { [ "$expected" -eq 0 ] && [ "$status" -ne 0 ]; } ||
       { [ "$expected" -ne 0 ] && [ "$status" -eq 0 ]; }; then
      printf 'folded qualification accepted wrong result: workflow=%q lane=%q status=%s\n' \
        "$workflow_result" "$lane_result" "$status" >&2
      exit 1
    fi
    cases=$((cases + 1))
  done
done

# Preserve the legacy fail-closed behavior for non-opted callers, and verify
# equivalent lane acceptance for ready PRs, drafts, queue drafts and main pushes.
for candidate in ready draft queue push; do
  draft=false author=0xble ref=feat/example
  case "$candidate" in
    draft) draft=true ;;
    queue) draft=true author='mergify[bot]' ref=mergify/merge-queue/example ;;
    push) draft='' author='' ref='' ;;
  esac
  for result in success failure cancelled skipped ''; do
    legacy_status=0 folded_status=0
    GATE_RESULT="$result" DRAFT_PR="$draft" PR_AUTHOR="$author" HEAD_REF="$ref" \
      bash -c "$legacy" >/dev/null 2>&1 || legacy_status=$?
    GATE_RESULT=success LANE_RESULT="$result" \
      bash -c "$folded" >/dev/null 2>&1 || folded_status=$?
    [ "$legacy_status" -eq "$folded_status" ] || {
      printf 'aggregation diverged for %s/%q: legacy=%s folded=%s\n' \
        "$candidate" "$result" "$legacy_status" "$folded_status" >&2
      exit 1
    }
    cases=$((cases + 1))
  done
done
printf 'qualification aggregation contract passed (%s result cases)\n' "$cases"
