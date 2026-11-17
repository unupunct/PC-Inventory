//go:build !windows

package collect

import "os/exec"

func hideCmd(cmd *exec.Cmd) {}
