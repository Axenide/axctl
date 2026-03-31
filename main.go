package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"syscall"

	"axctl/pkg/config"
	"axctl/pkg/ipc"
	"axctl/pkg/ipc/hyprland"
	"axctl/pkg/ipc/mango"
	"axctl/pkg/ipc/niri"
	"axctl/pkg/server"
)

var Version = "dev"

func main() {
	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "--version", "-v", "version":
			printVersion()
			return
		}
	}

	if len(os.Args) < 2 {
		usage()
		return
	}

	customConfigPath := ""

	// Parse -c flag before command (only for daemon command)
	i := 1
	for i < len(os.Args) && os.Args[i] != "daemon" {
		if os.Args[i] == "-c" && i+1 < len(os.Args) {
			customConfigPath = os.Args[i+1]
			i += 2
		} else {
			break
		}
	}

	// Shift args to remove parsed flags
	remainingArgs := os.Args[i:]
	switch remainingArgs[0] {
	case "daemon":
		runDaemon(customConfigPath)
	case "subscribe":
		runSubscribe()
	case "window", "workspace", "monitor", "layout", "config", "system":
		if len(remainingArgs) < 2 {
			usage()
			return
		}
		handleRPC(remainingArgs[0], remainingArgs[1:])
	default:
		usage()
	}
}

func printVersion() {
	version := Version
	if version == "dev" {
		if bi, ok := debug.ReadBuildInfo(); ok {
			if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
				version = bi.Main.Version
			}
		}
	}

	fmt.Printf("axctl %s\n", version)
}

func usage() {
	fmt.Println("Usage: axctl [-c <path>] <command> <action> [args]")
	fmt.Println("\nOptions:")
	fmt.Println("  -c <path>                 Use custom config file path (daemon only)")
	fmt.Println("\nCommands:")
	fmt.Println("  daemon                    Start the IPC daemon")
	fmt.Println("  subscribe                 Stream events from the daemon")
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
	fmt.Println("    apply <json_string>     Apply declarative universal config payload")
	fmt.Println("    raw-batch <command>     Send raw compositor batch command")
	fmt.Println("    get-animations          Get animation configs")
	fmt.Println("    bind-key <mods> <key> <cmd> Bind a key")
	fmt.Println("    unbind-key <mods> <key> Unbind a key")
	fmt.Println("    keybinds-batch <json>   Batch bind/unbind keys (structured JSON)")
	fmt.Println("    reload                  Reload config")
	fmt.Println("\n  system <action> [args]")
	fmt.Println("    execute <cmd>           Execute command")
	fmt.Println("    get-cursor-position     Get absolute cursor position")
	fmt.Println("    switch-keyboard-layout [next|prev] Switch keyboard layout")
	fmt.Println("    set-keyboard-layouts <layouts> [variants] Set keyboard layouts (e.g. \"us,es\" \"altgr-intl,\")")
	fmt.Println("    idle-inhibit <0|1>      Inhibit or allow idle/sleep")
	fmt.Println("    idle-wait <ms>          Block until system is idle for <ms> milliseconds (honors inhibitors)")
	fmt.Println("    resume-wait <ms>        Block until system resumes from <ms> milliseconds of idle")
	fmt.Println("    is-idle <ms>            Check if system is currently idle for <ms> milliseconds (returns true/false)")
	fmt.Println("    input-idle-wait <ms>    Block until physical input is idle (ignores inhibitors)")
	fmt.Println("    input-resume-wait <ms>  Block until physical input resumes")
	fmt.Println("    is-input-idle <ms>      Check if physical input is idle (returns true/false)")
	fmt.Println("    is-inhibited            Check if axctl has idle inhibited (returns true/false)")
	fmt.Println("    idle-monitor-create [timeout_ms] [respect_inhibitors 0|1] [enabled 0|1] Create an idle monitor")
	fmt.Println("    idle-monitor-update <id> [timeout_ms] [respect_inhibitors 0|1] [enabled 0|1] Update an idle monitor")
	fmt.Println("    idle-monitor-get <id>   Get idle monitor state")
	fmt.Println("    idle-monitor-destroy <id> Destroy an idle monitor")
	fmt.Println("    idle-inhibitor-create [enabled 0|1] Create an idle inhibitor")
	fmt.Println("    idle-inhibitor-set <id> <0|1> Enable/disable an idle inhibitor")
	fmt.Println("    idle-inhibitor-get <id> Get idle inhibitor state")
	fmt.Println("    idle-inhibitor-destroy <id> Destroy an idle inhibitor")
	fmt.Println("    inhibit-system <0|1>    Enable or disable system-wide idle inhibition (systemd)")
	fmt.Println("    is-system-inhibited   Check if system-wide idle inhibition is active")
	fmt.Println("    app-inhibit-check [patterns...] Check if apps are inhibiting idle (default: vlc,mpv,firefox,chromium,brave,steam)")
	fmt.Println("    media-inhibit-check   Check for active audio/media (PulseAudio/PipeWire)")
	fmt.Println("    get-capabilities        Get compositor capabilities")
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

func runDaemon(customConfigPath string) {
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

	// 3. Try Mango fallback
	if comp == nil && err == nil {
		// First try the current WAYLAND_DISPLAY
		c, e := mango.New()
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
				c, e = mango.New()
				if e == nil {
					comp = c
					err = nil
					break
				}
			}

			if comp == nil {
				fmt.Printf("Debug - Mango detection failed on all sockets.\n")
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

	// Single instance check
	if conn, err := net.Dial("unix", socketPath); err == nil {
		conn.Close()
		fmt.Println("Error: axctl daemon is already running.")
		os.Exit(1)
	}
	os.Remove(socketPath) // Clean up stale socket if daemon is not running

	srv := server.New(comp, socketPath)

	fmt.Printf("Starting axctl daemon on %s\n", socketPath)

	// Load TOML config if it exists
	var cfgWatcher *config.ConfigWatcher
	configPath := customConfigPath
	if configPath == "" {
		configPath = config.DefaultConfigPath()
	}
	if _, statErr := os.Stat(configPath); statErr == nil {
		cfg, cfgErr := config.LoadConfig(configPath)
		if cfgErr != nil {
			fmt.Printf("[axctl-config] Error loading config: %v\n", cfgErr)
		} else {
			fmt.Printf("[axctl-config] Loaded config from %s\n", configPath)
			if applyErr := config.ApplyConfig(cfg, comp); applyErr != nil {
				fmt.Printf("[axctl-config] Error applying config: %v\n", applyErr)
			}
		}

		// Watch for config changes
		watcher, watchErr := config.NewConfigWatcher()
		if watchErr != nil {
			fmt.Printf("[axctl-config] Warning: could not start watcher: %v\n", watchErr)
		} else {
			watcher.Start(configPath, func(newCfg *config.TOMLConfig) {
				fmt.Println("[axctl-config] Config changed, reloading...")
				if applyErr := config.ApplyConfig(newCfg, comp); applyErr != nil {
					fmt.Printf("[axctl-config] Error applying config: %v\n", applyErr)
				}
			})
			cfgWatcher = watcher
		}
	} else {
		fmt.Printf("[axctl-config] No config file at %s, skipping\n", configPath)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := srv.Start(); err != nil {
			fmt.Printf("Server error: %v\n", err)
			os.Exit(1)
		}
	}()

	<-sig
	if cfgWatcher != nil {
		cfgWatcher.Stop()
	}
	os.Remove(socketPath)
}

func runSubscribe() {
	socketPath := fmt.Sprintf("/tmp/axctl-%d.sock", os.Getuid())
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		fmt.Printf("Error connecting to daemon: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	req := map[string]interface{}{
		"id":     1,
		"method": "System.Subscribe",
		"params": map[string]interface{}{},
	}
	json.NewEncoder(conn).Encode(req)

	dec := json.NewDecoder(conn)
	for {
		var msg json.RawMessage
		if err := dec.Decode(&msg); err != nil {
			fmt.Printf("Connection closed or error: %v\n", err)
			os.Exit(1)
		}
		var notif struct {
			JSONRPC string `json:"jsonrpc"`
		}
		if err := json.Unmarshal(msg, &notif); err == nil && notif.JSONRPC == "2.0" {
			fmt.Println(string(msg))
		}
	}
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
	case "Config.Apply":
		if len(args) > 1 {
			params["payload"] = args[1]
		}
	case "Config.RawBatch":
		if len(args) > 1 {
			params["command"] = args[1]
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
	case "Config.KeybindsBatch":
		if len(args) > 1 {
			params["payload"] = args[1]
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
	case "System.IdleInhibit":
		if len(args) > 1 {
			params["on"] = args[1] == "1"
		}
	case "System.IdleWait", "System.ResumeWait", "System.IsIdle", "System.InputIdleWait", "System.InputResumeWait", "System.IsInputIdle":
		if len(args) > 1 {
			var ms int
			fmt.Sscanf(args[1], "%d", &ms)
			params["timeout_ms"] = ms
		}
	case "System.IsInhibited":
		// No args needed
	case "System.IdleMonitorCreate":
		if len(args) > 1 {
			var ms int
			fmt.Sscanf(args[1], "%d", &ms)
			params["timeout_ms"] = ms
		}
		if len(args) > 2 {
			params["respect_inhibitors"] = args[2] == "1"
		}
		if len(args) > 3 {
			params["enabled"] = args[3] == "1"
		}
	case "System.IdleMonitorUpdate":
		if len(args) > 1 {
			var id int
			fmt.Sscanf(args[1], "%d", &id)
			params["id"] = id
		}
		if len(args) > 2 {
			var ms int
			fmt.Sscanf(args[2], "%d", &ms)
			params["timeout_ms"] = ms
		}
		if len(args) > 3 {
			params["respect_inhibitors"] = args[3] == "1"
		}
		if len(args) > 4 {
			params["enabled"] = args[4] == "1"
		}
	case "System.IdleMonitorGet", "System.IdleMonitorDestroy":
		if len(args) > 1 {
			var id int
			fmt.Sscanf(args[1], "%d", &id)
			params["id"] = id
		}
	case "System.IdleInhibitorCreate":
		if len(args) > 1 {
			params["enabled"] = args[1] == "1"
		}
	case "System.IdleInhibitorSet":
		if len(args) > 2 {
			var id int
			fmt.Sscanf(args[1], "%d", &id)
			params["id"] = id
			params["enabled"] = args[2] == "1"
		}
	case "System.IdleInhibitorGet", "System.IdleInhibitorDestroy":
		if len(args) > 1 {
			var id int
			fmt.Sscanf(args[1], "%d", &id)
			params["id"] = id
		}
	case "System.InhibitSystem":
		if len(args) > 1 {
			params["on"] = args[1] == "1"
		}
	case "System.IsSystemInhibited":
		// No args needed
	case "System.AppInhibitCheck":
		if len(args) > 1 {
			var patterns []string
			for i := 1; i < len(args); i++ {
				patterns = append(patterns, args[i])
			}
			params["patterns"] = patterns
		}
	case "System.MediaInhibitCheck":
		// No args needed - checks PulseAudio/PipeWire sink-inputs
	case "System.Exit":
		// No args needed - exits the compositor
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
