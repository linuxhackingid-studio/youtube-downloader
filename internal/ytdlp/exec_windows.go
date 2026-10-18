//go:build windows

package ytdlp

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
)

// procAttr returns the process attributes used when spawning yt-dlp and
// ffmpeg: the console window is hidden and CREATE_NO_WINDOW (0x08000000)
// prevents a console from flashing while the TUI is running.
func procAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}

// killTree force-terminates pid and every process below it with
// taskkill /T /F, the only way Windows walks and kills a process tree. A
// non-positive pid is a no-op.
//
// Killing stays best effort, matching the package's "Kill never fails"
// contract: taskkill that ran but could not terminate anything (yt-dlp is
// usually already gone, and taskkill reports that as an error) is not a
// failure. Only a taskkill that could not even be launched - a missing or
// unusable binary - returns an error, because then the caller's only
// remaining fallback is canceling the run context.
func killTree(pid int) error {
	if pid <= 0 {
		return nil
	}

	cmd := exec.Command(taskkillBin(), "/T", "/F", "/PID", strconv.Itoa(pid))
	cmd.SysProcAttr = procAttr()

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil
		}
		return err
	}
	return nil
}

// taskkillBin resolves the taskkill.exe used to terminate a download's
// process tree. The fully qualified path under %SystemRoot% is preferred:
// a PATH lookup can be missing or poisoned in stripped environments. When
// the file does not exist, the bare name is returned so exec falls back to
// a PATH search, which itself falls back to canceling the context.
func taskkillBin() string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	bin := filepath.Join(root, "System32", "taskkill.exe")
	if _, err := os.Stat(bin); err != nil {
		return "taskkill"
	}
	return bin
}
