package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
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
	fmt.Println("    focus <id>              Focus a window")
	fmt.Println("    focus-dir <l|r|u|d>     Focus in direction")
	fmt.Println("    close <id>              Close a window")
	fmt.Println("    move <id> <dir>         Move window")
	fmt.Println("    resize <id> <w> <h>     Resize window")
	fmt.Println("    toggle-floating <id>    Toggle floating")
	fmt.Println("    fullscreen <id> <0|1>   Set fullscreen")
	fmt.Println("\n  workspace <action> [args]")
	fmt.Println("    list                    List all workspaces")
	fmt.Println("    switch <id>             Switch workspace")
	fmt.Println("    move-to <win_id> <ws_id> Move window to workspace")
	fmt.Println("\n  monitor <action> [args]")
	fmt.Println("    list                    List all monitors")
	fmt.Println("    focus <id>              Focus monitor")
	fmt.Println("    move-to <win_id> <mon_id> Move window to monitor")
	fmt.Println("\n  layout <action> [args]")
	fmt.Println("    set <name>              Set layout")
	fmt.Println("\n  config <action> [args]")
	fmt.Println("    set <key> <value>       Set config key")
	fmt.Println("    reload                  Reload config")
	fmt.Println("\n  system <action> [args]")
	fmt.Println("    execute <cmd>           Execute command")
	fmt.Println("    exit                    Exit compositor")
}

func runDaemon() {
	var comp ipc.Compositor
	var err error

	if os.Getenv("HYPRLAND_INSTANCE_SIGNATURE") != "" {
		comp, err = hyprland.New()
	} else if os.Getenv("NIRI_SOCKET") != "" {
		comp, err = niri.New()
	} else {
		comp, err = mangowc.New()
	}

	if err != nil {
		fmt.Printf("Error initializing compositor: %v\n", err)
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
	case "Window.FocusDirection":
		if len(args) > 1 {
			params["direction"] = args[1]
		}
	case "Window.Close":
		if len(args) > 1 {
			params["id"] = args[1]
		}
	case "Window.Move":
		if len(args) > 2 {
			params["id"] = args[1]
			params["direction"] = args[2]
		}
	case "Window.Resize":
		if len(args) > 3 {
			var w, h int
			params["id"] = args[1]
			fmt.Sscanf(args[2], "%d", &w)
			fmt.Sscanf(args[3], "%d", &h)
			params["width"] = w
			params["height"] = h
		}
	case "Window.ToggleFloating":
		if len(args) > 1 {
			params["id"] = args[1]
		}
	case "Window.SetFullscreen":
		if len(args) > 2 {
			params["id"] = args[1]
			params["state"] = args[2] == "1"
		}
	case "Workspace.Switch":
		if len(args) > 1 {
			params["id"] = args[1]
		}
	case "Workspace.MoveTo":
		if len(args) > 2 {
			params["window_id"] = args[1]
			params["workspace_id"] = args[2]
		}
	case "Monitor.Focus":
		if len(args) > 1 {
			params["id"] = args[1]
		}
	case "Monitor.MoveTo":
		if len(args) > 2 {
			params["window_id"] = args[1]
			params["monitor_id"] = args[2]
		}
	case "Layout.Set":
		if len(args) > 1 {
			params["name"] = args[1]
		}
	case "Config.Set":
		if len(args) > 2 {
			params["key"] = args[1]
			params["value"] = args[2]
		}
	case "System.Execute":
		if len(args) > 1 {
			params["command"] = args[1]
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
