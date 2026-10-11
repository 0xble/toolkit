package sendguard

import (
	"context"
	"fmt"
	"os"
)

type fileLock struct {
	file *os.File
}

func acquireLock(ctx context.Context, path string) (*fileLock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("sendguard: open lock: %w", err)
	}
	if err := lockFile(ctx, f); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("sendguard: acquire lock: %w", err)
	}
	return &fileLock{file: f}, nil
}

func (l *fileLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	if err := unlockFile(l.file); err != nil {
		_ = l.file.Close()
		return fmt.Errorf("sendguard: release lock: %w", err)
	}
	return l.file.Close()
}
