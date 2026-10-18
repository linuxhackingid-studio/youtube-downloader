//go:build !windows && !unix

package ytdlp

import (
	"errors"
	"os"
	"syscall"
)

// procAttr returns no special process attributes on platforms that are
// neither Windows nor Unix-like (plan9, js/wasm).
func procAttr() *syscall.SysProcAttr {
	return nil
}

// killTree force-terminates pid. These platforms offer no portable
// process-group handle, so only the direct process is signaled; canceling
// the run context remains the fallback for any children it may have
// spawned. A non-positive pid is a no-op, and os.ErrProcessDone means the
// process is already gone, which counts as success.
func killTree(pid int) error {
	if pid <= 0 {
		return nil
	}

	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := p.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}
