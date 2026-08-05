package mango

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"axctl/pkg/ipc"
)

type watchSession struct {
	conn net.Conn
	br   *bufio.Reader

	closeOnce sync.Once
	done      chan struct{}

	updates chan ipc.Event
}

func openWatches(socketPath string, events []string) (*watchSession, error) {
	conn, err := net.DialTimeout("unix", socketPath, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("dial watches: %w", err)
	}
	if _, err := io.WriteString(conn, "watch "+strings.Join(events, " ")+"\n"); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("send watch: %w", err)
	}
	s := &watchSession{
		conn:    conn,
		br:      bufio.NewReader(conn),
		done:    make(chan struct{}),
		updates: make(chan ipc.Event, 64),
	}
	go s.loop()
	return s, nil
}

func (s *watchSession) Updates() <-chan ipc.Event { return s.updates }

func (s *watchSession) Close() {
	s.closeOnce.Do(func() {
		if s.conn != nil {
			_ = s.conn.Close()
		}
		close(s.done)
	})
}

func (s *watchSession) loop() {
	defer close(s.updates)
	for {
		line, err := s.br.ReadBytes('\n')
		if err != nil {
			return
		}
		line = bytes.TrimRight(line, "\r\n")
		if len(line) == 0 {
			continue
		}
		ev := s.mapLine(line)
		if ev.Type == "" {
			continue
		}
		select {
		case s.updates <- ev:
		case <-s.done:
			return
		}
	}
}

func (s *watchSession) mapLine(line []byte) ipc.Event {
	now := time.Now().Unix()

	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return ipc.Event{}
	}

	if trimmed[0] == '[' {
		var arr []map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &arr); err != nil || len(arr) == 0 {
			return rawEvent(trimmed, now)
		}
		first := arr[0]
		switch {
		case hasKey(first, "appid"), hasKey(first, "title"):
			var cs []mangoClient
			if err := json.Unmarshal(trimmed, &cs); err == nil {
				return clientsListEvent(cs, now)
			}
		case hasKey(first, "width"), hasKey(first, "height"):
			var ms []mangoMonitor
			if err := json.Unmarshal(trimmed, &ms); err == nil {
				return monitorsListEvent(ms, now)
			}
		case hasKey(first, "index"):
			var ts []mangoTag
			if err := json.Unmarshal(trimmed, &ts); err == nil {
				return tagsListEvent(ts, now)
			}
		}
		return rawEvent(trimmed, now)
	}

	if trimmed[0] == '{' {
		var probe map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &probe); err != nil {
			return rawEvent(trimmed, now)
		}
		if hasKey(probe, "appid") || hasKey(probe, "title") {
			var c mangoClient
			if err := json.Unmarshal(trimmed, &c); err == nil {
				return focusingClientEvent(&c, now)
			}
		}
		if hasKey(probe, "x") && hasKey(probe, "y") {
			var cur mangoCursorPos
			if err := json.Unmarshal(trimmed, &cur); err == nil {
				return ipc.Event{Type: ipc.EventWorkspaceChanged, Timestamp: now,
					Payload: map[string]interface{}{"x": cur.X, "y": cur.Y}}
			}
		}
		return rawEvent(trimmed, now)
	}

	return rawEvent(trimmed, now)
}

func hasKey(m map[string]json.RawMessage, key string) bool {
	_, ok := m[key]
	return ok
}

func rawEvent(line []byte, now int64) ipc.Event {
	return ipc.Event{Type: ipc.EventConfigReloaded, Timestamp: now,
		Payload: map[string]interface{}{"raw": string(line)}}
}

func clientsListEvent(cs []mangoClient, now int64) ipc.Event {
	wins := make([]ipc.Window, 0, len(cs))
	for i := range cs {
		c := &cs[i]
		wins = append(wins, ipc.Window{
			ID:           fmt.Sprintf("%d", c.ID),
			Title:        c.Title,
			AppID:        c.AppID,
			IsFloating:   c.Floating != 0,
			IsFullscreen: c.Fullscreen != 0,
			Metadata: map[string]interface{}{
				"monitor":    c.MonitorName,
				"monitor_id": c.MonitorName,
				"maximized":  c.Maximized != 0,
				"global":     c.Global != 0,
			},
		})
	}
	return ipc.Event{Type: ipc.EventWindowCreated, Timestamp: now,
		Payload: map[string]interface{}{"windows": wins, "count": len(wins)}}
}

func monitorsListEvent(ms []mangoMonitor, now int64) ipc.Event {
	names := make([]string, 0, len(ms))
	active := ""
	for _, m := range ms {
		names = append(names, m.Name)
		if m.Active != 0 {
			active = m.Name
		}
	}
	return ipc.Event{Type: ipc.EventMonitorChanged, Timestamp: now,
		Payload: map[string]interface{}{"monitors": names, "active": active}}
}

func tagsListEvent(ts []mangoTag, now int64) ipc.Event {
	active := ""
	for _, t := range ts {
		if t.Active != 0 {
			active = t.Name
		}
	}
	return ipc.Event{Type: ipc.EventWorkspaceChanged, Timestamp: now,
		Payload: map[string]interface{}{"tags": ts, "active": active}}
}

func focusingClientEvent(c *mangoClient, now int64) ipc.Event {
	w := ipc.Window{
		ID:           fmt.Sprintf("%d", c.ID),
		Title:        c.Title,
		AppID:        c.AppID,
		IsFocused:    true,
		IsFloating:   c.Floating != 0,
		IsFullscreen: c.Fullscreen != 0,
		Metadata: map[string]interface{}{
			"monitor":    c.MonitorName,
			"monitor_id": c.MonitorName,
			"maximized":  c.Maximized != 0,
			"global":     c.Global != 0,
		},
	}
	return ipc.Event{Type: ipc.EventWindowFocused, Timestamp: now,
		Window: &w,
		Payload: map[string]interface{}{
			"id": w.ID, "title": c.Title, "appid": c.AppID, "monitor": c.MonitorName,
		}}
}
