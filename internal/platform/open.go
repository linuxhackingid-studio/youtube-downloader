package platform

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// OpenFolder opens dir in the operating system's file manager. The launcher
// is started detached and is never waited for, so opening the download folder
// cannot block the TUI.
//
// The returned error covers only the launch itself: an empty path or a
// launcher that could not be started. Whether dir exists is left to the file
// manager, which reports it in its own window (Windows explorer.exe starts
// successfully even for a missing directory).
func OpenFolder(dir string) error {
	return openFolder(dir, runtime.GOOS)
}

// openFolder is the testable core of OpenFolder: it maps the GOOS and starts
// the launcher through the launchFn seam.
func openFolder(dir, goos string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return errors.New("platform: open folder: empty path")
	}

	name, args := openCommand(goos, dir)
	if name == "" {
		return fmt.Errorf("platform: open folder: no file manager known for %s", goos)
	}
	if err := launchFn(name, args); err != nil {
		return fmt.Errorf("platform: open folder %q: %w", dir, err)
	}
	return nil
}

// openCommand maps a GOOS to the file-manager binary that opens dir and the
// arguments to invoke it with. dir is always passed as a single argument, so
// paths containing spaces need no quoting on any platform.
func openCommand(goos, dir string) (name string, args []string) {
	switch goos {
	case "windows":
		return "explorer.exe", []string{dir}
	case "darwin":
		return "open", []string{dir}
	default:
		return "xdg-open", []string{dir}
	}
}

// launchFn is the process launcher OpenFolder uses. It is a variable so tests
// can verify the wiring (and force a launch failure) without opening a real
// file-manager window on the machine running the suite.
var launchFn = launchDetached

// launchDetached starts name with args and returns as soon as the process is
// created. It deliberately never calls Wait: the launcher outlives the TUI,
// and the TUI must not stall while the file manager starts up. It is called
// from the Bubble Tea event loop only, so the resulting unreaped child is
// bounded by the process lifetime.
func launchDetached(name string, args []string) error {
	cmd := exec.Command(name, args...)
	applyDetached(cmd)
	return cmd.Start()
}
