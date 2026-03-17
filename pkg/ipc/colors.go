package ipc

import (
	"regexp"
	"strings"
)

func FirstColor(colorStr string) string {
	fields := strings.Fields(colorStr)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func MangoColor(colorStr string) string {
	fields := strings.Fields(colorStr)
	if len(fields) == 0 {
		return ""
	}
	c := fields[0]

	// Remove rgb/rgba wrapper
	re := regexp.MustCompile(`(?i)^rgba?\(([a-fA-F0-9]+)\)$`)
	if m := re.FindStringSubmatch(c); len(m) > 1 {
		c = m[1]
	}

	// Strip #
	if strings.HasPrefix(c, "#") {
		c = c[1:]
	}

	// Strip 0x
	if strings.HasPrefix(strings.ToLower(c), "0x") {
		c = c[2:]
	}

	// Mango strictly expects RRGGBBAA (uint32).
	// If alpha is missing (6 chars), passing "0xff0000" causes it to be parsed as 0x00FF0000
	// R=0x00, G=0xFF, B=0x00, A=0x00 -> fully transparent green!
	if len(c) == 6 {
		c = c + "ff" // append opaque alpha
	} else if len(c) == 3 {
		c = string([]byte{c[0], c[0], c[1], c[1], c[2], c[2], 'f', 'f'})
	} else if len(c) == 4 {
		c = string([]byte{c[0], c[0], c[1], c[1], c[2], c[2], c[3], c[3]})
	}

	return "0x" + c
}
