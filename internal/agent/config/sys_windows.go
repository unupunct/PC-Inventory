//go:build windows

package config

import (
	"os/exec"
	"regexp"
	"strings"
)

// regValue reads a REG_SZ value from the registry using reg.exe (no cgo).
func regValue(key, name string) string {
	out := runCmd("reg", "query", key, "/v", name)
	if out == "" {
		return ""
	}
	re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(name) + `\s+REG_SZ\s+(.+?)\s*$`)
	if m := re.FindStringSubmatch(out); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// runCmd runs a command and returns trimmed stdout (empty on error).
func runCmd(name string, args ...string) string {
	cmd := exec.Command(name, args...)
	hideWindow(cmd)
	b, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

var quotedRe = regexp.MustCompile(`"([^"]+)"`)

func extractQuoted(s, key string) string {
	idx := strings.Index(s, key)
	if idx < 0 {
		return ""
	}
	rest := s[idx+len(key):]
	m := quotedRe.FindStringSubmatch(rest)
	if len(m) == 2 {
		return m[1]
	}
	return ""
}
