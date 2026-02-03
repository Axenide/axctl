package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
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
	case "window":
		if len(os.Args) < 3 {
			usage()
			return
		}
		handleRPC("Window", os.Args[2:])
	case "workspace":
		if len(os.Args) < 3 {
			usage()
			return
		}
		handleRPC("Workspace", os.Args[2:])
	default:
		usage()
	}
}

func usage() {
	fmt.Println("Usage: axctl <command> <action> [args]")
	fmt.Println("\nCommands:")
	fmt.Println("  daemon                    Start the IPC daemon")
	fmt.Println("  window list               List all windows")
	fmt.Println("  window focus <id>         Focus a window")
	fmt.Println("  window close <id>         Close a window")
	fmt.Println("  workspace list            List all workspaces")
	fmt.Println("  workspace switch <id>     Switch to a workspace")
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
	method := fmt.Sprintf("%s.%s", category, capitalize(action))

	params := make(map[string]interface{})
	if len(args) > 1 {
		params["id"] = args[1]
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
	return fmt.Sprintf("%c%s", s[0]-32, s[1:])
}
