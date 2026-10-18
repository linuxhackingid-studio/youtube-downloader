//go:build windows

package platform

import (
	"os/exec"
	"syscall"
)

// applyDetached configures a launcher process so it survives the TUI and
// never flashes a console window: HideWindow suppresses the console
// allocation the Go runtime would otherwise create, and
// CREATE_NEW_PROCESS_GROUP detaches the launcher from this process's console
// control events, so closing or killing the TUI cannot take the file manager
// down with it.
func applyDetached(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}
