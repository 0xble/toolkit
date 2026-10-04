# Execution image for ./bin/ci. The gate builds it from the PR base's reviewed
# ci/, never from the candidate.
FROM golang:1.27.1-bookworm@sha256:69a7b9788769bec032d238959b61854e9ae87f57be9029ec04e9885fabf99195
ENV GOTOOLCHAIN=local GOPATH=/opt/go GOBIN=/usr/local/bin
RUN go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 \
    && go install golang.org/x/vuln/cmd/govulncheck@v1.8.0 \
    && rm -rf /opt/go /root/.cache \
    && useradd --uid 1000 --create-home ci
ENV CI=true LANG=C.UTF-8 LC_ALL=C.UTF-8 GOPATH=/home/ci/go
USER 1000:1000
WORKDIR /workspace
