package toolkit

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"time"
)

// ListenUnix listens on a Unix socket at path with mode 0600, so only the
// owning user (and a proxy running as that user) can connect. A stale socket
// left by a crashed server is replaced; a live one is an error. The socket is
// bound under a private temporary name and renamed into place, so it is never
// reachable with a wider mode.
func ListenUnix(path string) (net.Listener, error) {
	if err := removeStale(path); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(filepath.Dir(path), ".sock-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	tmp := filepath.Join(dir, "s")
	ln, err := net.Listen("unix", tmp)
	if err != nil {
		return nil, err
	}
	ul := ln.(*net.UnixListener)
	ul.SetUnlinkOnClose(false)
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = ln.Close()
		return nil, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = ln.Close()
		return nil, err
	}
	return ul, nil
}

func removeStale(path string) error {
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if fi.Mode().Type() != fs.ModeSocket {
		return fmt.Errorf("%s exists and is not a socket", path)
	}
	if c, err := net.DialTimeout("unix", path, time.Second); err == nil {
		_ = c.Close()
		return fmt.Errorf("%s is already being served", path)
	}
	return os.Remove(path)
}
