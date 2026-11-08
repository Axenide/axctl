package mango

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type mangoBool bool

func (b *mangoBool) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("true")) {
		*b = true
		return nil
	}
	if bytes.Equal(trimmed, []byte("false")) || bytes.Equal(trimmed, []byte("null")) {
		*b = false
		return nil
	}
	var number int
	if err := json.Unmarshal(trimmed, &number); err == nil {
		*b = number != 0
		return nil
	}
	return fmt.Errorf("invalid Mango boolean %s", trimmed)
}

func (b mangoBool) value() bool { return bool(b) }

type mangoClient struct {
	ID              uint64    `json:"id"`
	PID             int       `json:"pid"`
	ForeignID       string    `json:"foreign_toplevel_id"`
	Title           string    `json:"title"`
	AppID           string    `json:"appid"`
	Tags            []int     `json:"tags"`
	Floating        mangoBool `json:"is_floating"`
	Fullscreen      mangoBool `json:"is_fullscreen"`
	Maximized       mangoBool `json:"is_maximized"`
	Global          mangoBool `json:"is_global"`
	Overlay         mangoBool `json:"is_overlay"`
	FakeFullscreen  mangoBool `json:"is_fakefullscreen"`
	Minimized       mangoBool `json:"is_minimized"`
	Urgent          mangoBool `json:"is_urgent"`
	Visible         mangoBool `json:"is_visible"`
	Focused         mangoBool `json:"is_focused"`
	IsGroup         mangoBool `json:"is_group"`
	GroupFocus      mangoBool `json:"is_group_focus"`
	Scratchpad      mangoBool `json:"is_scratchpad"`
	NamedScratchpad mangoBool `json:"is_namedscratchpad"`
	XWayland        mangoBool `json:"is_xwayland"`
	Monitor         string    `json:"monitor"`
	X               int       `json:"x"`
	Y               int       `json:"y"`
	Width           int       `json:"width"`
	Height          int       `json:"height"`
	ScrollerRatio   float64   `json:"scroller_proportion"`
}

type mangoMonitor struct {
	Name         string     `json:"name"`
	Active       mangoBool  `json:"active"`
	Index        int        `json:"index"`
	X            int        `json:"x"`
	Y            int        `json:"y"`
	Width        int        `json:"width"`
	Height       int        `json:"height"`
	Scale        float64    `json:"scale"`
	Tags         []mangoTag `json:"tags"`
	ActiveTags   []int      `json:"active_tags"`
	LayoutIndex  int        `json:"layout_index"`
	LayoutSymbol string     `json:"layout_symbol"`
	IsHDR        mangoBool  `json:"is_hdr"`
	IsVRR        mangoBool  `json:"is_vrr"`
}

type mangoTag struct {
	Index       int       `json:"index"`
	Active      mangoBool `json:"is_active"`
	Urgent      mangoBool `json:"is_urgent"`
	Layout      string    `json:"layout"`
	ClientCount int       `json:"client_count"`
}

type mangoTagsForMonitor struct {
	Monitor string     `json:"monitor"`
	Tags    []mangoTag `json:"tags"`
}

type mangoAllTags struct {
	Monitors []mangoTagsForMonitor `json:"all_tags"`
}

type mangoClients struct {
	Clients []mangoClient `json:"clients"`
}

type mangoMonitors struct {
	Monitors []mangoMonitor `json:"monitors"`
}

type mangoCursorPos struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}
