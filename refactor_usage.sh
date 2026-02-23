#!/bin/bash
sed -i '/func printUsage() {/,/^}/c\
func printUsage() {\
\tfmt.Println(`Usage: axctl <command> <action> [args]\n\
Commands:\n\
  daemon                    Start the IPC daemon\n\
\n\
  window <action> [args]\n\
    list                    List all windows\n\
    focus <id>              Focus a window\n\
    focus-dir <l|r|u|d>     Focus in direction\n\
    close [id]              Close a window\n\
    move <dir> [id]         Move window\n\
    resize <w> <h> [id]     Resize window\n\
    toggle-floating [id]    Toggle floating\n\
    fullscreen <0|1> [id]   Set fullscreen\n\
    maximize <0|1> [id]     Set maximized\n\
    pin <0|1> [id]          Pin window\n\
    toggle-group [id]       Toggle window group (Hyprland)\n\
    group-nav <f|b>         Navigate group tabs\n\
    layout-prop <k> <v> [id] Set layout property (Niri/Mango)\n\
    move-pixel <x> <y> [id] Move window exactly by pixel\n\
    move-to-workspace-silent <ws> [id] Move window silently\n\
\n\
  workspace <action> [args]\n\
    list                    List all workspaces\n\
    switch <id>             Switch workspace\n\
    move-to <ws_id> [win_id] Move window to workspace\n\
    toggle-special <name>   Toggle special workspace\n\
\n\
  monitor <action> [args]\n\
    list                    List all monitors\n\
    focus <id>              Focus monitor\n\
    move-to <mon_id> [win_id] Move window to monitor\n\
\n\
  layout <action> [args]\n\
    set <name>              Set layout\n\
\n\
  config <action> [args]\n\
    get <key>               Get config key\n\
    set <key> <value>       Set config key\n\
                            Keys: gaps.inner, gaps.outer, border.width,\n\
                                  border.active_color, border.inactive_color,\n\
                                  opacity.active, opacity.inactive,\n\
                                  blur.enabled, blur.size, blur.passes\n\
    batch <json_string>     Batch apply configs\n\
    get-animations          Get animation configs\n\
    bind-key <mods> <key> <cmd> Bind a key\n\
    unbind-key <mods> <key> Unbind a key\n\
    reload                  Reload config\n\
\n\
  system <action> [args]\n\
    execute <cmd>           Execute command\n\
    get-cursor-position     Get absolute cursor position\n\
    exit                    Exit compositor`)\
}' cmd/axctl/main.go
bash refactor_usage.sh
rm refactor_usage.sh
go build -o axctl ./cmd/axctl/main.go
