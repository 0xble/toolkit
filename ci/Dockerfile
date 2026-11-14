# Execution image for ./bin/ci. The gate builds it from the PR base's reviewed
# ci/, never from the candidate.
FROM golang:1.27.4-bookworm@sha256:fb7cceb528b47141eccd9b60bf3cbd27d5ef5f8cf5b7d338bec866da495b50dc
ENV GOTOOLCHAIN=local GOPATH=/opt/go GOBIN=/usr/local/bin
RUN apt-get update \
    && apt-get install -y --no-install-recommends jq=1.6-2.1+deb12u3 \
    && rm -rf /var/lib/apt/lists/*
COPY govulncheck-version /tmp/govulncheck-version
RUN vuln_version="$(tr -d '[:space:]' </tmp/govulncheck-version)" \
    && go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 \
    && go install "golang.org/x/vuln/cmd/govulncheck@$vuln_version" \
    && rm -rf /opt/go /root/.cache /tmp/govulncheck-version \
    && useradd --uid 1000 --create-home ci
ENV CI=true LANG=C.UTF-8 LC_ALL=C.UTF-8 GOPATH=/home/ci/go
USER 1000:1000
WORKDIR /workspace
