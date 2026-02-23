#!/bin/bash
sed -i '/"Window.MoveToWorkspace":/a \
		"Window.MovePixel":            s.handleWindowMovePixel,\
		"Window.MoveToWorkspaceSilent": s.handleWindowMoveToWorkspaceSilent,\
		"Workspace.ToggleSpecial":      s.handleWorkspaceToggleSpecial,\
		"Config.Get":                   s.handleConfigGet,\
		"Config.Batch":                 s.handleConfigBatch,\
		"Config.GetAnimations":         s.handleConfigGetAnimations,\
		"Config.BindKey":               s.handleConfigBindKey,\
		"Config.UnbindKey":             s.handleConfigUnbindKey,\
		"System.GetCursorPosition":     s.handleSystemGetCursorPosition,
' pkg/server/server.go

cat << 'INNER_EOF' >> pkg/server/server.go

func (s *Server) handleWindowMovePixel(params json.RawMessage) (interface{}, error) {
	var p struct {
		ID string `json:"id"`
		X  int    `json:"x"`
		Y  int    `json:"y"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	id, err := s.resolveID(p.ID)
	if err != nil {
		return nil, err
	}
	return nil, s.compositor.MoveWindowPixel(id, p.X, p.Y)
}

func (s *Server) handleWindowMoveToWorkspaceSilent(params json.RawMessage) (interface{}, error) {
	var p struct {
		WindowID    string `json:"window_id"`
		WorkspaceID string `json:"workspace_id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	id, err := s.resolveID(p.WindowID)
	if err != nil {
		return nil, err
	}
	return nil, s.compositor.MoveToWorkspaceSilent(id, p.WorkspaceID)
}

func (s *Server) handleWorkspaceToggleSpecial(params json.RawMessage) (interface{}, error) {
	var p struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	return nil, s.compositor.ToggleSpecialWorkspace(p.Name)
}

func (s *Server) handleConfigGet(params json.RawMessage) (interface{}, error) {
	var p struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	return s.compositor.GetConfig(p.Key)
}

func (s *Server) handleConfigBatch(params json.RawMessage) (interface{}, error) {
	var p struct {
		Configs map[string]interface{} `json:"configs"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	return nil, s.compositor.BatchConfig(p.Configs)
}

func (s *Server) handleConfigGetAnimations(params json.RawMessage) (interface{}, error) {
	return s.compositor.GetAnimations()
}

func (s *Server) handleSystemGetCursorPosition(params json.RawMessage) (interface{}, error) {
	x, y, err := s.compositor.GetCursorPosition()
	if err != nil {
		return nil, err
	}
	return map[string]int{"x": x, "y": y}, nil
}

func (s *Server) handleConfigBindKey(params json.RawMessage) (interface{}, error) {
	var p struct {
		Mods    string `json:"mods"`
		Key     string `json:"key"`
		Command string `json:"command"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	return nil, s.compositor.BindKey(p.Mods, p.Key, p.Command)
}

func (s *Server) handleConfigUnbindKey(params json.RawMessage) (interface{}, error) {
	var p struct {
		Mods string `json:"mods"`
		Key  string `json:"key"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	return nil, s.compositor.UnbindKey(p.Mods, p.Key)
}
INNER_EOF
bash patch_server.sh
rm patch_server.sh
