//go:build windows

package tools

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// defaultPathExt is the extension list Windows uses when PATHEXT is
// unset, matching the documented system default.
const defaultPathExt = ".COM;.EXE;.BAT;.CMD"

// fallbackPatterns lists the well-known Windows install locations that
// package managers use without necessarily updating the user's PATH:
// WinGet shims, per-user Python script directories (pip) and Scoop
// shims, plus the machine-wide Chocolatey bin directory. The Python
// entry is a glob because the interpreter directory carries a version
// suffix (Python310, Python311, ...).
func fallbackPatterns() []string {
	localAppData := os.Getenv("LOCALAPPDATA")
	appData := os.Getenv("APPDATA")
	profile := os.Getenv("USERPROFILE")
	if profile == "" {
		profile, _ = os.UserHomeDir()
	}
	programData := os.Getenv("ProgramData")

	return compact(
		childDir(localAppData, "Microsoft", "WinGet", "Links"),
		childDir(localAppData, "Programs", "Python", "Python*", "Scripts"),
		childDir(appData, "Python", "Scripts"),
		childDir(profile, "scoop", "shims"),
		childDir(programData, "chocolatey", "bin"),
	)
}

// executableExtensions returns the executable extensions to try, in
// PATHEXT order and lowercased so that comparisons against names on
// disk are case-insensitive. It falls back to the Windows default when
// PATHEXT is unset or blank.
func executableExtensions() []string {
	raw := os.Getenv("PATHEXT")
	if strings.TrimSpace(raw) == "" {
		raw = defaultPathExt
	}

	exts := make([]string, 0, 4)
	for _, ext := range strings.Split(raw, ";") {
		ext = strings.ToLower(strings.TrimSpace(ext))
		if ext == "" || slices.Contains(exts, ext) {
			continue
		}
		exts = append(exts, ext)
	}
	return exts
}

// isExecutable reports whether path is a regular file whose extension
// Windows treats as executable.
func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}

	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" {
		return false
	}
	return slices.Contains(executableExtensions(), ext)
}

// dirKey normalizes a directory path for comparison. Windows paths
// compare case-insensitively.
func dirKey(path string) string { return strings.ToLower(path) }
