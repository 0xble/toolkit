#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
expected="$(tr -d '[:space:]' <"$root/ci/govulncheck-version")"
workflow_version="$(awk '
  /^      govulncheck-version:/ { found=1; next }
  found && /^[[:space:]]*default:/ { print $2; exit }
' "$root/.github/workflows/tool-nightly.yml")"
dockerfile="$root/ci/Dockerfile"
docker_version=""
if grep -Fq 'COPY govulncheck-version /tmp/govulncheck-version' "$dockerfile" &&
  grep -Fq "go install \"golang.org/x/vuln/cmd/govulncheck@\$vuln_version\"" "$dockerfile"; then
  docker_version="$expected"
fi

printf '%s' "$expected" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$' || {
  printf 'invalid govulncheck pin: %s\n' "$expected" >&2
  exit 1
}
[ "$workflow_version" = "$expected" ] || {
  printf 'workflow govulncheck pin %s differs from %s\n' "$workflow_version" "$expected" >&2
  exit 1
}
[ "$docker_version" = "$expected" ] || {
  printf 'Dockerfile govulncheck pin %s differs from %s\n' "$docker_version" "$expected" >&2
  exit 1
}

required_go="$(GOWORK=off GOTOOLCHAIN=local go list -m -f '{{.GoVersion}}' "golang.org/x/vuln@$expected")"
project_go="$(GOWORK=off GOTOOLCHAIN=local go list -m -f '{{.GoVersion}}')"
version_at_least() {
  awk -v have="$1" -v need="$2" '
    BEGIN {
      split(have, h, ".")
      split(need, n, ".")
      for (i = 1; i <= 3; i++) {
        if ((h[i] + 0) > (n[i] + 0)) exit 0
        if ((h[i] + 0) < (n[i] + 0)) exit 1
      }
      exit 0
    }'
}
version_at_least "$project_go" "$required_go" || {
  printf 'govulncheck %s requires Go %s, but go.mod declares Go %s\n' \
    "$expected" "$required_go" "$project_go" >&2
  exit 1
}

printf 'govulncheck pin %s is shared by the workflow and image; requires Go %s; project declares Go %s\n' \
  "$expected" "$required_go" "$project_go"
