package sendguard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/0xble/toolkit/op"
)

func testOptions(dir string, now *time.Time) Options {
	return Options{stateDir: dir, now: func() time.Time { return *now }}
}

func testKey() Key {
	return Key{Tool: "imsg", Account: "self", Target: "family", Body: "  hello\tworld  "}
}

func TestClaimNormalizesBodyAndStoresOnlyDigest(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	key := testKey()
	release, err := ClaimWithOptions(context.Background(), key, time.Minute, testOptions(dir, &now))
	if err != nil {
		t.Fatal(err)
	}
	if err := release.NotStarted(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "toolkit", "sendguard", "imsg.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := normalizeBody(key.Body)
	sum := sha256.Sum256([]byte(key.Tool + "|" + key.Account + "|" + key.Target + "|" + body))
	want := hex.EncodeToString(sum[:])
	if !strings.Contains(string(data), want) || strings.Contains(string(data), "hello") {
		t.Fatalf("ledger=%q, want digest only", data)
	}
}

func TestDuplicateWindowAndExpiry(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	opts := testOptions(dir, &now)
	if _, err := ClaimWithOptions(context.Background(), testKey(), time.Minute, opts); err != nil {
		t.Fatal(err)
	}
	_, err := ClaimWithOptions(context.Background(), testKey(), time.Minute, opts)
	var duplicate *DuplicateError
	if !errors.As(err, &duplicate) || !duplicate.ClaimedAt.Equal(now) {
		t.Fatalf("err=%v, duplicate=%+v", err, duplicate)
	}
	now = now.Add(time.Minute + time.Nanosecond)
	release, err := ClaimWithOptions(context.Background(), testKey(), time.Minute, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := release.NotStarted(); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentClaimsExactlyOneWins(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	opts := testOptions(dir, &now)
	type result struct {
		release Release
		err     error
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := ClaimWithOptions(context.Background(), testKey(), time.Minute, opts)
			results <- result{release: release, err: err}
		}()
	}
	wg.Wait()
	close(results)
	var wins, duplicates int
	for result := range results {
		if result.err == nil {
			wins++
			if err := result.release.NotStarted(); err != nil {
				t.Error("release failed")
			}
			continue
		}
		var duplicate *DuplicateError
		if errors.As(result.err, &duplicate) {
			duplicates++
		} else {
			t.Errorf("unexpected error: %v", result.err)
		}
	}
	if wins != 1 || duplicates != 1 {
		t.Fatalf("wins=%d duplicates=%d", wins, duplicates)
	}
}

func TestNotStartedAllowsRetry(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	opts := testOptions(dir, &now)
	first, err := ClaimWithOptions(context.Background(), testKey(), time.Minute, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.NotStarted(); err != nil {
		t.Fatal(err)
	}
	second, err := ClaimWithOptions(context.Background(), testKey(), time.Minute, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.NotStarted(); err != nil {
		t.Fatal(err)
	}
}

func TestUnverifiedKeepsBlocking(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	opts := testOptions(dir, &now)
	first, err := ClaimWithOptions(context.Background(), testKey(), time.Minute, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Unverified(); err != nil {
		t.Fatal(err)
	}
	if _, err := ClaimWithOptions(context.Background(), testKey(), time.Minute, opts); !errors.As(err, new(*DuplicateError)) {
		t.Fatalf("err=%v, want duplicate", err)
	}
}

func TestOverrideBypassesAndBecomesBlockingClaim(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	opts := testOptions(dir, &now)
	first, err := ClaimWithOptions(context.Background(), testKey(), time.Minute, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Sent(); err != nil {
		t.Fatal(err)
	}
	opts.AllowDuplicate = true
	second, err := ClaimWithOptions(context.Background(), testKey(), time.Minute, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Sent(); err != nil {
		t.Fatal(err)
	}
	opts.AllowDuplicate = false
	if _, err := ClaimWithOptions(context.Background(), testKey(), time.Minute, opts); !errors.As(err, new(*DuplicateError)) {
		t.Fatalf("err=%v, want duplicate from override claim", err)
	}
}

func TestRemoteCheck(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	remoteAt := now.Add(-time.Second)
	called := false
	opts := testOptions(dir, &now)
	opts.RemoteCheck = func(context.Context, Key, time.Duration) (time.Time, bool, error) {
		called = true
		return remoteAt, true, nil
	}
	_, err := ClaimWithOptions(context.Background(), testKey(), time.Minute, opts)
	var duplicate *DuplicateError
	if !called || !errors.As(err, &duplicate) || !duplicate.ClaimedAt.Equal(remoteAt) {
		t.Fatalf("called=%v err=%v duplicate=%+v", called, err, duplicate)
	}
	if _, err := os.Stat(filepath.Join(dir, "toolkit")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("remote duplicate should not create local state, stat err=%v", err)
	}
}

func TestDuplicateErrorMapsToOperationKind(t *testing.T) {
	err := &DuplicateError{ClaimedAt: time.Now()}
	mapped := op.AsError(err, op.KindError)
	if mapped.Kind != op.KindDuplicateSend || mapped.Code != duplicateCode || mapped.Kind.ExitCode() != 4 || mapped.Kind.HTTPStatus() != 409 {
		t.Fatalf("mapped=%+v", mapped)
	}
}

func TestPrunesOldEntriesOnWrite(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(dir, "toolkit", "sendguard", "imsg.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	old := ledgerEntry{Hash: strings.Repeat("a", 64), ClaimedAt: now.Add(-retention - time.Second).Format(time.RFC3339Nano), Outcome: outcomeSent}
	if err := rewriteLedger(path, []ledgerEntry{old}); err != nil {
		t.Fatal(err)
	}
	release, err := ClaimWithOptions(context.Background(), testKey(), time.Minute, testOptions(dir, &now))
	if err != nil {
		t.Fatal(err)
	}
	if err := release.NotStarted(); err != nil {
		t.Fatal(err)
	}
	entries, err := loadLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Hash == old.Hash {
			t.Fatal("old entry was not pruned")
		}
	}
}
