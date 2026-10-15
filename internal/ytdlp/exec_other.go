//go:build !windows

package ytdlp

import "syscall"

// procAttr returns no special process attributes on non-Windows
// platforms.
func procAttr() *syscall.SysProcAttr {
	return nil
}
