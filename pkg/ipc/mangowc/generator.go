package mangowc

import (
	"axctl/pkg/ipc"
)

type Generator struct{}

// NewGenerator returns a new instance of the MangoWC config generator
func NewGenerator() *Generator {
	return &Generator{}
}

func (g *Generator) GenerateAppearance(config ipc.ConfigAppearance) string {
	return "# MangoWC Appearance Configuration Stub\n"
}

func (g *Generator) GenerateKeybinds(config ipc.ConfigKeybinds) string {
	return "# MangoWC Keybinds Configuration Stub\n"
}

func (g *Generator) GenerateWindowRules(rules []ipc.WindowRule) string {
	return "# MangoWC Window Rules Configuration Stub\n"
}
