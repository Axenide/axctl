//go:build linux

package server

import (
	"fmt"
	"net"
	"os"

	"golang.org/x/sys/unix"
)

// verifyPeerUID rejects connections from processes running as a different
// user. The socket lives in the per-user runtime dir, but this still
// guards against a listener planted on a legacy /tmp path.
func verifyPeerUID(conn net.Conn) error {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return fmt.Errorf("connection is not a unix socket")
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return err
	}
	var cred *unix.Ucred
	var sockErr error
	if ctrlErr := raw.Control(func(fd uintptr) {
		cred, sockErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); ctrlErr != nil {
		return ctrlErr
	}
	if sockErr != nil {
		return sockErr
	}
	if cred == nil {
		return fmt.Errorf("peer credentials unavailable")
	}
	if cred.Uid != uint32(os.Getuid()) {
		return fmt.Errorf("peer uid %d is not authorized", cred.Uid)
	}
	return nil
}
