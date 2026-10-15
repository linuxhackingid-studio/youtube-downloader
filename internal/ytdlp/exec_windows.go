//go:build windows

package ytdlp

import "syscall"

// procAttr returns the process attributes used when spawning yt-dlp and
// ffmpeg: the console window is hidden and CREATE_NO_WINDOW (0x08000000)
// prevents a console from flashing while the TUI is running.
func procAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
