//go:build !windows && !unix

package platform

import "os/exec"

// applyDetached is a no-op on systems that have no process groups: the
// launcher is still started with exec.Cmd, it simply shares the environment
// of this process.
func applyDetached(*exec.Cmd) {}
