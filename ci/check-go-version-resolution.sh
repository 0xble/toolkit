#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"

for workflow in .github/workflows/tool-gate.yml .github/workflows/tool-nightly.yml; do
  file="$root/$workflow"
  [ -f "$file" ] || { printf 'missing workflow: %s\n' "$workflow" >&2; exit 1; }

  grep -Fq 'id: go_version' "$file" || {
    printf '%s does not resolve a Go minor version\n' "$workflow" >&2
    exit 1
  }
  grep -Fq "GO_VERSION_FILE: \${{ inputs.go-version-file }}" "$file" || {
    printf '%s does not read the configured Go version file\n' "$workflow" >&2
    exit 1
  }
  grep -Fq "\$1 == \"toolchain\"" "$file" || {
    printf '%s does not prefer a Go toolchain directive when present\n' "$workflow" >&2
    exit 1
  }
  grep -Fq "go-version: \${{ steps.go_version.outputs.version }}" "$file" || {
    printf '%s does not pass the resolved version to setup-go\n' "$workflow" >&2
    exit 1
  }
  grep -Fq 'check-latest: true' "$file" || {
    printf '%s does not request the latest patch release\n' "$workflow" >&2
    exit 1
  }
  if grep -Fq "go-version-file: \${{ inputs.go-version-file }}" "$file"; then
    printf '%s still passes the exact file version to setup-go\n' "$workflow" >&2
    exit 1
  fi
done

nightly="$root/.github/workflows/tool-nightly.yml"
grep -Fq 'name: Check govulncheck Go compatibility' "$nightly" || {
  echo 'tool-nightly lost the govulncheck compatibility check' >&2
  exit 1
}

printf 'runner-mode Go patch resolution contract passed\n'
