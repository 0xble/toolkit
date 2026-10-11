# Outbound duplicate-send guard

Use `sendguard` immediately before an outbound provider call in the shared
operation handler. CLI, HTTP and MCP calls then use the same guard and `op`
error mapping. It is not an adapter option: only the handler can distinguish a
send that never started from one whose outcome is unknown. Previews must not
claim anything.

```go
if !req.Apply {
    return preview, nil
}
claim, err := sendguard.ClaimWithOptions(ctx, sendguard.Key{
    Tool: "messenger", Account: accountID, Target: targetID, Body: in.Body,
}, 0, sendguard.Options{AllowDuplicate: in.AllowDuplicate})
if err != nil {
    return Out{}, err
}
// Call the provider only after Claim succeeds. A cancellation or ambiguous
// provider failure is NOT proof that the send never started.
result, sendErr := provider.Send(ctx, targetID, in.Body)
if sendErr != nil {
    // Leaving the claim unfinished keeps its initial unverified outcome.
    return Out{}, sendErr
}
if err := claim.Sent(); err != nil {
    // The initial durable claim still blocks retries if finalization fails.
    return result, err
}
return result, nil
```

Add an explicit input such as
`AllowDuplicate bool` tagged `json:"allow_duplicate,omitempty"` and
`help:"Allow an identical outbound send within the duplicate window"`.
The CLI exposes `--allow-duplicate`; HTTP and MCP receive the same explicit
input. The override skips both local and remote duplicate checks, but still
records a new local claim.

## Contract

- `Claim(ctx, key, window)` is shorthand for `ClaimWithOptions` with no override
  or remote check. A non-positive window selects `DefaultWindow` (10 minutes).
  A window above 24 hours is rejected because the ledger retains only 24 hours.
- `Key` contains `Tool`, `Account`, `Target` and `Body`. Use stable account and
  target identifiers resolved before claiming, not user-facing aliases.
  `Tool` must be a simple filename component; tool, account and target cannot
  contain `|` or NUL. Account may be empty for a single-account tool.
- Body normalization trims and collapses Unicode whitespace and applies Unicode
  NFC. Case is preserved. The digest is SHA-256 over
  `tool|account|target|normalized body`. `EqualBody(a, b)` exposes this comparison
  for remote-history callbacks. Body, account and target are never persisted.
- `Release.Sent()` confirms a send; `Release.NotStarted()` permits a retry only
  when the caller can prove no outbound send began; `Release.Unverified()` keeps
  blocking. Doing nothing also keeps blocking, including process cancellation
  or a crash. Each release can be completed once; a failed ledger write can be
  retried. A late release cannot release another claim.
- `DuplicateError.ClaimedAt` carries the prior claim's timestamp. Its wrapped
  `op.Error` has kind and code `duplicate_send`, CLI exit 4 (conflict), HTTP 409
  and an MCP `isError` result. No automatic retry is advertised.
- `Options.RemoteCheck` is a caller-supplied
  `func(context.Context, Key, time.Duration) (time.Time, bool, error)`.
  It must filter history to outbound messages from the same account, in the
  same target, within the supplied window, and compare bodies with `EqualBody`.
  A positive result returns `DuplicateError`; an error fails closed. It runs
  before the atomic local claim and is not a distributed lock: simultaneous
  sends on separate devices can still race before either appears in history.

## Local ledger

The ledger is `$XDG_STATE_HOME/toolkit/sendguard/<tool>.jsonl`, or
`~/.local/state/toolkit/sendguard/<tool>.jsonl` when `XDG_STATE_HOME` is unset.
The package creates its state directory with mode 0700 and files with mode
0600. Each JSONL record has only `hash`, `claimed_at` and `outcome`.
Claims and outcomes are appended; writes prune entries older than 24 hours
with atomic replacement under the same lock. A separate `<tool>.jsonl.lock`
file supplies the exclusive flock so compaction does not change the lock inode.
The claim append is synced before the provider can run. Corrupt ledger data
fails closed instead of being silently discarded. An unterminated trailing record
is repaired into an unverified block for all messages in that tool, measured
from the file's last modification time. It expires with the requested window;
even complete JSON without a final newline is not treated as a verified outcome.
Claim finalization waits at most one second for its ledger lock; a timeout leaves
the initial unverified claim blocking and allows finalization to be retried.

This protects concurrent processes on one machine on flock-capable Unix
platforms. An unsupported platform fails closed. Put state on a local
filesystem with working flock semantics; the ledger is not a cross-machine
lock or a replacement for provider-side idempotency.

## Five-line adoption recipe

1. Pin the toolkit release and import `github.com/0xble/toolkit/sendguard`.
2. Add `AllowDuplicate` to the send operation input; resolve stable account and target IDs.
3. After the apply/preview gate, call `ClaimWithOptions` before any outbound provider call.
4. On success call `Sent`; use `NotStarted` only for proven pre-send failures; leave ambiguous outcomes unverified.
5. Return duplicate errors unchanged, and optionally supply `RemoteCheck` using `EqualBody` for outbound history.
