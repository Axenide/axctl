package mangowc

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"axctl/pkg/ipc"
)

type Mangowc struct {
	socketPath string
	mu         sync.Mutex
	conn       net.Conn
}

func New() (*Mangowc, error) {
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = fmt.Sprintf("/run/user/%d", os.Getuid())
	}
	path := fmt.Sprintf("%s/mangowc.sock", runtimeDir)
	return &Mangowc{socketPath: path}, nil
}

func (m *Mangowc) command(cmd string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	conn, err := net.Dial("unix", m.socketPath)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	if _, err = conn.Write([]byte(cmd + "\n")); err != nil {
		return "", err
	}

	var sb strings.Builder
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		sb.WriteString(scanner.Text())
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

func (m *Mangowc) ListWindows() ([]ipc.Window, error) {
	resp, err := m.command("getwindows")
	if err != nil {
		return nil, err
	}

	lines := strings.Split(resp, "\n")
	var windows []ipc.Window
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 4 {
			windows = append(windows, ipc.Window{
				ID:          parts[0],
				Title:       parts[1],
				Class:       parts[2],
				WorkspaceID: parts[3],
			})
		}
	}
	return windows, nil
}

func (m *Mangowc) ActiveWindow() (string, error) {
	resp, err := m.command("getwindows")
	if err != nil {
		return "", err
	}
	lines := strings.Split(resp, "\n")
	for _, line := range lines {
		if strings.Contains(line, "*") {
			parts := strings.Fields(line)
			if len(parts) > 0 {
				return parts[0], nil
			}
		}
	}
	return "", nil
}

func (m *Mangowc) FocusWindow(id string) error {
	_, err := m.command(fmt.Sprintf("dispatch focuswindow %s", id))
	return err
}

func (m *Mangowc) FocusDir(direction string) error {
	_, err := m.command(fmt.Sprintf("dispatch focusdir %s", direction))
	return err
}

func (m *Mangowc) CloseWindow(id string) error {
	_, err := m.command(fmt.Sprintf("dispatch killclient %s", id))
	return err
}

func (m *Mangowc) MoveWindow(id string, direction string) error {
	_, err := m.command(fmt.Sprintf("dispatch movewin %s", direction))
	return err
}

func (m *Mangowc) ResizeWindow(id string, width, height int) error {
	_, err := m.command(fmt.Sprintf("dispatch resizewin %d,%d", width, height))
	return err
}

func (m *Mangowc) ToggleFloating(id string) error {
	_, err := m.command(fmt.Sprintf("dispatch togglefloating %s", id))
	return err
}

func (m *Mangowc) SetFullscreen(id string, state bool) error {
	_, err := m.command(fmt.Sprintf("dispatch togglefullscreen %s", id))
	return err
}

func (m *Mangowc) SetMaximized(id string, state bool) error {
	return ipc.ErrNotSupported
}

func (m *Mangowc) PinWindow(id string, state bool) error {
	return ipc.ErrNotSupported
}

func (m *Mangowc) ToggleGroup(id string) error {
	return ipc.ErrNotSupported
}

func (m *Mangowc) GroupNav(direction string) error {
	return ipc.ErrNotSupported
}

func (m *Mangowc) SetLayoutProperty(id string, key, value string) error {
	if key == "tag" {
		_, err := m.command(fmt.Sprintf("dispatch tag %s", value))
		return err
	}
	return ipc.ErrNotSupported
}

func (m *Mangowc) ListWorkspaces() ([]ipc.Workspace, error) {
	resp, err := m.command("getworkspaces")
	if err != nil {
		return nil, err
	}

	lines := strings.Split(resp, "\n")
	var workspaces []ipc.Workspace
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 3 {
			workspaces = append(workspaces, ipc.Workspace{
				ID:        parts[0],
				Name:      parts[1],
				MonitorID: parts[2],
			})
		}
	}
	return workspaces, nil
}

func (m *Mangowc) SwitchWorkspace(id string) error {
	_, err := m.command(fmt.Sprintf("dispatch view %s", id))
	return err
}

func (m *Mangowc) MoveToWorkspace(windowID, workspaceID string) error {
	_, err := m.command(fmt.Sprintf("dispatch tag %s", workspaceID))
	return err
}

func (m *Mangowc) ListMonitors() ([]ipc.Monitor, error) {
	return nil, ipc.ErrNotSupported
}

func (m *Mangowc) FocusMonitor(id string) error {
	_, err := m.command(fmt.Sprintf("dispatch focusmon %s", id))
	return err
}

func (m *Mangowc) MoveToMonitor(windowID, monitorID string) error {
	_, err := m.command(fmt.Sprintf("dispatch tagmon %s", monitorID))
	return err
}

func (m *Mangowc) SetLayout(name string) error {
	_, err := m.command(fmt.Sprintf("dispatch switch_layout %s", name))
	return err
}

func (m *Mangowc) SetConfig(key string, value interface{}) error {
	switch key {
	case "gaps.inner":
		m.command(fmt.Sprintf("setoption gappih %v", value))
		m.command(fmt.Sprintf("setoption gappiv %v", value))
	case "gaps.outer":
		m.command(fmt.Sprintf("setoption gappoh %v", value))
		m.command(fmt.Sprintf("setoption gappov %v", value))
	case "border.width":
		m.command(fmt.Sprintf("setoption borderpx %v", value))
	case "opacity.active":
		m.command(fmt.Sprintf("setoption focused_opacity %v", value))
	case "opacity.inactive":
		m.command(fmt.Sprintf("setoption unfocused_opacity %v", value))
	default:
		return ipc.ErrNotSupported
	}
	return nil
}

func (m *Mangowc) ReloadConfig() error {
	_, err := m.command("dispatch reload_config")
	return err
}

func (m *Mangowc) Execute(command string) error {
	_, err := m.command(fmt.Sprintf("dispatch spawn %s", command))
	return err
}

func (m *Mangowc) Exit() error {
	_, err := m.command("dispatch quit")
	return err
}

func (m *Mangowc) Subscribe() (<-chan ipc.Event, error) {
	conn, err := net.Dial("unix", m.socketPath)
	if err != nil {
		return nil, err
	}

	if _, err = conn.Write([]byte("watch\n")); err != nil {
		conn.Close()
		return nil, err
	}

	ch := make(chan ipc.Event)
	go func() {
		defer conn.Close()
		defer close(ch)
		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}

			ch <- ipc.Event{
				Type:      ipc.EventWorkspaceChanged,
				Timestamp: time.Now().Unix(),
			}
		}
	}()

	return ch, nil
}
