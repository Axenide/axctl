package server

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStandaloneApplicationRPC(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_DATA_DIRS", filepath.Join(root, "system"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CURRENT_DESKTOP", "Ambxst")
	entryPath := filepath.Join(root, "data", "applications", "org.example.Editor.desktop")
	if err := os.MkdirAll(filepath.Dir(entryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entryPath, []byte("[Desktop Entry]\nType=Application\nName=Example Editor\nExec=example-editor\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	server := New(nil, filepath.Join(root, "axctl.sock"))
	defer server.Close()
	listener, err := net.Listen("unix", filepath.Join(root, "rpc.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr == nil {
			server.handleConnection(conn)
		}
	}()
	conn, err := net.DialTimeout("unix", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)
	for _, test := range []struct {
		method string
		params any
		check  func(json.RawMessage, string)
	}{
		{
			method: "Application.Search",
			params: map[string]any{"query": "example"},
			check: func(result json.RawMessage, rpcErr string) {
				if rpcErr != "" {
					t.Fatal(rpcErr)
				}
				var apps []map[string]any
				if err := json.Unmarshal(result, &apps); err != nil {
					t.Fatal(err)
				}
				if len(apps) != 1 || apps[0]["id"] != "org.example.Editor.desktop" {
					t.Fatalf("search result = %#v", apps)
				}
			},
		},
		{
			method: "Window.List",
			params: map[string]any{},
			check: func(_ json.RawMessage, rpcErr string) {
				if rpcErr != "no supported compositor is available" {
					t.Fatalf("compositor error = %q", rpcErr)
				}
			},
		},
	} {
		if err := encoder.Encode(map[string]any{"id": 1, "method": test.method, "params": test.params}); err != nil {
			t.Fatal(err)
		}
		var response struct {
			Result json.RawMessage `json:"result"`
			Error  string          `json:"error"`
		}
		if err := decoder.Decode(&response); err != nil {
			t.Fatal(err)
		}
		test.check(response.Result, response.Error)
	}
}
