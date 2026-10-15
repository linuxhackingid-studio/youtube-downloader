//go:build windows

package tools

import "syscall"

// procAttr returns the process attributes used when spawning external
// commands: the console window is hidden and CREATE_NO_WINDOW
// (0x08000000) prevents a console from flashing in the TUI.
func procAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
