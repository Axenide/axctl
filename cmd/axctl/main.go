package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"axctl/pkg/ipc"
	"axctl/pkg/ipc/hyprland"
	"axctl/pkg/ipc/mangowc"
	"axctl/pkg/ipc/niri"
	"axctl/pkg/server"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		return
	}

	switch os.Args[1] {
	case "daemon":
		runDaemon()
	case "window", "workspace", "monitor", "layout", "config", "system":
		if len(os.Args) < 3 {
			usage()
			return
		}
		handleRPC(os.Args[1], os.Args[2:])
	default:
		usage()
	}
}

func usage() {
	fmt.Println("Usage: axctl <command> <action> [args]")
	fmt.Println("\nCommands:")
	fmt.Println("  daemon                    Start the IPC daemon")
	fmt.Println("\n  window <action> [args]")
	fmt.Println("    list                    List all windows")
	fmt.Println("    active                  Get active window ID")
	fmt.Println("    focus <id>              Focus a window")
	fmt.Println("    focus-dir <l|r|u|d>     Focus in direction")
	fmt.Println("    close [id]              Close a window")
	fmt.Println("    move <dir> [id]         Move window")
	fmt.Println("    resize <w> <h> [id]     Resize window")
	fmt.Println("    toggle-floating [id]    Toggle floating")
	fmt.Println("    fullscreen <0|1> [id]   Set fullscreen")
	fmt.Println("    maximize <0|1> [id]     Set maximized")
	fmt.Println("    pin <0|1> [id]          Pin window")
	fmt.Println("    toggle-group [id]       Toggle window group (Hyprland)")
	fmt.Println("    group-nav <f|b>         Navigate group tabs")
	fmt.Println("    layout-prop <k> <v> [id] Set layout property (Niri/Mango)")
	fmt.Println("    move-pixel <x> <y> [id] Move window exactly by pixel")
	fmt.Println("    move-to-workspace-silent <ws> [id] Move window silently")
	fmt.Println("\n  workspace <action> [args]")
	fmt.Println("    list                    List all workspaces")
	fmt.Println("    active                  Get active workspace")
	fmt.Println("    switch <id>             Switch workspace")
	fmt.Println("    move-to <ws_id> [win_id] Move window to workspace")
	fmt.Println("    toggle-special [name]   Toggle special workspace")
	fmt.Println("\n  monitor <action> [args]")
	fmt.Println("    list                    List all monitors")
	fmt.Println("    focus <id>              Focus monitor")
	fmt.Println("    move-to <mon_id> [win_id] Move window to monitor")
	fmt.Println("    set-dpms <mon_id> <0|1> Set DPMS on/off")
	fmt.Println("\n  layout <action> [args]")
	fmt.Println("    set <name>              Set layout")
	fmt.Println("\n  config <action> [args]")
	fmt.Println("    get <key>               Get config value")
	fmt.Println("    set <key> <value>       Set config key")
	fmt.Println("                            Keys: gaps.inner, gaps.outer, border.width,")
	fmt.Println("                                  border.active_color, border.inactive_color,")
	fmt.Println("                                  opacity.active, opacity.inactive,")
	fmt.Println("                                  blur.enabled, blur.size, blur.passes")
	fmt.Println("    batch <json_string>     Batch apply configs")
	fmt.Println("    get-animations          Get animation configs")
	fmt.Println("    bind-key <mods> <key> <cmd> Bind a key")
	fmt.Println("    unbind-key <mods> <key> Unbind a key")
	fmt.Println("    reload                  Reload config")
	fmt.Println("\n  system <action> [args]")
	fmt.Println("    execute <cmd>           Execute command")
	fmt.Println("    get-cursor-position     Get absolute cursor position")
	fmt.Println("    switch-keyboard-layout [next|prev] Switch keyboard layout")
	fmt.Println("    set-keyboard-layouts <layouts> [variants] Set keyboard layouts (e.g. \"us,es\" \"altgr-intl,\")")
	fmt.Println("    exit                    Exit compositor")
}

func socketExists(path string) bool {
	if stat, err := os.Stat(path); err == nil && !stat.IsDir() {
		return true
	}
	return false
}

func findLatestSocket(pattern string) string {
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return ""
	}

	// Filter out lock files
	var filtered []string
	for _, m := range matches {
		if !strings.HasSuffix(m, ".lock") {
			filtered = append(filtered, m)
		}
	}
	matches = filtered
	if len(matches) == 0 {
		return ""
	}

	if len(matches) == 1 {
		return matches[0]
	}

	var latest string
	var latestTime int64
	for _, m := range matches {
		if fi, err := os.Stat(m); err == nil {
			if fi.ModTime().UnixNano() > latestTime {
				latestTime = fi.ModTime().UnixNano()
				latest = m
			}
		}
	}
	if latest == "" {
		return matches[0]
	}
	return latest
}

func runDaemon() {
	var comp ipc.Compositor
	var err error

	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = fmt.Sprintf("/run/user/%d", os.Getuid())
		os.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	}

	// Validate existing WAYLAND_DISPLAY
	if wd := os.Getenv("WAYLAND_DISPLAY"); wd != "" {
		if !socketExists(filepath.Join(runtimeDir, wd)) {
			os.Unsetenv("WAYLAND_DISPLAY")
		}
	}

	// Guess WAYLAND_DISPLAY if missing or invalid (useful for tmux)
	if os.Getenv("WAYLAND_DISPLAY") == "" {
		// Try to find all wayland sockets and use the one that we can actually connect to,
		// or just pick the latest one that exists.
		wlSock := findLatestSocket(filepath.Join(runtimeDir, "wayland-[0-9]*"))
		if wlSock != "" && !strings.HasSuffix(wlSock, ".lock") {
			os.Setenv("WAYLAND_DISPLAY", filepath.Base(wlSock))
		}
	}

	// 1. Try Niri
	niriSock := os.Getenv("NIRI_SOCKET")
	if niriSock != "" && !socketExists(niriSock) {
		niriSock = ""
	}
	if niriSock == "" {
		niriSock = findLatestSocket(filepath.Join(runtimeDir, "niri-*.sock"))
		if niriSock != "" {
			os.Setenv("NIRI_SOCKET", niriSock)
		}
	}
	if niriSock != "" {
		if _, statErr := os.Stat(niriSock); statErr == nil {
			comp, err = niri.New()
		}
	}

	// 2. Try Hyprland
	if comp == nil && err == nil {
		hyprSig := os.Getenv("HYPRLAND_INSTANCE_SIGNATURE")
		if hyprSig != "" {
			if !socketExists(filepath.Join(runtimeDir, "hypr", hyprSig, ".socket.sock")) && !socketExists(filepath.Join("/tmp/hypr", hyprSig, ".socket.sock")) {
				hyprSig = ""
			}
		}
		if hyprSig == "" {
			sock := findLatestSocket(filepath.Join(runtimeDir, "hypr", "*", ".socket.sock"))
			if sock == "" {
				sock = findLatestSocket(filepath.Join("/tmp/hypr", "*", ".socket.sock"))
			}
			if sock != "" {
				hyprSig = filepath.Base(filepath.Dir(sock))
				os.Setenv("HYPRLAND_INSTANCE_SIGNATURE", hyprSig)
			}
		}
		if hyprSig != "" {
			socketPath := filepath.Join(runtimeDir, "hypr", hyprSig, ".socket.sock")
			if _, statErr := os.Stat(socketPath); statErr != nil {
				socketPath = filepath.Join("/tmp/hypr", hyprSig, ".socket.sock")
			}
			if _, statErr := os.Stat(socketPath); statErr == nil {
				comp, err = hyprland.New()
			}
		}
	}

	// 3. Try MangoWC fallback
	if comp == nil && err == nil {
		// First try the current WAYLAND_DISPLAY
		c, e := mangowc.New()
		if e == nil {
			comp = c
			err = nil
		} else {
			// If it failed, try other wayland sockets
			matches, _ := filepath.Glob(filepath.Join(runtimeDir, "wayland-[0-9]*"))
			var filtered []string
			for _, m := range matches {
				if !strings.HasSuffix(m, ".lock") {
					filtered = append(filtered, m)
				}
			}

			for _, wlSock := range filtered {
				if filepath.Base(wlSock) == os.Getenv("WAYLAND_DISPLAY") {
					continue // Already tried
				}
				os.Setenv("WAYLAND_DISPLAY", filepath.Base(wlSock))
				c, e = mangowc.New()
				if e == nil {
					comp = c
					err = nil
					break
				}
			}

			if comp == nil {
				fmt.Printf("Debug - MangoWC detection failed on all sockets.\n")
			}
		}
	}

	if comp == nil {
		if err != nil {
			fmt.Printf("Error initializing compositor: %v\n", err)
		} else {
			fmt.Println("Error: no supported compositor detected")
		}
		os.Exit(1)
	}

	fmt.Printf("Detected compositor: %T\n", comp)

	socketPath := fmt.Sprintf("/tmp/axctl-%d.sock", os.Getuid())
	srv := server.New(comp, socketPath)

	fmt.Printf("Starting axctl daemon on %s\n", socketPath)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := srv.Start(); err != nil {
			fmt.Printf("Server error: %v\n", err)
			os.Exit(1)
		}
	}()

	<-sig
	os.Remove(socketPath)
}

func handleRPC(category string, args []string) {
	action := args[0]
	method := fmt.Sprintf("%s.%s", capitalize(category), capitalize(action))

	params := make(map[string]interface{})

	switch method {
	case "Window.Focus":
		if len(args) > 1 {
			params["id"] = args[1]
		}
	case "Window.FocusDir":
		if len(args) > 1 {
			params["direction"] = args[1]
		}
	case "Window.Close":
		if len(args) > 1 {
			params["id"] = args[1]
		}
	case "Window.Move":
		if len(args) > 1 {
			params["direction"] = args[1]
		}
		if len(args) > 2 {
			params["id"] = args[2]
		}
	case "Window.Resize":
		if len(args) > 2 {
			var w, h int
			fmt.Sscanf(args[1], "%d", &w)
			fmt.Sscanf(args[2], "%d", &h)
			params["width"] = w
			params["height"] = h
		}
		if len(args) > 3 {
			params["id"] = args[3]
		}
	case "Window.ToggleFloating":
		if len(args) > 1 {
			params["id"] = args[1]
		}
	case "Window.Fullscreen":
		if len(args) > 1 {
			params["state"] = args[1] == "1"
		}
		if len(args) > 2 {
			params["id"] = args[2]
		}
	case "Window.Maximize":
		if len(args) > 1 {
			params["state"] = args[1] == "1"
		}
		if len(args) > 2 {
			params["id"] = args[2]
		}
	case "Window.Pin":
		if len(args) > 1 {
			params["state"] = args[1] == "1"
		}
		if len(args) > 2 {
			params["id"] = args[2]
		}
	case "Window.ToggleGroup":
		if len(args) > 1 {
			params["id"] = args[1]
		}
	case "Window.GroupNav":
		if len(args) > 1 {
			params["direction"] = args[1]
		}
	case "Window.LayoutProp":
		if len(args) > 2 {
			params["key"] = args[1]
			params["value"] = args[2]
		}
		if len(args) > 3 {
			params["id"] = args[3]
		}
	case "Window.MovePixel":
		if len(args) > 2 {
			var x, y int
			fmt.Sscanf(args[1], "%d", &x)
			fmt.Sscanf(args[2], "%d", &y)
			params["x"] = x
			params["y"] = y
		}
		if len(args) > 3 {
			params["id"] = args[3]
		}
	case "Window.MoveToWorkspaceSilent":
		if len(args) > 1 {
			params["workspace_id"] = args[1]
		}
		if len(args) > 2 {
			params["window_id"] = args[2]
		}
	case "Workspace.Switch":
		if len(args) > 1 {
			params["id"] = args[1]
		}
	case "Workspace.MoveTo":
		if len(args) > 1 {
			params["workspace_id"] = args[1]
		}
		if len(args) > 2 {
			params["window_id"] = args[2]
		}
	case "Workspace.ToggleSpecial":
		if len(args) > 1 {
			params["name"] = args[1]
		}
	case "Monitor.Focus":
		if len(args) > 1 {
			params["id"] = args[1]
		}
	case "Monitor.MoveTo":
		if len(args) > 1 {
			params["monitor_id"] = args[1]
		}
		if len(args) > 2 {
			params["window_id"] = args[2]
		}
	case "Monitor.SetDpms":
		if len(args) > 1 {
			params["monitor_id"] = args[1]
		}
		if len(args) > 2 {
			params["on"] = args[2] == "1"
		}
	case "Layout.Set":
		if len(args) > 1 {
			params["name"] = args[1]
		}
	case "Config.Get":
		if len(args) > 1 {
			params["key"] = args[1]
		}
	case "Config.Set":
		if len(args) > 2 {
			params["key"] = args[1]
			params["value"] = args[2]
		}
	case "Config.Batch":
		if len(args) > 1 {
			var configs map[string]interface{}
			if err := json.Unmarshal([]byte(args[1]), &configs); err == nil {
				params["configs"] = configs
			} else {
				fmt.Printf("Error parsing JSON: %v\n", err)
				return
			}
		}
	case "Config.BindKey":
		if len(args) > 3 {
			params["mods"] = args[1]
			params["key"] = args[2]
			params["command"] = args[3]
		}
	case "Config.UnbindKey":
		if len(args) > 2 {
			params["mods"] = args[1]
			params["key"] = args[2]
		}
	case "System.Execute":
		if len(args) > 1 {
			params["command"] = args[1]
		}
	case "System.SwitchKeyboardLayout":
		if len(args) > 1 {
			params["action"] = args[1]
		} else {
			params["action"] = "next"
		}
	case "System.SetKeyboardLayouts":
		if len(args) > 1 {
			params["layouts"] = args[1]
		}
		if len(args) > 2 {
			params["variants"] = args[2]
		}
	}

	socketPath := fmt.Sprintf("/tmp/axctl-%d.sock", os.Getuid())
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		fmt.Printf("Error connecting to daemon: %v\n", err)
		return
	}
	defer conn.Close()

	req := map[string]interface{}{
		"id":     1,
		"method": method,
		"params": params,
	}
	json.NewEncoder(conn).Encode(req)

	var resp struct {
		Result interface{} `json:"result"`
		Error  string      `json:"error"`
	}
	json.NewDecoder(conn).Decode(&resp)

	if resp.Error != "" {
		fmt.Printf("Error: %s\n", resp.Error)
		return
	}

	if s, ok := resp.Result.(string); ok && s == "ok" {
		fmt.Println("Success")
		return
	}

	out, _ := json.MarshalIndent(resp.Result, "", "  ")
	fmt.Println(string(out))
}

func capitalize(s string) string {
	if len(s) == 0 {
		return ""
	}
	parts := strings.Split(s, "-")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = fmt.Sprintf("%c%s", p[0]-32, p[1:])
		}
	}
	return strings.Join(parts, "")
}
