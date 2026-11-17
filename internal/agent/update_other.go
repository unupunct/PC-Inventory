//go:build !windows

package agent

import (
	"fmt"
	"os"
)

// swapExecutable replaces the running binary on Unix. Renaming over a running
// executable is allowed there, so the new file simply takes its place.
func swapExecutable(exe, staging string) error {
	if err := os.Chmod(staging, 0o755); err != nil {
		return fmt.Errorf("cannot mark new binary executable: %w", err)
	}
	if err := os.Rename(staging, exe); err != nil {
		return fmt.Errorf("cannot install new binary: %w", err)
	}
	return nil
}
