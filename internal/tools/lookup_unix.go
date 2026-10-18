//go:build unix

package tools

import (
	"os"
	"path/filepath"
	"runtime"
)

// fallbackPatterns lists the well-known Unix install directories to
// probe when PATH does not expose a tool. On macOS the Homebrew prefixes
// come first, because Apple silicon installs into /opt/homebrew by
// default; $HOME/.local/bin follows, since that is where pipx and
// pip install --user place executables.
func fallbackPatterns() []string {
	patterns := make([]string, 0, 8)
	if runtime.GOOS == "darwin" {
		patterns = append(patterns, "/opt/homebrew/bin", "/usr/local/bin")
	}
	patterns = append(patterns,
		localBinDir(),
		"/usr/local/bin",
		"/usr/bin",
		"/bin",
		"/snap/bin",
		"/var/lib/flatpak/exports/bin",
	)
	return compact(patterns...)
}

// localBinDir returns $HOME/.local/bin, or "" when HOME is unavailable.
func localBinDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".local", "bin")
}

// executableExtensions is empty on Unix: executables are recognized by
// their permission bits, not by a file name suffix.
func executableExtensions() []string { return nil }

// isExecutable reports whether path is a regular file with at least one
// executable permission bit set.
func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	return info.Mode().Perm()&0o111 != 0
}

// dirKey returns path unchanged. Unix file names are case-sensitive, so
// distinct spellings may legitimately be distinct directories.
func dirKey(path string) string { return path }
