//go:build unix

package tools

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

// TestProbeExecutableUnix covers permission-bit based detection: a file
// with an executable bit is found by its bare name, the same file after
// chmod 0o644 is not, and a missing name is not.
func TestProbeExecutableUnix(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tool := filepath.Join(dir, "yt-dlp")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write tool: %v", err)
	}

	got, ok := probeExecutable(dir, "yt-dlp")
	if !ok {
		t.Fatalf("probeExecutable(%q, %q) = not found, want %q", dir, "yt-dlp", tool)
	}
	if got != tool {
		t.Errorf("probeExecutable(%q, %q) = %q, want %q", dir, "yt-dlp", got, tool)
	}

	if err := os.Chmod(tool, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	if got, ok := probeExecutable(dir, "yt-dlp"); ok {
		t.Errorf("probeExecutable on a 0o644 file = %q, true; want false", got)
	}
	if got, ok := probeExecutable(dir, "ffmpeg"); ok {
		t.Errorf("probeExecutable(%q, %q) = %q, true; want false", dir, "ffmpeg", got)
	}

	// A directory that carries the tool's name is not executable.
	if err := os.Mkdir(filepath.Join(dir, "somedir"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if got, ok := probeExecutable(dir, "somedir"); ok {
		t.Errorf("probeExecutable on a directory = %q, true; want false", got)
	}
}

// TestFallbackPatternsUnix pins the probe order: the Homebrew prefixes
// first on macOS, then $HOME/.local/bin, then the system directories.
func TestFallbackPatternsUnix(t *testing.T) {
	t.Parallel()

	patterns := fallbackPatterns()

	if runtime.GOOS == "darwin" {
		if len(patterns) < 2 || patterns[0] != "/opt/homebrew/bin" || patterns[1] != "/usr/local/bin" {
			t.Errorf("darwin fallbackPatterns() = %v; want /opt/homebrew/bin then /usr/local/bin first", patterns)
		}
	}

	wantOrder := []string{
		"/usr/local/bin",
		"/usr/bin",
		"/bin",
		"/snap/bin",
		"/var/lib/flatpak/exports/bin",
	}
	next := 0
	for _, pattern := range patterns {
		if next < len(wantOrder) && pattern == wantOrder[next] {
			next++
		}
	}
	if next != len(wantOrder) {
		t.Errorf("fallbackPatterns() = %v; missing or misordered %q onward", patterns, wantOrder[next])
	}

	if home, err := os.UserHomeDir(); err == nil && home != "" {
		want := filepath.Join(home, ".local", "bin")
		if i, j := slices.Index(patterns, want), slices.Index(patterns, "/usr/bin"); i < 0 {
			t.Errorf("fallbackPatterns() = %v; want %q", patterns, want)
		} else if j >= 0 && i > j {
			t.Errorf("fallbackPatterns() = %v; want %q before %q", patterns, want, "/usr/bin")
		}
	}
}

// TestLookPathPrefersPath pins the precedence rule: a tool reachable
// through PATH wins over the same name in a fallback directory.
// Skipped when the environment has no sh to stand in as the tool.
func TestLookPathPrefersPath(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skipf("sh is not on PATH: %v", err)
	}

	dir := t.TempDir()
	tool := filepath.Join(dir, "sh")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write tool: %v", err)
	}

	prependPATH(t, dir)

	got, err := lookPath("sh")
	if err != nil {
		t.Fatalf("lookPath(%q) error = %v, want nil", "sh", err)
	}
	if got != tool {
		t.Errorf("lookPath(%q) = %q, want the PATH hit %q", "sh", got, tool)
	}
}

// TestLookPathUsesHomeLocalBin proves the fallback chain works with no
// PATH at all: a tool in $HOME/.local/bin, where pipx and pip install
// --user place executables, is still resolved.
func TestLookPathUsesHomeLocalBin(t *testing.T) {
	const name = "lhi-faketool-local-bin"

	home := t.TempDir()
	binDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", binDir, err)
	}
	tool := filepath.Join(binDir, name)
	if err := os.WriteFile(tool, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write tool: %v", err)
	}

	t.Setenv("HOME", home)
	t.Setenv("PATH", "")

	got, err := lookPath(name)
	if err != nil {
		t.Fatalf("lookPath(%q) error = %v, want nil", name, err)
	}
	if got != tool {
		t.Errorf("lookPath(%q) = %q, want %q", name, got, tool)
	}
}

// prependPATH puts dir at the front of PATH for the duration of the test.
func prependPATH(t *testing.T, dir string) {
	t.Helper()

	current, ok := os.LookupEnv("PATH")
	if !ok {
		t.Skip("PATH is not set")
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+current)
}
