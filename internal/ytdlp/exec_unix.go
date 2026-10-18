//go:build unix

package ytdlp

import (
	"errors"
	"syscall"
)

// procAttr returns the process attributes used when spawning yt-dlp and
// ffmpeg. Setpgid puts every child into its own process group whose group
// id equals the child's pid (Pgid stays zero), which is what lets killTree
// take down a whole download - yt-dlp plus any ffmpeg it spawned - with a
// single signal.
func procAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// killTree force-kills pid together with every process in its process
// group, which is where yt-dlp's ffmpeg children live. A non-positive pid
// is a no-op: kill(-0, sig) would signal the caller's own group. A group
// that is already gone reports ESRCH, which counts as success because the
// goal - the tree is dead - is met.
//
// SIGKILL is deliberate. It matches the force semantics of the Windows
// path (taskkill /T /F), Python's default SIGTERM disposition runs no
// cleanup handler anyway, and cancellation must stay snappy rather than
// wait out a graceful shutdown window.
func killTree(pid int) error {
	if pid <= 0 {
		return nil
	}
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	}
	return nil
}
