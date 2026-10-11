//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd

package sendguard

import (
	"context"
	"errors"
	"os"
)

func lockFile(context.Context, *os.File) error {
	return errors.New("advisory flock is unsupported on this platform")
}

func unlockFile(*os.File) error { return nil }
