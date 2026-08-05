package mango

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeRequest struct {
	raw   string
	reply func(t *testing.T, parts []string, w *bufio.Writer)
}

type fakeServer struct {
	t          *testing.T
	socketPath string
	listener   net.Listener
	mu         sync.Mutex

	requests   []string
	watchers   []net.Conn
	watchLines []string

	handler func(t *testing.T, parts []string) (rawJSON string, wrapped bool, ok bool, errMsg string)

	closed   chan struct{}
	closeOne sync.Once
}

func newFakeServer(t *testing.T, handler func(t *testing.T, parts []string) (string, bool, bool, string)) *fakeServer {
	t.Helper()
	dir := t.TempDir()
	socket := filepath.Join(dir, "mango.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("Listen(%s): %v", socket, err)
	}
	s := &fakeServer{
		t:          t,
		socketPath: socket,
		listener:   listener,
		handler:    handler,
		closed:     make(chan struct{}),
	}
	go s.acceptLoop()
	t.Cleanup(s.shutdown)
	return s
}

func (s *fakeServer) addr() string { return s.socketPath }

func (s *fakeServer) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.closed:
				return
			default:
				return
			}
		}
		s.recordConn(conn)
		go s.serve(conn)
	}
}

func (s *fakeServer) recordConn(c net.Conn) {
	s.mu.Lock()
	if strings.HasPrefix(c.RemoteAddr().String(), "@") {
		s.watchers = append(s.watchers, c)
	} else {
		s.watchers = append(s.watchers, c)
	}
	s.mu.Unlock()
}

func (s *fakeServer) serve(conn net.Conn) {
	defer conn.Close()
	br := bufio.NewReader(conn)
	bw := bufio.NewWriter(conn)
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return
			}
			return
		}
		req := strings.TrimRight(line, "\r\n")
		if req == "" {
			continue
		}
		s.recordRequest(req)

		if strings.HasPrefix(req, "watch ") {
			if err := bw.Flush(); err != nil {
				return
			}
			continue
		}

		parts := strings.Fields(req)
		if s.handler != nil {
			raw, wrapped, ok, errMsg := s.handler(s.t, parts)
			if err := writeReply(bw, raw, wrapped, ok, errMsg); err != nil {
				return
			}
			if err := bw.Flush(); err != nil {
				return
			}
			continue
		}
		if err := writeReply(bw, "", false, true, ""); err != nil {
			return
		}
		if err := bw.Flush(); err != nil {
			return
		}
	}
}

func writeReply(w *bufio.Writer, raw string, wrapped, ok bool, errMsg string) error {
	switch {
	case wrapped:
		out := struct {
			Success bool        `json:"success"`
			Error   string      `json:"error,omitempty"`
			Data    interface{} `json:"data,omitempty"`
		}{Success: ok, Error: errMsg}
		if raw != "" {
			out.Data = json.RawMessage(raw)
		}
		b, err := json.Marshal(out)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "%s\n", b); err != nil {
			return err
		}
	default:
		if raw != "" {
			if _, err := fmt.Fprintf(w, "%s\n", raw); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *fakeServer) recordRequest(req string) {
	s.mu.Lock()
	s.requests = append(s.requests, req)
	s.mu.Unlock()
}

func (s *fakeServer) Requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.requests))
	copy(out, s.requests)
	return out
}

func (s *fakeServer) shutdown() {
	s.closeOne.Do(func() {
		_ = s.listener.Close()
		close(s.closed)
	})
}

func (s *fakeServer) Client(t *testing.T) *Mango {
	t.Helper()
	m := &Mango{
		conn:       nil,
		socketPath: s.socketPath,
	}
	conn, err := dialMango(s.socketPath, 2*time.Second)
	if err != nil {
		t.Fatalf("dialMango: %v", err)
	}
	m.conn = conn
	return m
}

func withFakeEnv(t *testing.T, socket string) {
	t.Helper()
	if socket != "" {
		if err := os.Setenv("MANGO_INSTANCE_SIGNATURE", socket); err != nil {
			t.Fatalf("setenv: %v", err)
		}
		t.Cleanup(func() { _ = os.Unsetenv("MANGO_INSTANCE_SIGNATURE") })
	}
}

func mustJSON(t *testing.T, v interface{}) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func okReply(data string) (string, bool, bool, string) {
	return data, false, true, ""
}

func errReply(msg string) (string, bool, bool, string) {
	return "", true, false, msg
}
