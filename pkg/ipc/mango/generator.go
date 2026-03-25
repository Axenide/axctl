package mango

import (
	"axctl/pkg/ipc"
)

type Generator struct{}

// NewGenerator returns a new instance of the Mango config generator
func NewGenerator() *Generator {
	return &Generator{}
}

func (g *Generator) GenerateAppearance(config ipc.ConfigAppearance) string {
	return "# Mango Appearance Configuration Stub\n"
}

func (g *Generator) GenerateKeybinds(config ipc.ConfigKeybinds) string {
	return "# Mango Keybinds Configuration Stub\n"
}

func (g *Generator) GenerateWindowRules(rules []ipc.WindowRule) string {
	return "# Mango Window Rules Configuration Stub\n"
}

func (g *Generator) GenerateLayerRules(rules []ipc.LayerRule) string {
	return "# Mango Layer Rules Configuration Stub\n"
}

func (g *Generator) GenerateStartup(exec []string, execOnce []string) string {
	return ""
}
