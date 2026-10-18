//go:build !windows && !unix

package tools

import (
	"os"
	"path/filepath"
	"strings"
)

// fallbackPatterns lists the extra directories to probe when PATH does not
// expose a tool. The platforms in this build (js/wasm, plan9, wasip1) have
// no documented package-manager install layout, so the list is empty and
// lookPath relies on PATH alone.
func fallbackPatterns() []string { return nil }

// executableExtensions is empty here: executables are not recognized by a
// file name suffix on these platforms.
func executableExtensions() []string { return nil }

// isExecutable reports whether path is a regular file. These platforms
// expose no portable executable bit, so a regular file is the honest
// approximation available.
func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// dirKey normalizes a directory path for comparison. Without per-OS
// semantics to lean on, the clean path is lowercased so two spellings of
// the same directory collapse into one entry.
func dirKey(path string) string { return strings.ToLower(filepath.Clean(path)) }
