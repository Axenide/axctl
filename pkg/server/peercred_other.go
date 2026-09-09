//go:build !linux

package server

import (
	"fmt"
	"net"
)

// verifyPeerUID is a no-op peer check for platforms without SO_PEERCRED.
func verifyPeerUID(conn net.Conn) error {
	if _, ok := conn.(*net.UnixConn); !ok {
		return fmt.Errorf("connection is not a unix socket")
	}
	return nil
}
