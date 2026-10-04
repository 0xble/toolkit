# Agents

Public Go module `github.com/0xble/toolkit`. Read `README.md` for the model.

- Run `./bin/ci preflight` before pushing. `./bin/ci gate` is what the
  required `qualification` check runs, in the image built from `ci/`.
- Set `GOWORK=off` for ad hoc Go commands; `bin/ci` does.
- `output/{errors,format,fields,confirm}.go` are byte-for-byte copies of the
  shared output package used across tools. Change them only together with
  those copies.
- Keep the public API small. Every exported name is a compatibility promise
  once tools pin it.
- Behaviour shared by all surfaces (validation, apply and confirm, error
  kinds) belongs in `op`, not in an adapter.
- Tests use in-memory fakes and loopback only: no credentials, no live APIs.
- Nothing personal in this repository: no private hostnames, paths, account
  names or business names.
