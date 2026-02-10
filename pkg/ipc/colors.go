package ipc

import (
	"strings"
)

func FirstColor(colorStr string) string {
	fields := strings.Fields(colorStr)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
