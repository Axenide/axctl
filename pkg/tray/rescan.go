package tray

import (
	"context"
	"encoding/xml"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	rescanTimeout        = 12 * time.Second
	rescanCallTimeout    = 500 * time.Millisecond
	rescanMaxDepth       = 8
	rescanMaxObjectPaths = 32
	rescanWorkerLimit    = 12
)

type introspectionNode struct {
	Interfaces []struct {
		Name string `xml:"name,attr"`
	} `xml:"interface"`
	Nodes []struct {
		Name string `xml:"name,attr"`
	} `xml:"node"`
}

type scanNode struct {
	Path  dbus.ObjectPath
	Depth int
}

func (s *Service) Rescan() (int, error) {
	s.mu.RLock()
	conn := s.conn
	watcherOwner := s.watcherOwner
	closed := s.closed
	s.mu.RUnlock()
	if conn == nil || closed {
		return 0, fmt.Errorf("tray watcher is not connected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), rescanTimeout)
	defer cancel()

	registered := 0
	if watcherOwner != "" {
		registered += s.syncWatcherItems(watcherOwner)
	}

	callCtx, callCancel := context.WithTimeout(ctx, 2*time.Second)
	call := conn.BusObject().CallWithContext(callCtx, "org.freedesktop.DBus.ListNames", 0)
	callCancel()
	if call.Err != nil {
		return registered, call.Err
	}
	var names []string
	if err := call.Store(&names); err != nil {
		return registered, err
	}
	self := string(conn.Names()[0])
	owners := make([]string, 0, len(names))
	for _, name := range names {
		if strings.HasPrefix(name, ":") && name != self {
			owners = append(owners, name)
		}
	}
	sort.Strings(owners)
	if len(owners) == 0 {
		return registered, nil
	}

	workerCount := min(rescanWorkerLimit, len(owners))
	jobs := make(chan string)
	results := make(chan []ItemRef, workerCount)
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			for owner := range jobs {
				refs := s.scanServiceItems(ctx, conn, owner)
				select {
				case results <- refs:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, owner := range owners {
			select {
			case jobs <- owner:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		workers.Wait()
		close(results)
	}()
	for refs := range results {
		for _, ref := range refs {
			if s.trackResolvedItem(ref) {
				registered++
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return registered, err
	}
	return registered, nil
}

func (s *Service) rescanInBackground() {
	if _, err := s.Rescan(); err != nil {
		fmt.Printf("[tray] rescan items: %v\n", err)
	}
}

func (s *Service) scanServiceItems(ctx context.Context, conn *dbus.Conn, owner string) []ItemRef {
	paths := make(map[dbus.ObjectPath]struct{})
	defaultPath := dbus.ObjectPath("/StatusNotifierItem")
	if hasStatusNotifierProperties(ctx, conn, owner, defaultPath) {
		paths[defaultPath] = struct{}{}
	}
	queue := []scanNode{{Path: dbus.ObjectPath("/"), Depth: 0}}
	visited := make(map[dbus.ObjectPath]struct{})
	for len(queue) > 0 && len(visited) < rescanMaxObjectPaths && ctx.Err() == nil {
		current := queue[0]
		queue = queue[1:]
		if _, exists := visited[current.Path]; exists {
			continue
		}
		visited[current.Path] = struct{}{}

		callCtx, cancel := context.WithTimeout(ctx, rescanCallTimeout)
		call := conn.Object(owner, current.Path).CallWithContext(callCtx, "org.freedesktop.DBus.Introspectable.Introspect", dbus.FlagNoAutoStart)
		cancel()
		if call.Err != nil {
			continue
		}
		var document string
		if err := call.Store(&document); err != nil {
			continue
		}
		var node introspectionNode
		if err := xml.Unmarshal([]byte(document), &node); err != nil {
			continue
		}
		for _, iface := range node.Interfaces {
			if iface.Name == itemInterface && hasStatusNotifierProperties(ctx, conn, owner, current.Path) {
				paths[current.Path] = struct{}{}
				break
			}
		}
		if current.Depth >= rescanMaxDepth {
			continue
		}
		for _, child := range node.Nodes {
			if child.Name == "" {
				continue
			}
			path := childObjectPath(current.Path, child.Name)
			if path.IsValid() {
				queue = append(queue, scanNode{Path: path, Depth: current.Depth + 1})
			}
		}
	}

	refs := make([]ItemRef, 0, len(paths))
	for path := range paths {
		refs = append(refs, ItemRef{Service: owner, Owner: owner, Path: path})
	}
	return refs
}

func hasStatusNotifierProperties(ctx context.Context, conn *dbus.Conn, owner string, path dbus.ObjectPath) bool {
	callCtx, cancel := context.WithTimeout(ctx, rescanCallTimeout)
	defer cancel()
	var values map[string]dbus.Variant
	call := conn.Object(owner, path).CallWithContext(callCtx, "org.freedesktop.DBus.Properties.GetAll", dbus.FlagNoAutoStart, itemInterface)
	if call.Err != nil || call.Store(&values) != nil {
		return false
	}
	_, hasCategory := values["Category"]
	_, hasStatus := values["Status"]
	return hasCategory && hasStatus
}

func childObjectPath(parent dbus.ObjectPath, child string) dbus.ObjectPath {
	if strings.HasPrefix(child, "/") {
		return dbus.ObjectPath(child)
	}
	if parent == "/" {
		return dbus.ObjectPath("/" + child)
	}
	return dbus.ObjectPath(string(parent) + "/" + child)
}
