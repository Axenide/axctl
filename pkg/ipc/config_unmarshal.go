package ipc

import (
	"encoding/json"
	"strings"
)

// UnmarshalJSON handles both nested and flat "gaps.inner" style JSON seamlessly
func (c *ConfigAppearance) UnmarshalJSON(data []byte) error {
	type Alias ConfigAppearance
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(c),
	}
	
	// First try standard nested unmarshalling
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	// Then also unmarshal as a flat map to catch dot notation keys
	var flat map[string]interface{}
	if err := json.Unmarshal(data, &flat); err == nil {
		c.applyFlatKeys(flat)
	}

	return nil
}

func (c *ConfigAppearance) applyFlatKeys(flat map[string]interface{}) {
	for k, v := range flat {
		valF, isFloat := v.(float64)
		valS, isStr := v.(string)
		valB, isBool := v.(bool)
		
		valI := int(valF)

		if strings.Contains(k, ".") {
			parts := strings.Split(k, ".")
			if len(parts) == 2 {
				switch parts[0] {
				case "gaps":
					if c.Gaps == nil {
						c.Gaps = &Gaps{}
					}
					if parts[1] == "inner" && isFloat {
						vCopy := valI
						c.Gaps.Inner = &vCopy
					} else if parts[1] == "outer" && isFloat {
						vCopy := valI
						c.Gaps.Outer = &vCopy
					}
				case "border":
					if c.Border == nil {
						c.Border = &Border{}
					}
					if parts[1] == "width" && isFloat {
						vCopy := valI
						c.Border.Width = &vCopy
					} else if parts[1] == "active_color" && isStr {
						vCopy := valS
						c.Border.ActiveColor = &vCopy
					} else if parts[1] == "inactive_color" && isStr {
						vCopy := valS
						c.Border.InactiveColor = &vCopy
					}
				case "opacity":
					if c.Opacity == nil {
						c.Opacity = &Opacity{}
					}
					if parts[1] == "active" && isFloat {
						vCopy := valF
						c.Opacity.Active = &vCopy
					} else if parts[1] == "inactive" && isFloat {
						vCopy := valF
						c.Opacity.Inactive = &vCopy
					}
				case "blur":
					if c.Blur == nil {
						c.Blur = &Blur{}
					}
					if parts[1] == "enabled" && isBool {
						vCopy := valB
						c.Blur.Enabled = &vCopy
					} else if parts[1] == "size" && isFloat {
						vCopy := valI
						c.Blur.Size = &vCopy
					} else if parts[1] == "passes" && isFloat {
						vCopy := valI
						c.Blur.Passes = &vCopy
					}
				case "shadow":
					if c.Shadow == nil {
						c.Shadow = &Shadow{}
					}
					if parts[1] == "enabled" && isBool {
						vCopy := valB
						c.Shadow.Enabled = &vCopy
					} else if parts[1] == "size" && isFloat {
						vCopy := valI
						c.Shadow.Size = &vCopy
					} else if parts[1] == "color" && isStr {
						vCopy := valS
						c.Shadow.Color = &vCopy
					}
				}
			}
		} else {
			// Single level flat like "rounding"
			if k == "rounding" && isFloat {
				if c.Border == nil {
					c.Border = &Border{}
				}
				vCopy := valI
				c.Border.Rounding = &vCopy
			}
		}
	}
}
