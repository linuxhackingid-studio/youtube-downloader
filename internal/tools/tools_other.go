//go:build !windows

package tools

import "syscall"

// procAttr is a no-op on non-Windows platforms.
func procAttr() *syscall.SysProcAttr { return nil }
