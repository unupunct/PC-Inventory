//go:build !windows

package config

import (
	"os/exec"
	"regexp"
	"strings"
)

// regValue is only meaningful on Windows.
func regValue(key, name string) string { return "" }

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
