// Package sendguard provides a process-safe duplicate-send gate for outbound
// messages. It stores only a digest of the normalized message, never the body.
package sendguard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/0xble/toolkit/op"
	"golang.org/x/text/unicode/norm"
)

const (
	// DefaultWindow is used when Claim receives a non-positive window.
	DefaultWindow = 10 * time.Minute
	retention     = 24 * time.Hour
	duplicateCode = "duplicate_send"
)

// Key identifies one outbound message. Body is normalized before it is hashed.
// The raw body is never written to the ledger.
type Key struct {
	Tool    string
	Account string
	Target  string
	Body    string
}

type outcome string

const (
	outcomeSent       outcome = "sent"
	outcomeNotStarted outcome = "not_started"
	outcomeUnverified outcome = "unverified"
)

// Release completes a claim. If the caller does not complete it, the initial
// unverified ledger entry remains blocking for the claim window.
type Release interface {
	Sent() error
	NotStarted() error
	Unverified() error
}

// RemoteCheck reports whether the target already contains an identical
// outbound message from this account. A non-zero time is carried into
// DuplicateError when found.
type RemoteCheck func(context.Context, Key, time.Duration) (claimedAt time.Time, found bool, err error)

// Options controls the optional remote-history check and duplicate override.
type Options struct {
	// AllowDuplicate bypasses both the local ledger and RemoteCheck.
	AllowDuplicate bool
	// RemoteCheck checks history outside this process, such as another device.
	RemoteCheck RemoteCheck

	// These fields are test seams kept private so the public API cannot redirect
	// production state or replace its clock.
	stateDir string
	now      func() time.Time
}

// DuplicateError reports a claim that is already blocked by a prior outbound
// send. ClaimedAt is the prior claim's timestamp, not message content.
type DuplicateError struct {
	ClaimedAt time.Time
}

func (e *DuplicateError) Error() string {
	if e == nil {
		return duplicateCode
	}
	return fmt.Sprintf("outbound send is blocked as a duplicate of the claim at %s", e.ClaimedAt.UTC().Format(time.RFC3339Nano))
}

// Unwrap lets op.AsError preserve the shared duplicate-send kind and conflict
// exit/status mapping while callers can still errors.As to DuplicateError.
func (e *DuplicateError) Unwrap() error {
	return &op.Error{Kind: op.KindDuplicateSend, Code: duplicateCode, Message: e.Error()}
}

// Claim reserves an outbound message for DefaultWindow (or the supplied
// positive window). The reservation is atomically checked and appended under
// an exclusive flock on the local ledger.
func Claim(ctx context.Context, key Key, window time.Duration) (Release, error) {
	return ClaimWithOptions(ctx, key, window, Options{})
}

// ClaimWithOptions is Claim with an explicit duplicate override and optional
// remote-history check.
func ClaimWithOptions(ctx context.Context, key Key, window time.Duration, opts Options) (Release, error) {
	if err := validateKey(key); err != nil {
		return nil, err
	}
	if window <= 0 {
		window = DefaultWindow
	}
	if window > retention {
		return nil, errors.New("sendguard: window must not exceed 24 hours")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	now := time.Now
	if opts.now != nil {
		now = opts.now
	}
	if !opts.AllowDuplicate && opts.RemoteCheck != nil {
		claimedAt, found, err := opts.RemoteCheck(ctx, key, window)
		if err != nil {
			return nil, err
		}
		if found {
			if claimedAt.IsZero() {
				claimedAt = now()
			}
			return nil, &DuplicateError{ClaimedAt: claimedAt}
		}
	}
	return claimLocal(ctx, key, window, opts, now)
}

func validateKey(key Key) error {
	if strings.TrimSpace(key.Tool) == "" {
		return errors.New("sendguard: key tool is empty")
	}
	if strings.TrimSpace(key.Target) == "" {
		return errors.New("sendguard: key target is empty")
	}
	if strings.ContainsAny(key.Tool, `/\\`) || key.Tool == "." || key.Tool == ".." {
		return errors.New("sendguard: key tool is not a simple name")
	}
	// Pipes delimit identity components in the digest. Reject them in these
	// fields so two different keys cannot hash the same joined bytes.
	if strings.ContainsAny(key.Tool+key.Account+key.Target, "|\x00") {
		return errors.New("sendguard: identity fields must not contain pipes or NUL")
	}
	return nil
}

// EqualBody reports whether two message bodies are equal after trimming,
// whitespace collapsing and Unicode NFC normalization. RemoteCheck callbacks
// can use it to compare outbound history without duplicating normalization.
func EqualBody(a, b string) bool {
	return normalizeBody(a) == normalizeBody(b)
}

func normalizeBody(body string) string {
	return strings.Join(strings.Fields(norm.NFC.String(body)), " ")
}

func digest(key Key) string {
	body := normalizeBody(key.Body)
	sum := sha256.Sum256([]byte(key.Tool + "|" + key.Account + "|" + key.Target + "|" + body))
	return hex.EncodeToString(sum[:])
}

func statePath(key Key, stateDir string) (string, error) {
	if stateDir == "" {
		stateDir = os.Getenv("XDG_STATE_HOME")
		if stateDir == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("sendguard: resolve home: %w", err)
			}
			stateDir = filepath.Join(home, ".local", "state")
		}
	}
	return filepath.Join(stateDir, "toolkit", "sendguard", key.Tool+".jsonl"), nil
}

type ledgerEntry struct {
	Hash      string  `json:"hash"`
	ClaimedAt string  `json:"claimed_at"`
	Outcome   outcome `json:"outcome"`
}

func (e ledgerEntry) time() (time.Time, error) {
	return time.Parse(time.RFC3339Nano, e.ClaimedAt)
}

type release struct {
	mu        sync.Mutex
	done      bool
	path      string
	hash      string
	claimedAt time.Time
	now       func() time.Time
}

func (r *release) Sent() error       { return r.finish(outcomeSent) }
func (r *release) NotStarted() error { return r.finish(outcomeNotStarted) }
func (r *release) Unverified() error { return r.finish(outcomeUnverified) }

func (r *release) finish(result outcome) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done {
		return errors.New("sendguard: claim release already completed")
	}
	err := finishClaim(r.path, r.hash, r.claimedAt, result, r.now())
	if err == nil {
		r.done = true
	}
	return err
}
