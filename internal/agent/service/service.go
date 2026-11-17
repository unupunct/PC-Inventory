// Package service manages the agent as an OS service:
// Windows service (SCM), systemd unit (Linux), launchd plist (macOS).
package service

import (
	"errors"
	"runtime"
)

// ErrNotSupported is returned on platforms without service support.
var ErrNotSupported = errors.New("service management is not supported on " + runtime.GOOS)

// Name is the service name on every platform.
const Name = "pcim-agent"

const DisplayName = "PC Inventory Manager Agent"

const Description = "Collects hardware inventory and performance metrics and reports them to the PC Inventory Manager server."
