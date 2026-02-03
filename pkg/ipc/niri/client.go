package niri

import (
	"encoding/json"
	"fmt"
	"net"
	"os"

	"axctl/pkg/ipc"
)

type Niri struct {
	socketPath string
}

func New() (*Niri, error) {
	path := os.Getenv("NIRI_SOCKET")
	if path == "" {
		return nil, fmt.Errorf("NIRI_SOCKET not set")
	}
	return &Niri{socketPath: path}, nil
}

func (n *Niri) request(req interface{}, resp interface{}) error {
	conn, err := net.Dial("unix", n.socketPath)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return err
	}

	var reply struct {
		Reply struct {
			Ok  json.RawMessage `json:"Ok"`
			Err json.RawMessage `json:"Err"`
		} `json:"Reply"`
	}

	if err := json.NewDecoder(conn).Decode(&reply); err != nil {
		return err
	}

	if len(reply.Reply.Err) > 0 {
		return fmt.Errorf("niri error: %s", string(reply.Reply.Err))
	}

	if resp != nil {
		return json.Unmarshal(reply.Reply.Ok, resp)
	}

	return nil
}

func (n *Niri) ListWindows() ([]ipc.Window, error) {
	var niriWindows []struct {
		ID          int     `json:"id"`
		Title       *string `json:"title"`
		AppID       *string `json:"app_id"`
		WorkspaceID *int    `json:"workspace_id"`
	}

	err := n.request("Windows", &niriWindows)
	if err != nil {
		return nil, err
	}

	windows := make([]ipc.Window, len(niriWindows))
	for i, w := range niriWindows {
		title := ""
		if w.Title != nil {
			title = *w.Title
		}
		class := ""
		if w.AppID != nil {
			class = *w.AppID
		}
		wsID := ""
		if w.WorkspaceID != nil {
			wsID = fmt.Sprintf("%d", *w.WorkspaceID)
		}

		windows[i] = ipc.Window{
			ID:          fmt.Sprintf("%d", w.ID),
			Title:       title,
			Class:       class,
			WorkspaceID: wsID,
		}
	}
	return windows, nil
}

func (n *Niri) FocusWindow(id string) error {
	var idInt int
	if _, err := fmt.Sscanf(id, "%d", &idInt); err != nil {
		return err
	}
	return n.request(map[string]interface{}{
		"Action": map[string]interface{}{
			"FocusWindow": map[string]interface{}{
				"id": idInt,
			},
		},
	}, nil)
}

func (n *Niri) CloseWindow(id string) error {
	var idInt int
	if _, err := fmt.Sscanf(id, "%d", &idInt); err != nil {
		return err
	}
	return n.request(map[string]interface{}{
		"Action": map[string]interface{}{
			"CloseWindow": map[string]interface{}{
				"id": idInt,
			},
		},
	}, nil)
}

func (n *Niri) ListWorkspaces() ([]ipc.Workspace, error) {
	var niriWorkspaces []struct {
		ID     int     `json:"id"`
		Name   *string `json:"name"`
		Output *string `json:"output"`
	}

	err := n.request("Workspaces", &niriWorkspaces)
	if err != nil {
		return nil, err
	}

	res := make([]ipc.Workspace, len(niriWorkspaces))
	for i, w := range niriWorkspaces {
		name := ""
		if w.Name != nil {
			name = *w.Name
		}
		output := ""
		if w.Output != nil {
			output = *w.Output
		}
		res[i] = ipc.Workspace{
			ID:        fmt.Sprintf("%d", w.ID),
			Name:      name,
			MonitorID: output,
		}
	}
	return res, nil
}

func (n *Niri) SwitchWorkspace(id string) error {
	var idInt int
	if _, err := fmt.Sscanf(id, "%d", &idInt); err != nil {
		return err
	}
	return n.request(map[string]interface{}{
		"Action": map[string]interface{}{
			"FocusWorkspace": map[string]interface{}{
				"reference": map[string]interface{}{
					"Index": idInt,
				},
			},
		},
	}, nil)
}

func (n *Niri) Subscribe() (<-chan ipc.Event, error) {
	conn, err := net.Dial("unix", n.socketPath)
	if err != nil {
		return nil, err
	}

	if err := json.NewEncoder(conn).Encode("EventStream"); err != nil {
		conn.Close()
		return nil, err
	}

	ch := make(chan ipc.Event)
	go func() {
		defer conn.Close()
		defer close(ch)
		dec := json.NewDecoder(conn)
		for {
			var eventWrapper struct {
				Event json.RawMessage `json:"Event"`
			}
			if err := dec.Decode(&eventWrapper); err != nil {
				break
			}

			ch <- ipc.Event{
				Type: ipc.EventWorkspaceChanged,
			}
		}
	}()

	return ch, nil
}
