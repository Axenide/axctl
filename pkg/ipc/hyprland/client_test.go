package hyprland

import (
	"net"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"axctl/pkg/ipc"
)

func TestParseHyprlandVersion(t *testing.T) {
	tests := []struct {
		name string
		resp string
		want string
	}{
		{name: "json version", resp: `{"version":"0.55.0","tag":"v0.55.0"}`, want: "0.55.0"},
		{name: "json tag fallback", resp: `{"tag":"v0.54.2"}`, want: "v0.54.2"},
		{name: "text version", resp: "Hyprland 0.55.1 (git commit abc)", want: "0.55.1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseHyprlandVersion(tt.resp)
			if err != nil {
				t.Fatalf("parseHyprlandVersion() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("parseHyprlandVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsHyprlandVersionAtLeast055(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{version: "0.54.2", want: false},
		{version: "0.55.0", want: true},
		{version: "v0.55.1", want: true},
		{version: "1.0.0", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			got := isHyprlandVersionAtLeast(tt.version, 0, 55)
			if got != tt.want {
				t.Fatalf("isHyprlandVersionAtLeast(%q, 0, 55) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}

func TestDispatchVersionedRetriesAfterVersionParseFailure(t *testing.T) {
	commands := runFakeHyprlandSocket(t, []string{`{}`, `{"version":"0.55.0"}`})
	h := &Hyprland{signature: "test"}

	if err := h.Execute("kitty"); err != nil {
		t.Fatalf("Execute() first call error = %v", err)
	}
	if err := h.Execute("kitty"); err != nil {
		t.Fatalf("Execute() second call error = %v", err)
	}

	want := []string{
		"j/version",
		"dispatch exec kitty",
		"j/version",
		`dispatch hl.dsp.exec_cmd("kitty")`,
	}
	if got := commands(); !reflect.DeepEqual(got, want) {
		t.Fatalf("commands = %#v, want %#v", got, want)
	}
}

func TestHyprlandLegacyDispatcherCommands(t *testing.T) {
	tests := []struct {
		name string
		run  func(*Hyprland) error
		want string
	}{
		{name: "move window keeps shorthand direction", run: func(h *Hyprland) error { return h.MoveWindow("0x1", "l") }, want: "dispatch movewindow l,address:0x1"},
		{name: "switch workspace", run: func(h *Hyprland) error { return h.SwitchWorkspace("+1") }, want: "dispatch workspace +1"},
		{name: "toggle special workspace", run: func(h *Hyprland) error { return h.ToggleSpecialWorkspace("") }, want: "dispatch togglespecialworkspace"},
		{name: "execute", run: func(h *Hyprland) error { return h.Execute("kitty --class demo") }, want: "dispatch exec kitty --class demo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			commands := runFakeHyprlandSocket(t, []string{`{"version":"0.54.2"}`})
			h := &Hyprland{signature: "test"}

			if err := tt.run(h); err != nil {
				t.Fatalf("command error = %v", err)
			}

			want := []string{"j/version", tt.want}
			if got := commands(); !reflect.DeepEqual(got, want) {
				t.Fatalf("commands = %#v, want %#v", got, want)
			}
		})
	}
}

func TestHyprlandLuaDispatcherCommands(t *testing.T) {
	tests := []struct {
		name string
		run  func(*Hyprland) error
		want string
	}{
		{name: "focus direction normalizes shorthand", run: func(h *Hyprland) error { return h.FocusDir("l") }, want: `dispatch hl.dsp.focus({ direction = "left" })`},
		{name: "move window normalizes shorthand and targets window", run: func(h *Hyprland) error { return h.MoveWindow("0x1", "r") }, want: `dispatch hl.dsp.window.move({ direction = "right", window = "address:0x1" })`},
		{name: "resize window targets window", run: func(h *Hyprland) error { return h.ResizeWindow("0x1", 800, 600) }, want: `dispatch hl.dsp.window.resize({ x = 800, y = 600, window = "address:0x1" })`},
		{name: "switch workspace quotes relative target", run: func(h *Hyprland) error { return h.SwitchWorkspace("+1") }, want: `dispatch hl.dsp.focus({ workspace = "+1" })`},
		{name: "switch workspace quotes relative reverse target", run: func(h *Hyprland) error { return h.SwitchWorkspace("-1") }, want: `dispatch hl.dsp.focus({ workspace = "-1" })`},
		{name: "move workspace silent uses follow false", run: func(h *Hyprland) error { return h.MoveToWorkspaceSilent("0x1", "3") }, want: `dispatch hl.dsp.window.move({ workspace = "3", follow = false, window = "address:0x1" })`},
		{name: "toggle special workspace uses dispatcher object", run: func(h *Hyprland) error { return h.ToggleSpecialWorkspace("") }, want: "dispatch hl.dsp.workspace.toggle_special()"},
		{name: "toggle named special workspace quotes name", run: func(h *Hyprland) error { return h.ToggleSpecialWorkspace("magic") }, want: `dispatch hl.dsp.workspace.toggle_special("magic")`},
		{name: "dpms monitor", run: func(h *Hyprland) error { return h.SetDpms("DP-1", false) }, want: `dispatch hl.dsp.dpms({ action = "off", monitor = "DP-1" })`},
		{name: "close active window", run: func(h *Hyprland) error { return h.CloseWindow("") }, want: "dispatch hl.dsp.window.close()"},
		{name: "set maximized uses set action", run: func(h *Hyprland) error { return h.SetMaximized("", true) }, want: `dispatch hl.dsp.window.fullscreen({ mode = "maximized", action = "set" })`},
		{name: "unset maximized uses unset action", run: func(h *Hyprland) error { return h.SetMaximized("", false) }, want: `dispatch hl.dsp.window.fullscreen({ mode = "maximized", action = "unset" })`},
		{name: "group nav backwards", run: func(h *Hyprland) error { return h.GroupNav("l") }, want: "dispatch hl.dsp.group.prev()"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			commands := runFakeHyprlandSocket(t, []string{`{"version":"0.55.0"}`})
			h := &Hyprland{signature: "test"}

			if err := tt.run(h); err != nil {
				t.Fatalf("command error = %v", err)
			}

			want := []string{"j/version", tt.want}
			if got := commands(); !reflect.DeepEqual(got, want) {
				t.Fatalf("commands = %#v, want %#v", got, want)
			}
		})
	}
}

func TestListLayoutsParsesJson(t *testing.T) {
	responses := map[string]string{
		"j/layouts":                  `["dwindle","master","scrolling","monocle"]`,
		"j/getoption general:layout": `{"str":"master"}`,
	}
	getCommands := runFakeHyprlandSocketWith(t, nil, responses)
	h := &Hyprland{signature: "test"}
	layouts, err := h.ListLayouts()
	if err != nil {
		t.Fatalf("ListLayouts() error = %v", err)
	}
	want := []ipc.Layout{
		{Name: "dwindle", Source: ipc.LayoutSourceDynamic},
		{Name: "master", Current: true, Source: ipc.LayoutSourceDynamic},
		{Name: "scrolling", Source: ipc.LayoutSourceDynamic},
		{Name: "monocle", Source: ipc.LayoutSourceDynamic},
	}
	if !reflect.DeepEqual(layouts, want) {
		t.Fatalf("layouts = %+v, want %+v", layouts, want)
	}
	cmds := getCommands()
	if len(cmds) < 2 || cmds[0] != "j/layouts" {
		t.Fatalf("commands = %+v, want j/layouts first", cmds)
	}
}

func TestListLayoutsParsesTextFallback(t *testing.T) {
	responses := map[string]string{
		"j/layouts":                  "unknown request",
		"j/getoption general:layout": `{"str":"dwindle"}`,
		"layouts":                    "dwindle\nmaster\nscrolling\nmonocle\n",
	}
	runFakeHyprlandSocketWith(t, nil, responses)
	h := &Hyprland{signature: "test"}
	layouts, err := h.ListLayouts()
	if err != nil {
		t.Fatalf("ListLayouts() error = %v", err)
	}
	if len(layouts) != 4 {
		t.Fatalf("layouts = %+v, want 4", layouts)
	}
	if layouts[0].Name != "dwindle" || !layouts[0].Current {
		t.Fatalf("layouts[0] = %+v, want dwindle current", layouts[0])
	}
}

func TestListLayoutsStaticFallbackWhenHyprctlUnknown(t *testing.T) {
	runFakeHyprlandSocketWith(t, nil, map[string]string{
		"j/layouts":                  "unknown request",
		"j/getoption general:layout": `{"str":"master"}`,
		"layouts":                    "unknown request",
	})
	t.Setenv("PATH", t.TempDir())
	h := &Hyprland{signature: "test"}
	layouts, err := h.ListLayouts()
	if err != nil {
		t.Fatalf("ListLayouts() error = %v", err)
	}
	if len(layouts) != 4 {
		t.Fatalf("layouts = %+v, want 4 built-in", layouts)
	}
	if layouts[1].Name != "master" || !layouts[1].Current {
		t.Fatalf("layouts[1] = %+v, want master current", layouts[1])
	}
	for _, l := range layouts {
		if l.Source != ipc.LayoutSourceStatic {
			t.Fatalf("layout %+v, want static source", l)
		}
	}
}

func TestParseHyprctlLayoutsList(t *testing.T) {
	resp := "dwindle some description\nmaster extra info\nscrolling\n"
	got := parseHyprctlLayoutsList(resp)
	want := []ipc.Layout{{Name: "dwindle"}, {Name: "master"}, {Name: "scrolling"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseHyprctlLayoutsList = %+v, want %+v", got, want)
	}
}

func runFakeHyprlandSocket(t *testing.T, versions []string) func() []string {
	return runFakeHyprlandSocketWith(t, versions, map[string]string{})
}

func runFakeHyprlandSocketWith(t *testing.T, versions []string, responses map[string]string) func() []string {
	t.Helper()

	runtimeDir := t.TempDir()
	socketDir := filepath.Join(runtimeDir, "hypr", "test")
	if err := os.MkdirAll(socketDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)

	listener, err := net.Listen("unix", filepath.Join(socketDir, ".socket.sock"))
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	var mu sync.Mutex
	var commands []string
	done := make(chan struct{})

	go func() {
		defer close(done)
		versionIndex := 0
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			func() {
				defer conn.Close()
				buf := make([]byte, 4096)
				n, err := conn.Read(buf)
				if err != nil {
					return
				}
				cmd := string(buf[:n])
				mu.Lock()
				commands = append(commands, cmd)
				mu.Unlock()
				if cmd == "j/version" {
					version := versions[len(versions)-1]
					if versionIndex < len(versions) {
						version = versions[versionIndex]
					}
					versionIndex++
					if version == "" {
						version = "{\"version\":\"0.55.0\"}"
					}
					_, _ = conn.Write([]byte(version))
					return
				}
				if resp, ok := responses[cmd]; ok {
					_, _ = conn.Write([]byte(resp))
					return
				}
				_, _ = conn.Write([]byte("ok"))
			}()
		}
	}()

	t.Cleanup(func() {
		_ = listener.Close()
		<-done
	})

	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), commands...)
	}
}
