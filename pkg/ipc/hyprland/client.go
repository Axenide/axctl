package hyprland

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"axctl/pkg/ipc"
)

type Hyprland struct {
	signature string
}

func New() (*Hyprland, error) {
	sig := os.Getenv("HYPRLAND_INSTANCE_SIGNATURE")
	if sig == "" {
		return nil, fmt.Errorf("HYPRLAND_INSTANCE_SIGNATURE not set")
	}
	return &Hyprland{signature: sig}, nil
}

func (h *Hyprland) getSocketPath(socketName string) string {
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	return fmt.Sprintf("%s/hypr/%s/%s", runtimeDir, h.signature, socketName)
}

func (h *Hyprland) dispatch(cmd string) (string, error) {
	conn, err := net.Dial("unix", h.getSocketPath(".socket.sock"))
	if err != nil {
		return "", err
	}
	defer conn.Close()

	_, err = conn.Write([]byte(cmd))
	if err != nil {
		return "", err
	}

	response, err := io.ReadAll(conn)
	if err != nil {
		return "", err
	}
	return string(response), nil
}

func (h *Hyprland) ListWindows() ([]ipc.Window, error) {
	resp, err := h.dispatch("j/clients")
	if err != nil {
		return nil, err
	}

	var clients []struct {
		Address   string `json:"address"`
		Title     string `json:"title"`
		Class     string `json:"class"`
		Workspace struct {
			ID int `json:"id"`
		} `json:"workspace"`
	}

	if err := json.Unmarshal([]byte(resp), &clients); err != nil {
		return nil, err
	}

	windows := make([]ipc.Window, len(clients))
	for i, c := range clients {
		windows[i] = ipc.Window{
			ID:          c.Address,
			Title:       c.Title,
			Class:       c.Class,
			WorkspaceID: fmt.Sprintf("%d", c.Workspace.ID),
		}
	}
	return windows, nil
}

func (h *Hyprland) FocusWindow(id string) error {
	_, err := h.dispatch(fmt.Sprintf("dispatch focuswindow address:%s", id))
	return err
}

func (h *Hyprland) CloseWindow(id string) error {
	_, err := h.dispatch(fmt.Sprintf("dispatch closewindow address:%s", id))
	return err
}

func (h *Hyprland) ListWorkspaces() ([]ipc.Workspace, error) {
	resp, err := h.dispatch("j/workspaces")
	if err != nil {
		return nil, err
	}

	var workspaces []struct {
		ID      int    `json:"id"`
		Name    string `json:"name"`
		Monitor string `json:"monitor"`
	}

	if err := json.Unmarshal([]byte(resp), &workspaces); err != nil {
		return nil, err
	}

	res := make([]ipc.Workspace, len(workspaces))
	for i, w := range workspaces {
		res[i] = ipc.Workspace{
			ID:        fmt.Sprintf("%d", w.ID),
			Name:      w.Name,
			MonitorID: w.Monitor,
		}
	}
	return res, nil
}

func (h *Hyprland) SwitchWorkspace(id string) error {
	_, err := h.dispatch(fmt.Sprintf("dispatch workspace %s", id))
	return err
}

func (h *Hyprland) Subscribe() (<-chan ipc.Event, error) {
	conn, err := net.Dial("unix", h.getSocketPath(".socket2.sock"))
	if err != nil {
		return nil, err
	}

	ch := make(chan ipc.Event)
	go func() {
		defer conn.Close()
		defer close(ch)
		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			line := scanner.Text()
			parts := strings.SplitN(line, ">>", 2)
			if len(parts) < 2 {
				continue
			}

			event := ipc.Event{
				Timestamp: time.Now().Unix(),
				Payload:   make(map[string]interface{}),
			}

			switch parts[0] {
			case "openwindow":
				event.Type = ipc.EventWindowCreated
				// address,workspace,class,title
				data := strings.SplitN(parts[1], ",", 4)
				if len(data) >= 4 {
					event.Window = &ipc.Window{
						ID:          "0x" + data[0],
						WorkspaceID: data[1],
						Class:       data[2],
						Title:       data[3],
					}
				}
			case "closewindow":
				event.Type = ipc.EventWindowClosed
				event.Payload["address"] = "0x" + parts[1]
			case "activewindow":
				event.Type = ipc.EventWindowFocused
				// class,title
				data := strings.SplitN(parts[1], ",", 2)
				if len(data) >= 2 {
					event.Payload["class"] = data[0]
					event.Payload["title"] = data[1]
				}
			case "workspace":
				event.Type = ipc.EventWorkspaceChanged
				event.Payload["name"] = parts[1]
			}

			if event.Type != "" {
				ch <- event
			}
		}
	}()

	return ch, nil
}
