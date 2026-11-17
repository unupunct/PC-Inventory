# Repository About

**Description** (GitHub → About → gear icon):

> Self-hosted IT inventory and monitoring: Go server with an embedded dashboard, plus a Windows/macOS/Linux agent that reports real hardware and performance data.

**Topics:** `inventory` `monitoring` `go` `self-hosted` `sysadmin` `hardware` `windows` `linux` `macos`

**Website:** the latest release page —
`https://github.com/unupunct/PC-Inventory/releases/latest`

Both the description and the topics are already applied to
https://github.com/unupunct/PC-Inventory. This file records them so they can be
restored if the repository settings are ever reset.

## Screenshots

The images in `docs/screenshots/` are referenced from the README:

| File | Shows |
|---|---|
| `01-login.png` | Sign-in page |
| `02-dashboard.png` | Fleet cards, computer table, changes and activity panels |
| `04-computer-and-performance.png` | Selected computer details next to the CPU/memory charts |
| `05-changes-and-activity.png` | Hardware change history and agent activity |

They were captured from a development server fed by `test/sample-agent`, which
sends a clearly labelled synthetic inventory (hostname `sample-vm-windows`,
model "Synthetic (test only) Sample VM"). Real deployments show the same screens
filled with data from real machines — no values are hard-coded in the dashboard.
