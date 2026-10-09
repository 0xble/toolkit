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
  grep -Fq "check-latest: \${{ steps.go_version.outputs.version != '' }}" "$file" || {
    printf '%s does not request the latest patch only for stable versions\n' "$workflow" >&2
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

# Run the resolver step itself against fixtures, so the parsing is tested and
# not only its presence.
for workflow in .github/workflows/tool-gate.yml .github/workflows/tool-nightly.yml; do
  # The step body: lines after `run: |` in the go_version step, de-indented.
  script="$(awk '
    /id: go_version/ { in_step = 1; next }
    in_step && /^      - / { exit }
    in_step && /run: \|/ { in_run = 1; next }
    in_run { sub(/^          /, ""); print }
  ' "$root/$workflow")"
  [ -n "$script" ] || { printf '%s: go_version step body not found\n' "$workflow" >&2; exit 1; }
  tmp="$(mktemp -d)"
  check() {
    local name=$1 content=$2 want=$3
    printf '%s\n' "$content" >"$tmp/$name"
    : >"$tmp/out"
    if ! GO_VERSION_FILE="$tmp/$name" GITHUB_OUTPUT="$tmp/out" bash -c "$script" >/dev/null 2>&1; then
      [ "$want" = "FAIL" ] && return 0
      printf '%s: resolver failed on %q\n' "$workflow" "$content" >&2; exit 1
    fi
    [ "$want" != "FAIL" ] || { printf '%s: resolver accepted %q\n' "$workflow" "$content" >&2; exit 1; }
    got="$(sed -n 's/^version=//p' "$tmp/out")"
    [ "$got" = "$want" ] || { printf '%s: %q resolved to %q, want %q\n' "$workflow" "$content" "$got" "$want" >&2; exit 1; }
  }
  check go.mod $'module x\n\ngo 1.26.8' 1.26.x
  check go.mod $'module x\n\ngo 1.26' 1.26.x
  check go.mod $'module x\n\ngo 1.25.5\n\ntoolchain go1.26.8' 1.26.x
  check go.mod $'module x\n\ngo 1.26.8\n\ntoolchain default' 1.26.x
  check go.mod $'module x\n\ngo 1.26.0\n\ntoolchain go1.27rc1' ''
  check go.work $'go 1.26.4\n\nuse .' 1.26.x
  check .tool-versions 'golang 1.25.14' 1.25.x
  check .go-version '1.26.9' 1.26.x
  check go.mod 'module x' FAIL
  rm -rf "$tmp"
done

printf 'runner-mode Go patch resolution contract passed\n'
