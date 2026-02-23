#!/bin/bash

# Add new CLI commands to main.go

sed -i '/windowCmd := flag.NewFlagSet("window", flag.ExitOnError)/a \
	windowMovePixelCmd := flag.NewFlagSet("move-pixel", flag.ExitOnError)\
	windowMoveToWorkspaceSilentCmd := flag.NewFlagSet("move-to-workspace-silent", flag.ExitOnError)
' cmd/axctl/main.go

sed -i '/workspaceCmd := flag.NewFlagSet("workspace", flag.ExitOnError)/a \
	workspaceToggleSpecialCmd := flag.NewFlagSet("toggle-special", flag.ExitOnError)
' cmd/axctl/main.go

sed -i '/configCmd := flag.NewFlagSet("config", flag.ExitOnError)/a \
	configGetCmd := flag.NewFlagSet("get", flag.ExitOnError)\
	configBatchCmd := flag.NewFlagSet("batch", flag.ExitOnError)\
	configGetAnimationsCmd := flag.NewFlagSet("get-animations", flag.ExitOnError)\
	configBindKeyCmd := flag.NewFlagSet("bind-key", flag.ExitOnError)\
	configUnbindKeyCmd := flag.NewFlagSet("unbind-key", flag.ExitOnError)
' cmd/axctl/main.go

sed -i '/systemCmd := flag.NewFlagSet("system", flag.ExitOnError)/a \
	systemGetCursorPositionCmd := flag.NewFlagSet("get-cursor-position", flag.ExitOnError)
' cmd/axctl/main.go

# Update usage strings

sed -i '/fmt.Println("  window <action> \\[args\\]")/a \
	fmt.Println("    move-pixel <x> <y> [id]")\
	fmt.Println("    move-to-workspace-silent <workspace_id> [window_id]")
' cmd/axctl/main.go

sed -i '/fmt.Println("  workspace <action> \\[args\\]")/a \
	fmt.Println("    toggle-special <name>")
' cmd/axctl/main.go

sed -i '/fmt.Println("  config <action> \\[args\\]")/a \
	fmt.Println("    get <key>")\
	fmt.Println("    batch <json_string>")\
	fmt.Println("    get-animations")\
	fmt.Println("    bind-key <mods> <key> <command>")\
	fmt.Println("    unbind-key <mods> <key>")
' cmd/axctl/main.go

sed -i '/fmt.Println("  system <action> \\[args\\]")/a \
	fmt.Println("    get-cursor-position")
' cmd/axctl/main.go

# Update sub-command handling

sed -i '/case "layout-prop":/a \
		case "move-pixel":\
			windowMovePixelCmd.Parse(os.Args[3:])\
			if windowMovePixelCmd.NArg() < 2 {\
				fmt.Println("Usage: axctl window move-pixel <x> <y> [id]")\
				os.Exit(1)\
			}\
			x, _ := strconv.Atoi(windowMovePixelCmd.Arg(0))\
			y, _ := strconv.Atoi(windowMovePixelCmd.Arg(1))\
			id := ""\
			if windowMovePixelCmd.NArg() >= 3 {\
				id = windowMovePixelCmd.Arg(2)\
			}\
			sendRPC("Window.MovePixel", map[string]interface{}{"id": id, "x": x, "y": y})\
		case "move-to-workspace-silent":\
			windowMoveToWorkspaceSilentCmd.Parse(os.Args[3:])\
			if windowMoveToWorkspaceSilentCmd.NArg() < 1 {\
				fmt.Println("Usage: axctl window move-to-workspace-silent <workspace_id> [window_id]")\
				os.Exit(1)\
			}\
			wsID := windowMoveToWorkspaceSilentCmd.Arg(0)\
			winID := ""\
			if windowMoveToWorkspaceSilentCmd.NArg() >= 2 {\
				winID = windowMoveToWorkspaceSilentCmd.Arg(1)\
			}\
			sendRPC("Window.MoveToWorkspaceSilent", map[string]interface{}{"workspace_id": wsID, "window_id": winID})
' cmd/axctl/main.go

sed -i '/case "move-to":/a \
		case "toggle-special":\
			workspaceToggleSpecialCmd.Parse(os.Args[3:])\
			if workspaceToggleSpecialCmd.NArg() < 1 {\
				fmt.Println("Usage: axctl workspace toggle-special <name>")\
				os.Exit(1)\
			}\
			sendRPC("Workspace.ToggleSpecial", map[string]interface{}{"name": workspaceToggleSpecialCmd.Arg(0)})
' cmd/axctl/main.go

sed -i '/case "reload":/a \
		case "get":\
			configGetCmd.Parse(os.Args[3:])\
			if configGetCmd.NArg() < 1 {\
				fmt.Println("Usage: axctl config get <key>")\
				os.Exit(1)\
			}\
			sendRPC("Config.Get", map[string]interface{}{"key": configGetCmd.Arg(0)})\
		case "batch":\
			configBatchCmd.Parse(os.Args[3:])\
			if configBatchCmd.NArg() < 1 {\
				fmt.Println("Usage: axctl config batch <json_string>")\
				os.Exit(1)\
			}\
			var configs map[string]interface{}\
			if err := json.Unmarshal([]byte(configBatchCmd.Arg(0)), &configs); err != nil {\
				fmt.Println("Invalid JSON:", err)\
				os.Exit(1)\
			}\
			sendRPC("Config.Batch", map[string]interface{}{"configs": configs})\
		case "get-animations":\
			configGetAnimationsCmd.Parse(os.Args[3:])\
			sendRPC("Config.GetAnimations", nil)\
		case "bind-key":\
			configBindKeyCmd.Parse(os.Args[3:])\
			if configBindKeyCmd.NArg() < 3 {\
				fmt.Println("Usage: axctl config bind-key <mods> <key> <command>")\
				os.Exit(1)\
			}\
			sendRPC("Config.BindKey", map[string]interface{}{"mods": configBindKeyCmd.Arg(0), "key": configBindKeyCmd.Arg(1), "command": strings.Join(configBindKeyCmd.Args()[2:], " ")})\
		case "unbind-key":\
			configUnbindKeyCmd.Parse(os.Args[3:])\
			if configUnbindKeyCmd.NArg() < 2 {\
				fmt.Println("Usage: axctl config unbind-key <mods> <key>")\
				os.Exit(1)\
			}\
			sendRPC("Config.UnbindKey", map[string]interface{}{"mods": configUnbindKeyCmd.Arg(0), "key": configUnbindKeyCmd.Arg(1)})
' cmd/axctl/main.go

sed -i '/case "exit":/a \
		case "get-cursor-position":\
			systemGetCursorPositionCmd.Parse(os.Args[3:])\
			sendRPC("System.GetCursorPosition", nil)
' cmd/axctl/main.go

# Add strconv import if missing
sed -i '/"os"/a \
	"strconv"
' cmd/axctl/main.go

bash patch_main.sh
rm patch_main.sh

go build -o axctl ./cmd/axctl/main.go
