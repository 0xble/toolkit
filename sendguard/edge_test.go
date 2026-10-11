package sendguard

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestEqualBody(t *testing.T) {
	for _, pair := range [][2]string{{"\t café \n here ", "cafe\u0301\u2003here"}, {" \r\n ", ""}, {"a\u00a0b", "a b"}} {
		if !EqualBody(pair[0], pair[1]) {
			t.Fatalf("%q != %q after normalization", pair[0], pair[1])
		}
	}
	if EqualBody("Case", "case") || EqualBody("a", "b") {
		t.Fatal("normalization must not erase case or content")
	}
}

func TestNotStartedOverrideDoesNotReleaseEarlierSentClaim(t *testing.T) {
	now := time.Now().UTC()
	opts := testOptions(t.TempDir(), &now)
	first, err := ClaimWithOptions(context.Background(), testKey(), 0, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Sent(); err != nil {
		t.Fatal(err)
	}
	opts.AllowDuplicate = true
	second, err := ClaimWithOptions(context.Background(), testKey(), 0, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.NotStarted(); err != nil {
		t.Fatal(err)
	}
	opts.AllowDuplicate = false
	_, err = ClaimWithOptions(context.Background(), testKey(), 0, opts)
	if !errors.As(err, new(*DuplicateError)) {
		t.Fatalf("earlier sent claim was lost: %v", err)
	}
}

func TestStaleReleaseDoesNotReleaseNewClaim(t *testing.T) {
	now := time.Now().UTC()
	opts := testOptions(t.TempDir(), &now)
	first, err := ClaimWithOptions(context.Background(), testKey(), time.Minute, opts)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if _, err := ClaimWithOptions(context.Background(), testKey(), time.Minute, opts); err != nil {
		t.Fatal(err)
	}
	if err := first.NotStarted(); err != nil {
		t.Fatal(err)
	}
	_, err = ClaimWithOptions(context.Background(), testKey(), time.Minute, opts)
	if !errors.As(err, new(*DuplicateError)) {
		t.Fatalf("stale release removed newer reservation: %v", err)
	}
}

func TestRemoteFailuresAndOverride(t *testing.T) {
	now := time.Now().UTC()
	opts := testOptions(t.TempDir(), &now)
	failure := errors.New("history unavailable")
	calls := 0
	opts.RemoteCheck = func(context.Context, Key, time.Duration) (time.Time, bool, error) {
		calls++
		return time.Time{}, false, failure
	}
	if _, err := ClaimWithOptions(context.Background(), testKey(), 0, opts); !errors.Is(err, failure) {
		t.Fatalf("remote errors must fail closed: %v", err)
	}
	opts.AllowDuplicate = true
	if _, err := ClaimWithOptions(context.Background(), testKey(), 0, opts); err != nil || calls != 1 {
		t.Fatalf("override must skip remote check: err=%v calls=%d", err, calls)
	}
}

func TestDefaultWindowAndValidation(t *testing.T) {
	now := time.Now().UTC()
	opts := testOptions(t.TempDir(), &now)
	if _, err := ClaimWithOptions(context.Background(), testKey(), 0, opts); err != nil {
		t.Fatal(err)
	}
	now = now.Add(DefaultWindow - time.Nanosecond)
	if _, err := ClaimWithOptions(context.Background(), testKey(), 0, opts); !errors.As(err, new(*DuplicateError)) {
		t.Fatalf("default window did not block: %v", err)
	}
	now = now.Add(time.Nanosecond)
	if _, err := ClaimWithOptions(context.Background(), testKey(), 0, opts); err != nil {
		t.Fatal(err)
	}
	for _, key := range []Key{{Tool: "../escape", Target: "target"}, {Tool: "tool", Account: "a|b", Target: "target"}, {Tool: "tool"}} {
		if _, err := ClaimWithOptions(context.Background(), key, 0, opts); err == nil {
			t.Fatalf("invalid key accepted: %+v", key)
		}
	}
	if _, err := ClaimWithOptions(context.Background(), testKey(), 25*time.Hour, opts); err == nil {
		t.Fatal("window beyond retention must be rejected")
	}
}

func TestCanceledLockWait(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.lock")
	lock, err := acquireLock(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := acquireLock(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lock wait ignored deadline: %v", err)
	}
}

func TestProcessClaimHelper(t *testing.T) {
	if os.Getenv("SENDGUARD_PROCESS_TEST") != "1" {
		return
	}
	_, err := Claim(context.Background(), testKey(), 0)
	if err == nil {
		os.Exit(0)
	}
	if errors.As(err, new(*DuplicateError)) {
		os.Exit(4)
	}
	os.Exit(2)
}

func TestConcurrentProcessClaimsExactlyOneWins(t *testing.T) {
	dir := t.TempDir()
	cmds := []*exec.Cmd{exec.Command(os.Args[0], "-test.run=^TestProcessClaimHelper$"), exec.Command(os.Args[0], "-test.run=^TestProcessClaimHelper$")}
	for _, cmd := range cmds {
		cmd.Env = append(os.Environ(), "SENDGUARD_PROCESS_TEST=1", "XDG_STATE_HOME="+dir)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
	}
	wins, duplicates := 0, 0
	for _, cmd := range cmds {
		err := cmd.Wait()
		if err == nil {
			wins++
		} else if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 4 {
			duplicates++
		} else {
			t.Fatalf("unexpected child error: %v", err)
		}
	}
	if wins != 1 || duplicates != 1 {
		t.Fatalf("wins=%d duplicates=%d", wins, duplicates)
	}
}
