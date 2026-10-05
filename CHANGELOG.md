# Changelog

Released versions are described in their
[GitHub releases](https://github.com/0xble/toolkit/releases).

## Unreleased

- `op.Request.Meta` carries a copy of an MCP tool call's request `_meta` on
  stdio and HTTP MCP, nil when absent and always nil on the CLI and the HTTP
  API. It is never read from the arguments.
- `serve --allow-apply=OP[,OP...]` allows applied calls to the listed write
  and destructive operations on the HTTP API from a Unix socket peer running
  as the server's uid, without a Tailscale identity header. Confirmation is
  still required for destructive operations, and an unknown or read
  operation fails startup with `invalid_allow_apply`. It adds to the tool's
  own authorizer.
