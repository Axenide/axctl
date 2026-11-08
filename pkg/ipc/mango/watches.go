package mango

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"reflect"
	"strconv"
	"sync"
	"time"

	"axctl/pkg/ipc"
)

const mangoClientsSnapshot ipc.EventType = "mango_clients_snapshot"

type watchSession struct {
	conns     []net.Conn
	closeOnce sync.Once
	done      chan struct{}
	updates   chan ipc.Event
}

func openWatches(socketPath string, events []string) (*watchSession, error) {
	s := &watchSession{
		done:    make(chan struct{}),
		updates: make(chan ipc.Event, 128),
	}
	for _, event := range events {
		conn, err := net.DialTimeout("unix", socketPath, 5*time.Second)
		if err != nil {
			s.Close()
			return nil, fmt.Errorf("dial watch %s: %w", event, err)
		}
		s.conns = append(s.conns, conn)
		_ = conn.SetDeadline(time.Time{})
		if _, err := io.WriteString(conn, "watch "+event+"\n"); err != nil {
			s.Close()
			return nil, fmt.Errorf("send watch %s: %w", event, err)
		}
	}
	var workers sync.WaitGroup
	workers.Add(len(s.conns))
	for _, conn := range s.conns {
		go func(c net.Conn) {
			defer workers.Done()
			s.readLoop(c)
		}(conn)
	}
	go func() {
		workers.Wait()
		close(s.updates)
	}()
	return s, nil
}

func (s *watchSession) Updates() <-chan ipc.Event { return s.updates }

func (s *watchSession) Close() {
	s.closeOnce.Do(func() {
		close(s.done)
		for _, conn := range s.conns {
			_ = conn.Close()
		}
	})
}

func (s *watchSession) readLoop(conn net.Conn) {
	br := bufio.NewReader(conn)
	for {
		line, err := br.ReadBytes('\n')
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
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return ipc.Event{}
	}
	now := time.Now().Unix()
	if trimmed[0] == '[' {
		return s.mapArray(trimmed, now)
	}
	if trimmed[0] != '{' {
		return ipc.Event{}
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return ipc.Event{}
	}
	switch {
	case hasKey(raw, "clients"):
		var clients mangoClients
		if json.Unmarshal(trimmed, &clients) != nil {
			return ipc.Event{}
		}
		return clientsListEvent(clients.Clients, now)
	case hasKey(raw, "monitors"):
		var monitors mangoMonitors
		if json.Unmarshal(trimmed, &monitors) != nil {
			return ipc.Event{}
		}
		return monitorsListEvent(monitors.Monitors, now)
	case hasKey(raw, "all_tags"):
		var tags mangoAllTags
		if json.Unmarshal(trimmed, &tags) != nil {
			return ipc.Event{}
		}
		return tagsListEvent(tags.Monitors, now)
	case hasKey(raw, "appid") || hasKey(raw, "title"):
		var client mangoClient
		if json.Unmarshal(trimmed, &client) != nil {
			return ipc.Event{}
		}
		return focusingClientEvent(&client, now)
	default:
		return ipc.Event{}
	}
}

func (s *watchSession) mapArray(line []byte, now int64) ipc.Event {
	var objects []map[string]json.RawMessage
	if err := json.Unmarshal(line, &objects); err != nil {
		return ipc.Event{}
	}
	if len(objects) == 0 {
		return clientsListEvent([]mangoClient{}, now)
	}
	first := objects[0]
	switch {
	case hasKey(first, "appid") || hasKey(first, "title"):
		var clients []mangoClient
		if json.Unmarshal(line, &clients) == nil {
			return clientsListEvent(clients, now)
		}
	case hasKey(first, "name") && (hasKey(first, "width") || hasKey(first, "active")):
		var monitors []mangoMonitor
		if json.Unmarshal(line, &monitors) == nil {
			return monitorsListEvent(monitors, now)
		}
	case hasKey(first, "index"):
		var tags []mangoTag
		if json.Unmarshal(line, &tags) == nil {
			return tagsListEvent([]mangoTagsForMonitor{{Tags: tags}}, now)
		}
	}
	return ipc.Event{}
}

func hasKey(m map[string]json.RawMessage, key string) bool {
	_, ok := m[key]
	return ok
}

func clientsListEvent(clients []mangoClient, now int64) ipc.Event {
	windows := make([]ipc.Window, 0, len(clients))
	for i := range clients {
		windows = append(windows, windowFromMango(&clients[i]))
	}
	return ipc.Event{
		Type:      mangoClientsSnapshot,
		Timestamp: now,
		Payload:   map[string]interface{}{"windows": windows},
	}
}

func monitorsListEvent(monitors []mangoMonitor, now int64) ipc.Event {
	names := make([]string, 0, len(monitors))
	active := ""
	for _, monitor := range monitors {
		names = append(names, monitor.Name)
		if monitor.Active.value() {
			active = monitor.Name
		}
	}
	return ipc.Event{
		Type:      ipc.EventMonitorChanged,
		Timestamp: now,
		Payload:   map[string]interface{}{"monitors": names, "active": active},
	}
}

func tagsListEvent(groups []mangoTagsForMonitor, now int64) ipc.Event {
	var tags []ipc.Workspace
	for _, group := range groups {
		for _, tag := range group.Tags {
			name := strconv.Itoa(tag.Index)
			tags = append(tags, ipc.Workspace{
				ID:        workspaceID(group.Monitor, tag.Index),
				Name:      name,
				MonitorID: group.Monitor,
				IsActive:  tag.Active.value(),
				IsEmpty:   tag.ClientCount == 0,
				Metadata: map[string]interface{}{
					"index":   tag.Index,
					"urgent":  tag.Urgent.value(),
					"focused": tag.Active.value(),
					"layout":  tag.Layout,
				},
			})
		}
	}
	return ipc.Event{
		Type:      ipc.EventWorkspaceChanged,
		Timestamp: now,
		Payload:   map[string]interface{}{"workspaces": tags},
	}
}

func focusingClientEvent(client *mangoClient, now int64) ipc.Event {
	if client.ID == 0 {
		return ipc.Event{Type: ipc.EventWorkspaceChanged, Timestamp: now}
	}
	window := windowFromMango(client)
	window.IsFocused = true
	return ipc.Event{
		Type:      ipc.EventWindowFocused,
		Timestamp: now,
		Window:    &window,
		Payload: map[string]interface{}{
			"id":      window.ID,
			"address": window.ID,
			"title":   client.Title,
			"appid":   client.AppID,
			"monitor": client.Monitor,
		},
	}
}

func snapshotEvents(previous map[string]ipc.Window, windows []ipc.Window, now int64) (map[string]ipc.Window, []ipc.Event) {
	current := make(map[string]ipc.Window, len(windows))
	for _, window := range windows {
		current[window.ID] = window
	}
	if previous == nil {
		return current, []ipc.Event{{Type: ipc.EventWorkspaceChanged, Timestamp: now}}
	}
	var events []ipc.Event
	for id, window := range current {
		old, exists := previous[id]
		if !exists {
			w := window
			events = append(events, ipc.Event{
				Type: ipc.EventWindowCreated, Timestamp: now, Window: &w,
				Payload: map[string]interface{}{"id": id},
			})
			continue
		}
		if !reflect.DeepEqual(old, window) {
			if old.Title != window.Title || old.AppID != window.AppID {
				w := window
				events = append(events, ipc.Event{
					Type: ipc.EventWindowTitleChanged, Timestamp: now, Window: &w,
					Payload: map[string]interface{}{"id": id, "title": window.Title},
				})
			}
			if old.AppID != window.AppID {
				events = append(events, ipc.Event{Type: ipc.EventWorkspaceChanged, Timestamp: now})
			}
			if old.WorkspaceID != window.WorkspaceID {
				events = append(events, ipc.Event{
					Type: ipc.EventWindowMoved, Timestamp: now,
					Payload: map[string]interface{}{"id": id, "workspace_id": window.WorkspaceID},
				})
			}
			if old.IsFullscreen != window.IsFullscreen {
				events = append(events, ipc.Event{
					Type: ipc.EventFullscreenChanged, Timestamp: now,
					Payload: map[string]interface{}{"id": id, "is_fullscreen": window.IsFullscreen},
				})
			}
			if old.IsFocused != window.IsFocused {
				events = append(events, ipc.Event{Type: ipc.EventWorkspaceChanged, Timestamp: now})
			}
			if !sameWindowAfterKnownChanges(old, window) {
				events = append(events, ipc.Event{Type: ipc.EventWorkspaceChanged, Timestamp: now})
			}
		}
	}
	for id := range previous {
		if _, exists := current[id]; !exists {
			events = append(events, ipc.Event{
				Type: ipc.EventWindowClosed, Timestamp: now,
				Payload: map[string]interface{}{"id": id},
			})
		}
	}
	return current, events
}

func sameWindowAfterKnownChanges(old, current ipc.Window) bool {
	old.Title = current.Title
	old.AppID = current.AppID
	old.WorkspaceID = current.WorkspaceID
	old.IsFocused = current.IsFocused
	old.IsFullscreen = current.IsFullscreen
	return reflect.DeepEqual(old, current)
}

func (m *Mango) forwardWatchEvents(ch chan ipc.Event, watch *watchSession) {
	defer func() {
		m.mu.Lock()
		if m.watch == watch {
			m.subscribed = false
			m.watch = nil
		}
		m.mu.Unlock()
		close(ch)
	}()
	var previous map[string]ipc.Window
	previousActiveMonitor := ""
	for event := range watch.Updates() {
		if event.Type == mangoClientsSnapshot {
			windows, _ := event.Payload["windows"].([]ipc.Window)
			var mapped []ipc.Event
			previous, mapped = snapshotEvents(previous, windows, event.Timestamp)
			for _, update := range mapped {
				select {
				case ch <- update:
				default:
				}
			}
			continue
		}
		if event.Type == ipc.EventMonitorChanged {
			active, _ := event.Payload["active"].(string)
			if previousActiveMonitor != "" && active != "" && active != previousActiveMonitor {
				select {
				case ch <- ipc.Event{
					Type: ipc.EventFocusedMonitorChanged, Timestamp: event.Timestamp,
					Payload: map[string]interface{}{"monitor": active},
				}:
				default:
				}
			}
			if active != "" {
				previousActiveMonitor = active
			}
		}
		if event.Type == "" {
			continue
		}
		select {
		case ch <- event:
		default:
		}
	}
}
