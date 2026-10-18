package tools

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// lookPath resolves an executable by name. It defers to exec.LookPath
// first and then probes the well-known per-OS install directories
// returned by fallbackDirs, so a tool installed by pipx, Homebrew,
// WinGet, Scoop or Chocolatey is found even when the user's shell PATH
// does not expose it.
//
// A tool that is only reachable as ./name is refused: the exec.ErrDot
// error from exec.LookPath is returned unchanged, keeping exec's own
// strictness about relative executables. When nothing matches,
// exec.ErrNotFound is returned.
func lookPath(name string) (string, error) {
	if name == "" {
		return "", exec.ErrNotFound
	}

	path, err := exec.LookPath(name)
	switch {
	case err == nil:
		return path, nil
	case errors.Is(err, exec.ErrDot):
		return "", err
	}

	for _, dir := range fallbackDirs() {
		if found, ok := probeExecutable(dir, name); ok {
			return found, nil
		}
	}
	return "", exec.ErrNotFound
}

// fallbackDirs returns the well-known directories to probe for
// executables when PATH does not expose them, in priority order. Only
// existing directories are returned, duplicates are dropped both within
// the list and against PATH (which exec.LookPath has already searched),
// and patterns that fail to glob are skipped silently.
func fallbackDirs() []string {
	seen := make(map[string]struct{}, 16)
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			continue
		}
		seen[dirKey(filepath.Clean(dir))] = struct{}{}
	}

	patterns := fallbackPatterns()
	dirs := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		for _, dir := range expandDirPattern(pattern) {
			key := dirKey(filepath.Clean(dir))
			if _, dup := seen[key]; dup {
				continue
			}
			if !isDir(dir) {
				continue
			}
			seen[key] = struct{}{}
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

// expandDirPattern returns the concrete directories described by one
// fallback pattern. Patterns containing "*" are expanded with
// filepath.Glob; malformed patterns and glob failures yield no matches.
func expandDirPattern(pattern string) []string {
	if pattern == "" {
		return nil
	}
	if !strings.Contains(pattern, "*") {
		return []string{pattern}
	}

	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	return matches
}

// probeExecutable reports whether an executable named name exists inside
// dir and returns its full path. The per-OS executable extensions are
// tried after the bare name.
func probeExecutable(dir, name string) (string, bool) {
	if dir == "" || name == "" {
		return "", false
	}

	if path := filepath.Join(dir, name); isExecutable(path) {
		return path, true
	}
	for _, ext := range executableExtensions() {
		path := filepath.Join(dir, name+ext)
		if isExecutable(path) {
			return path, true
		}
	}
	return "", false
}

// isDir reports whether path names an existing directory.
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// childDir joins elems onto base, returning "" when base is empty so an
// unset environment variable never produces a CWD-relative pattern.
func childDir(base string, elems ...string) string {
	if base == "" {
		return ""
	}
	return filepath.Join(base, filepath.Join(elems...))
}

// compact returns the non-empty patterns in their original order.
func compact(patterns ...string) []string {
	out := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		if pattern != "" {
			out = append(out, pattern)
		}
	}
	return out
}
