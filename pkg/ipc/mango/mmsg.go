package mango

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

type wireReply struct {
	Success bool            `json:"success"`
	Error   string          `json:"error,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func looksLikeReply(b []byte) bool {
	if len(b) == 0 || b[0] != '{' {
		return false
	}
	var r wireReply
	if err := json.Unmarshal(b, &r); err != nil {
		return false
	}
	return r.Success || r.Error != ""
}

type mmsgConn struct {
	conn net.Conn
	mu   sync.Mutex
	br   *bufio.Reader
}

func dialMango(socketPath string, timeout time.Duration) (*mmsgConn, error) {
	conn, err := net.DialTimeout("unix", socketPath, timeout)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", socketPath, err)
	}
	return &mmsgConn{
		conn: conn,
		br:   bufio.NewReader(conn),
	}, nil
}

func (m *mmsgConn) Close() error {
	if m == nil || m.conn == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.conn == nil {
		return nil
	}
	err := m.conn.Close()
	m.conn = nil
	return err
}

func (m *mmsgConn) Query(request string, out interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.writeLine(request); err != nil {
		return err
	}
	line, err := m.readLine()
	if err != nil {
		return fmt.Errorf("%s: %w", request, err)
	}
	if looksLikeReply(line) {
		var r wireReply
		if err := json.Unmarshal(line, &r); err != nil {
			return fmt.Errorf("%s: decode reply: %w", request, err)
		}
		if !r.Success {
			if r.Error != "" {
				return fmt.Errorf("%s: %s", request, r.Error)
			}
			return fmt.Errorf("%s: dispatch failed", request)
		}
		if out == nil || len(r.Data) == 0 || string(r.Data) == "null" {
			return nil
		}
		return json.Unmarshal(r.Data, out)
	}
	if out == nil || len(line) == 0 {
		return nil
	}
	return json.Unmarshal(line, out)
}

func (m *mmsgConn) Dispatch(payload string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.writeLine("dispatch " + payload); err != nil {
		return err
	}
	line, err := m.readLine()
	if err != nil {
		return fmt.Errorf("dispatch %q: %w", payload, err)
	}
	var r wireReply
	if err := json.Unmarshal(line, &r); err != nil {
		return fmt.Errorf("dispatch %q: decode reply %q: %w", payload, string(line), err)
	}
	if !r.Success {
		if r.Error != "" {
			return fmt.Errorf("dispatch %q: %s", payload, r.Error)
		}
		return fmt.Errorf("dispatch %q: failed", payload)
	}
	return nil
}

func (m *mmsgConn) WriteRawLine(payload string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.writeLine(payload)
}

func (m *mmsgConn) writeLine(payload string) error {
	if m.conn == nil {
		return fmt.Errorf("connection closed")
	}
	if _, err := io.WriteString(m.conn, payload+"\n"); err != nil {
		return err
	}
	return nil
}

func (m *mmsgConn) readLine() ([]byte, error) {
	if m.br == nil {
		return nil, fmt.Errorf("connection closed")
	}
	line, err := m.br.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	line = bytes.TrimRight(line, "\r\n")
	return line, nil
}
