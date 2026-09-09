package server

import (
	"net"
	"path/filepath"
	"testing"
)

func TestVerifyPeerUIDAcceptsSameUser(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "t.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	done := make(chan error, 1)
	go func() {
		conn, err := l.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		done <- verifyPeerUID(conn)
	}()

	c, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = c.Write([]byte("ping"))
	_ = c.Close()

	if err := <-done; err != nil {
		t.Fatalf("same-uid connection was rejected: %v", err)
	}
}

func TestVerifyPeerUIDRejectsNil(t *testing.T) {
	if err := verifyPeerUID(nil); err == nil {
		t.Fatal("nil connection was not rejected")
	}
}
