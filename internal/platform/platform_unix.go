//go:build unix

package platform

import (
	"os/exec"
	"syscall"
)

// applyDetached puts the launcher in its own process group, so a Ctrl+C aimed
// at the TUI (or the terminal window closing) cannot take the file manager
// down with it.
func applyDetached(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
