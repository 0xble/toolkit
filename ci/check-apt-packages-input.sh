#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"

for workflow in .github/workflows/tool-gate.yml .github/workflows/tool-nightly.yml; do
  file="$root/$workflow"
  [ -f "$file" ] || { printf 'missing workflow: %s\n' "$workflow" >&2; exit 1; }

  grep -Fq '      apt-packages:' "$file" || {
    printf '%s does not declare the apt-packages input\n' "$workflow" >&2
    exit 1
  }
  grep -Fq 'APT_PACKAGES: ${{ inputs.apt-packages }}' "$file" || {
    printf '%s does not pass apt-packages through the environment\n' "$workflow" >&2
    exit 1
  }
  condition="inputs.ci-image-dir == '' && inputs.apt-packages != ''"
  grep -Fq "$condition" "$file" || {
    printf '%s does not restrict installation to non-image jobs with packages\n' "$workflow" >&2
    exit 1
  }
  grep -Fq 'sudo apt-get update' "$file" || {
    printf '%s does not refresh the APT index\n' "$workflow" >&2
    exit 1
  }
  grep -Fq 'sudo apt-get install -y --no-install-recommends -- "${packages[@]}"' "$file" || {
    printf '%s does not install the requested package array\n' "$workflow" >&2
    exit 1
  }

  apt_line="$(awk '/sudo apt-get install -y --no-install-recommends --/ { print NR; exit }' "$file")"
  ci_line="$(awk '/Run \.\/bin\/ci (gate|nightly) on the exact SHA$/ { print NR; exit }' "$file")"
  [ -n "$apt_line" ] && [ -n "$ci_line" ] && [ "$apt_line" -lt "$ci_line" ] || {
    printf '%s installs packages after its CI command\n' "$workflow" >&2
    exit 1
  }
done

printf 'apt-packages workflow contract passed\n'
