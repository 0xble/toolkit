package sendguard

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func claimLocal(ctx context.Context, key Key, window time.Duration, opts Options, now func() time.Time) (Release, error) {
	path, err := statePath(key, opts.stateDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("sendguard: create state directory: %w", err)
	}
	lock, err := acquireLock(ctx, path+".lock")
	if err != nil {
		return nil, err
	}
	defer func() { _ = lock.Close() }()

	entries, err := loadLedger(path)
	if err != nil {
		return nil, err
	}
	claimedAt := now().UTC()
	originalLen := len(entries)
	entries = prune(entries, claimedAt)
	hash := digest(key)
	if !opts.AllowDuplicate {
		if prior, ok := blocking(entries, hash, claimedAt, window); ok {
			at, _ := prior.time() // loadLedger validated every timestamp.
			return nil, &DuplicateError{ClaimedAt: at}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(entries) != originalLen {
		if err := rewriteLedger(path, entries); err != nil {
			return nil, err
		}
	}
	// claimed_at also identifies this reservation. Keep it distinct even
	// when the clock has coarse resolution or moves backward.
	for _, entry := range entries {
		if entry.Hash == hash {
			at, _ := entry.time()
			if !claimedAt.After(at) {
				claimedAt = at.Add(time.Nanosecond)
			}
		}
	}
	entry := ledgerEntry{Hash: hash, ClaimedAt: claimedAt.Format(time.RFC3339Nano), Outcome: outcomeUnverified}
	if err := appendLedger(path, entry); err != nil {
		return nil, err
	}
	return &release{path: path, hash: hash, claimedAt: claimedAt, now: now}, nil
}

func finishClaim(path, hash string, claimedAt time.Time, result outcome, now time.Time) error {
	ctx, cancel := context.WithTimeout(context.Background(), finishLockTimeout)
	defer cancel()
	lock, err := acquireLock(ctx, path+".lock")
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	entries, err := loadLedger(path)
	if err != nil {
		return err
	}
	originalLen := len(entries)
	entries = prune(entries, now.UTC())
	if len(entries) != originalLen {
		if err := rewriteLedger(path, entries); err != nil {
			return err
		}
	}
	stamp := claimedAt.UTC().Format(time.RFC3339Nano)
	for i := len(entries) - 1; i >= 0; i-- {
		prior := entries[i]
		if prior.Hash == hash && prior.ClaimedAt == stamp {
			if prior.Outcome != outcomeUnverified {
				return nil
			}
			return appendLedger(path, ledgerEntry{Hash: hash, ClaimedAt: stamp, Outcome: result})
		}
	}
	// An expired, pruned claim cannot alter another reservation.
	return nil
}

func blocks(entry ledgerEntry, now time.Time, window time.Duration) bool {
	if entry.Outcome == outcomeNotStarted {
		return false
	}
	at, err := entry.time()
	if err != nil {
		return true
	}
	return now.Before(at.Add(window))
}

func blocking(entries []ledgerEntry, hash string, now time.Time, window time.Duration) (ledgerEntry, bool) {
	seen := make(map[string]bool)
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if (entry.Hash != hash && entry.Hash != tornHash) || seen[entry.Hash+entry.ClaimedAt] {
			continue
		}
		seen[entry.Hash+entry.ClaimedAt] = true
		if blocks(entry, now, window) {
			return entry, true
		}
	}
	return ledgerEntry{}, false
}

func prune(entries []ledgerEntry, now time.Time) []ledgerEntry {
	cutoff := now.Add(-retention)
	out := entries[:0]
	for _, entry := range entries {
		at, err := entry.time()
		if err == nil && !at.Before(cutoff) {
			out = append(out, entry)
		}
	}
	return out
}

func loadLedger(path string) ([]ledgerEntry, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("sendguard: open ledger: %w", err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("sendguard: stat ledger: %w", err)
	}
	entries, torn, err := decodeLedger(f, info.ModTime())
	if err != nil {
		return nil, err
	}
	if torn {
		// Repair the fragment before any append, preserving a bounded,
		// unverified block instead of leaving permanent malformed JSON.
		if err := rewriteLedger(path, entries); err != nil {
			return nil, err
		}
	}
	return entries, nil
}

const (
	tornHash          = "*" // A torn claim may not reveal which message was reserved.
	finishLockTimeout = time.Second
)

func decodeLedger(r io.Reader, modified time.Time) ([]ledgerEntry, bool, error) {
	s := bufio.NewScanner(r)
	s.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			return i + 1, data[:i+1], nil
		}
		if atEOF && len(data) > 0 {
			return len(data), data, nil
		}
		return 0, nil, nil
	})
	var entries []ledgerEntry
	torn := false
	for s.Scan() {
		line := s.Bytes()
		if line[len(line)-1] != '\n' {
			// Even valid JSON is not committed without its newline. Keep an
			// unknown claim blocking from the last write, never a release.
			entries = append(entries, ledgerEntry{Hash: tornHash, ClaimedAt: modified.UTC().Format(time.RFC3339Nano), Outcome: outcomeUnverified})
			torn = true
			continue
		}
		var entry ledgerEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			return nil, false, fmt.Errorf("sendguard: decode ledger: %w", err)
		}
		if (len(entry.Hash) != 64 && entry.Hash != tornHash) || strings.TrimSpace(entry.ClaimedAt) == "" {
			return nil, false, errors.New("sendguard: invalid ledger entry")
		}
		switch entry.Outcome {
		case outcomeSent, outcomeNotStarted, outcomeUnverified:
		default:
			return nil, false, fmt.Errorf("sendguard: invalid ledger outcome %q", entry.Outcome)
		}
		if _, err := entry.time(); err != nil {
			return nil, false, fmt.Errorf("sendguard: invalid ledger timestamp: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := s.Err(); err != nil {
		return nil, false, fmt.Errorf("sendguard: read ledger: %w", err)
	}
	return entries, torn, nil
}

func rewriteLedger(path string, entries []ledgerEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("sendguard: create state directory: %w", err)
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".sendguard-*")
	if err != nil {
		return fmt.Errorf("sendguard: create ledger temp: %w", err)
	}
	temp := f.Name()
	defer func() { _ = os.Remove(temp) }()
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return fmt.Errorf("sendguard: set ledger mode: %w", err)
	}
	enc := json.NewEncoder(f)
	for _, entry := range entries {
		if err := enc.Encode(entry); err != nil {
			_ = f.Close()
			return fmt.Errorf("sendguard: encode ledger: %w", err)
		}
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("sendguard: sync ledger: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("sendguard: close ledger temp: %w", err)
	}
	if err := os.Rename(temp, path); err != nil {
		return fmt.Errorf("sendguard: replace ledger: %w", err)
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("sendguard: open ledger directory: %w", err)
	}
	defer func() { _ = dir.Close() }()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("sendguard: sync ledger directory: %w", err)
	}
	return nil
}

func appendLedger(path string, entry ledgerEntry) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("sendguard: open ledger for append: %w", err)
	}
	defer func() { _ = f.Close() }()
	if err := json.NewEncoder(f).Encode(entry); err != nil {
		return fmt.Errorf("sendguard: append ledger: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sendguard: sync ledger append: %w", err)
	}
	return nil
}
