# Security Policy

## Reporting a Vulnerability

Report a vulnerability privately through GitHub's private vulnerability
reporting on this repository (Security, then Report a vulnerability). Do not
open a public issue for it.

## Model

- `serve` listens only on a Unix socket created with mode `0600`. It never
  binds a TCP port. Anything that exposes the socket further, such as a
  reverse proxy, owns authentication for that hop.
- The default `Authorizer` on served surfaces (HTTP and HTTP MCP) allows reads
  and previews and refuses any call that would apply a write or destructive
  change. A tool must opt in to remote writes with its own `Authorizer`.
- Local surfaces (the CLI and stdio MCP) run as the invoking user and allow
  writes, as running the binary directly would.
- A destructive operation applies only with both `apply` and `confirm` on
  every surface. The check runs in the registry before authorization and
  before the handler.
- Inputs are validated against the operation's JSON Schema before a handler
  runs. Request bodies are capped at 1 MiB.

## Repository Rules

- Secrets and private provider payloads must never be committed or logged.
- Test fixtures are synthetic. Tests use in-memory fakes and loopback only.
