package toolkit_test

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/0xble/toolkit"
)

func shortDir(t *testing.T) string {
	t.Helper()
	// t.TempDir can exceed the 104-byte socket path limit on macOS.
	dir, err := os.MkdirTemp("", "tk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func TestListenUnix(t *testing.T) {
	path := filepath.Join(shortDir(t), "s.sock")
	ln, err := toolkit.ListenUnix(path)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 || fi.Mode().Type() != os.ModeSocket {
		t.Fatalf("socket: %v %v", fi.Mode(), err)
	}
	c, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("dial the renamed socket: %v", err)
	}
	_ = c.Close()
	if _, err := toolkit.ListenUnix(path); err == nil {
		t.Error("a live socket must not be replaced")
	}
	_ = ln.Close()
	// The listener does not unlink, so the stale socket stays and is replaced.
	ln2, err := toolkit.ListenUnix(path)
	if err != nil {
		t.Fatalf("replace a stale socket: %v", err)
	}
	_ = ln2.Close()

	file := filepath.Join(filepath.Dir(path), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := toolkit.ListenUnix(file); err == nil {
		t.Error("a regular file must not be replaced")
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 2 {
		t.Errorf("temporary bind directories were left behind: %v", entries)
	}
}
