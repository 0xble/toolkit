package sendguard

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTornTrailingLedgerBlocksThenExpires(t *testing.T) {
	for _, fragment := range []string{`{"hash":"partial`, `{"hash":"*","claimed_at":"2026-01-01T00:00:00Z","outcome":"not_started"}`} {
		t.Run(fragment, func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Second)
			opts := testOptions(t.TempDir(), &now)
			path, err := statePath(testKey(), opts.stateDir)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(fragment), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(path, now, now); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				_, err := ClaimWithOptions(context.Background(), testKey(), time.Minute, opts)
				var duplicate *DuplicateError
				if !errors.As(err, &duplicate) || !duplicate.ClaimedAt.Equal(now) {
					t.Fatalf("torn record must remain an unverified block: %v", err)
				}
			}
			// The repair must be durable JSONL, not a malformed fragment that
			// permanently prevents any later append or claim.
			data, err := os.ReadFile(path)
			if err != nil || len(data) == 0 || data[len(data)-1] != '\n' {
				t.Fatalf("ledger not repaired: %q, %v", data, err)
			}
			var repaired ledgerEntry
			if err := json.Unmarshal(data, &repaired); err != nil || repaired.Outcome != outcomeUnverified {
				t.Fatalf("repair accepted an unverified release: %+v, %v", repaired, err)
			}
			now = now.Add(time.Minute)
			claim, err := ClaimWithOptions(context.Background(), testKey(), time.Minute, opts)
			if err != nil {
				t.Fatalf("torn block did not expire: %v", err)
			}
			if err := claim.Sent(); err != nil {
				t.Fatal(err)
			}
			if _, err := ClaimWithOptions(context.Background(), testKey(), time.Minute, opts); !errors.As(err, new(*DuplicateError)) {
				t.Fatalf("new durable claim not blocking: %v", err)
			}
		})
	}
}

func TestTornOutcomeCannotReleaseExistingClaim(t *testing.T) {
	now := time.Now().UTC()
	opts := testOptions(t.TempDir(), &now)
	claim, err := ClaimWithOptions(context.Background(), testKey(), time.Minute, opts)
	if err != nil {
		t.Fatal(err)
	}
	r := claim.(*release)
	f, err := os.OpenFile(r.path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(ledgerEntry{Hash: r.hash, ClaimedAt: r.claimedAt.Format(time.RFC3339Nano), Outcome: outcomeNotStarted})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := ClaimWithOptions(context.Background(), testKey(), time.Minute, opts); !errors.As(err, new(*DuplicateError)) {
		t.Fatalf("unterminated outcome released reservation: %v", err)
	}
}

func TestFinishClaimLockTimeoutPreservesReservation(t *testing.T) {
	now := time.Now().UTC()
	opts := testOptions(t.TempDir(), &now)
	claim, err := ClaimWithOptions(context.Background(), testKey(), time.Minute, opts)
	if err != nil {
		t.Fatal(err)
	}
	r := claim.(*release)
	lock, err := acquireLock(context.Background(), r.path+".lock")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Close() }()
	if err := claim.NotStarted(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("finalization must have a bounded lock wait: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := ClaimWithOptions(context.Background(), testKey(), time.Minute, opts); !errors.As(err, new(*DuplicateError)) {
		t.Fatalf("timed-out release removed reservation: %v", err)
	}
	if err := claim.NotStarted(); err != nil {
		t.Fatalf("finalization must be retryable: %v", err)
	}
	if _, err := ClaimWithOptions(context.Background(), testKey(), time.Minute, opts); err != nil {
		t.Fatalf("verified not-started did not release: %v", err)
	}
}
