package mango

type mangoClient struct {
	ID          uint64 `json:"id"`
	Title       string `json:"title"`
	AppID       string `json:"appid"`
	Tags        []int  `json:"tags"`
	Floating    int    `json:"floating"`
	Fullscreen  int    `json:"fullscreen"`
	Maximized   int    `json:"maximized"`
	Global      int    `json:"global"`
	Monitor     int    `json:"monitor"`
	MonitorName string `json:"monitor_name"`
	X           int    `json:"x"`
	Y           int    `json:"y"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
}

type mangoMonitor struct {
	Name    string `json:"name"`
	Active  int    `json:"active"`
	Focused int    `json:"focused"`
	Index   int    `json:"index"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Scale   int    `json:"scale"`
	Tags    []int  `json:"tags"`
}

type mangoTag struct {
	Name   string `json:"name"`
	Index  int    `json:"index"`
	Active int    `json:"active"`
	Urgent int    `json:"urgent"`
}

type mangoCursorPos struct {
	X int `json:"x"`
	Y int `json:"y"`
}
