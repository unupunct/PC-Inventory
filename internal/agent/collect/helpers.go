package collect

import (
	"context"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// runCommand runs a command and returns its output lines.
func runCommand(ctx context.Context, args []string) []string {
	if len(args) == 0 {
		return nil
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	hideCmd(cmd)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var lines []string
	for _, l := range strings.Split(string(out), "\n") {
		l = strings.TrimRight(l, "\r")
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// batteryStats is defined per-platform in the *_<os>.go files.

type batteryInfo struct {
	Percent   *float64
	Status    string
	PluggedIn *bool
	DesignMWh *int64
	FullMWh   *int64
	Cycles    *int
	Name      string
}

func isRemovableMount(mount string) bool {
	lm := strings.ToLower(mount)
	return strings.HasPrefix(lm, "/media/") || strings.HasPrefix(lm, "/mnt/") || strings.HasPrefix(lm, "/run/media/") || strings.HasPrefix(lm, "/volumes/")
}

var driveLetterRe = regexp.MustCompile(`^[A-Za-z]:`)

func isDriveRoot(p string) bool { return driveLetterRe.MatchString(p) }

func nowUTC() time.Time { return time.Now().UTC() }
