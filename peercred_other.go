//go:build !darwin && !linux

package toolkit

import (
	"errors"
	"net"
)

// peerUID is unsupported here, so --allow-apply never finds a local owner.
func peerUID(*net.UnixConn) (int, error) {
	return 0, errors.New("peer credentials are not supported on this platform")
}
