package server

import (
	"encoding/json"
	"fmt"
	"net"
	"os"

	"axctl/pkg/ipc"
)

type Server struct {
	compositor ipc.Compositor
	socketPath string
}

func New(c ipc.Compositor, path string) *Server {
	return &Server{
		compositor: c,
		socketPath: path,
	}
}

type Request struct {
	ID     interface{}     `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type Response struct {
	ID     interface{} `json:"id"`
	Result interface{} `json:"result,omitempty"`
	Error  string      `json:"error,omitempty"`
}

func (s *Server) Start() error {
	_ = os.Remove(s.socketPath)
	l, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return err
	}
	defer l.Close()

	for {
		conn, err := l.Accept()
		if err != nil {
			continue
		}
		go s.handleConnection(conn)
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()
	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)

	for {
		var req Request
		if err := dec.Decode(&req); err != nil {
			return
		}

		fmt.Printf("[Server] Request: %s\n", req.Method)
		resp := Response{ID: req.ID}
		switch req.Method {
		case "Window.List":
			windows, err := s.compositor.ListWindows()
			if err != nil {
				fmt.Printf("[Server] Error ListWindows: %v\n", err)
				resp.Error = err.Error()
			} else {
				resp.Result = windows
			}
		case "Window.Focus":
			var params struct {
				ID string `json:"id"`
			}
			json.Unmarshal(req.Params, &params)
			err := s.compositor.FocusWindow(params.ID)
			if err != nil {
				resp.Error = err.Error()
			} else {
				resp.Result = "ok"
			}
		case "Window.Close":
			var params struct {
				ID string `json:"id"`
			}
			json.Unmarshal(req.Params, &params)
			err := s.compositor.CloseWindow(params.ID)
			if err != nil {
				resp.Error = err.Error()
			} else {
				resp.Result = "ok"
			}
		case "Workspace.List":
			workspaces, err := s.compositor.ListWorkspaces()
			if err != nil {
				resp.Error = err.Error()
			} else {
				resp.Result = workspaces
			}
		case "Workspace.Switch":
			var params struct {
				ID string `json:"id"`
			}
			json.Unmarshal(req.Params, &params)
			err := s.compositor.SwitchWorkspace(params.ID)
			if err != nil {
				resp.Error = err.Error()
			} else {
				resp.Result = "ok"
			}
		default:
			resp.Error = "method not found"
		}

		if err := enc.Encode(resp); err != nil {
			fmt.Printf("[Server] Error encoding response: %v\n", err)
		}
	}
}
