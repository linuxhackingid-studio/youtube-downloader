//go:build windows

package tools

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// defaultExts is the PATHEXT fallback the Windows tests expect.
var defaultExts = []string{".com", ".exe", ".bat", ".cmd"}

// TestExecutableExtensionsWindows pins the PATHEXT parsing: entries are
// lowercased and trimmed, duplicates and empty entries are dropped, and
// an unset or blank PATHEXT falls back to the Windows default.
func TestExecutableExtensionsWindows(t *testing.T) {
	t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")
	if got := executableExtensions(); !slices.Equal(got, defaultExts) {
		t.Errorf("executableExtensions() = %v, want %v", got, defaultExts)
	}

	t.Setenv("PATHEXT", "")
	if got := executableExtensions(); !slices.Equal(got, defaultExts) {
		t.Errorf("executableExtensions() with blank PATHEXT = %v, want %v", got, defaultExts)
	}

	t.Setenv("PATHEXT", ".EXE;.exe;; .cmd ")
	if got, want := executableExtensions(), []string{".exe", ".cmd"}; !slices.Equal(got, want) {
		t.Errorf("executableExtensions() with duplicates = %v, want %v", got, want)
	}
}

// TestFallbackPatternsWindows pins the probe order and proves that unset
// environment variables are dropped instead of degrading into
// CWD-relative patterns.
func TestFallbackPatternsWindows(t *testing.T) {
	root := t.TempDir()
	local := filepath.Join(root, "local")
	roaming := filepath.Join(root, "roaming")
	profile := filepath.Join(root, "profile")
	programData := filepath.Join(root, "programdata")

	t.Setenv("LOCALAPPDATA", local)
	t.Setenv("APPDATA", roaming)
	t.Setenv("USERPROFILE", profile)
	t.Setenv("ProgramData", programData)

	want := []string{
		filepath.Join(local, "Microsoft", "WinGet", "Links"),
		filepath.Join(local, "Programs", "Python", "Python*", "Scripts"),
		filepath.Join(roaming, "Python", "Scripts"),
		filepath.Join(profile, "scoop", "shims"),
		filepath.Join(programData, "chocolatey", "bin"),
	}
	if got := fallbackPatterns(); !slices.Equal(got, want) {
		t.Errorf("fallbackPatterns() = %v, want %v", got, want)
	}

	t.Setenv("APPDATA", "")
	got := fallbackPatterns()
	if len(got) != len(want)-1 {
		t.Fatalf("fallbackPatterns() with a blank APPDATA = %v, want %d entries", got, len(want)-1)
	}
	for _, pattern := range got {
		if !filepath.IsAbs(pattern) {
			t.Errorf("fallbackPatterns() returned the relative pattern %q", pattern)
		}
	}
}

// TestProbeExecutableWindows covers PATHEXT-based detection: a .cmd file
// is found from its bare name, a file whose extension is not in PATHEXT
// is not executable, and a missing name is not found.
func TestProbeExecutableWindows(t *testing.T) {
	t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")

	dir := t.TempDir()
	tool := filepath.Join(dir, "faketool.cmd")
	if err := os.WriteFile(tool, []byte("@echo off\r\n"), 0o644); err != nil {
		t.Fatalf("write tool: %v", err)
	}

	got, ok := probeExecutable(dir, "faketool")
	if !ok {
		t.Fatalf("probeExecutable(%q, %q) = not found, want %q", dir, "faketool", tool)
	}
	if !strings.EqualFold(got, tool) {
		t.Errorf("probeExecutable(%q, %q) = %q, want %q", dir, "faketool", got, tool)
	}

	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write notes: %v", err)
	}
	if got, ok := probeExecutable(dir, "notes"); ok {
		t.Errorf("probeExecutable(%q, %q) = %q, true; want false", dir, "notes", got)
	}
	if got, ok := probeExecutable(dir, "nosuchtool"); ok {
		t.Errorf("probeExecutable(%q, %q) = %q, true; want false", dir, "nosuchtool", got)
	}

	// A directory that carries an executable extension is not a file.
	if err := os.Mkdir(filepath.Join(dir, "adir.cmd"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if got, ok := probeExecutable(dir, "adir"); ok {
		t.Errorf("probeExecutable on a directory = %q, true; want false", got)
	}
}

// TestLookPathUsesWinGetLinks proves the fallback chain works with no
// usable PATH: a tool that WinGet installed into its Links directory is
// still resolved.
func TestLookPathUsesWinGetLinks(t *testing.T) {
	const name = "lhi-faketool-winget"

	localAppData := t.TempDir()
	links := filepath.Join(localAppData, "Microsoft", "WinGet", "Links")
	if err := os.MkdirAll(links, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", links, err)
	}
	tool := filepath.Join(links, name+".cmd")
	if err := os.WriteFile(tool, []byte("@echo off\r\n"), 0o644); err != nil {
		t.Fatalf("write tool: %v", err)
	}

	t.Setenv("LOCALAPPDATA", localAppData)
	t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")
	t.Setenv("PATH", filepath.Join(localAppData, "no-such-path-entry"))

	got, err := lookPath(name)
	if err != nil {
		t.Fatalf("lookPath(%q) error = %v, want nil", name, err)
	}
	if !strings.EqualFold(got, tool) {
		t.Errorf("lookPath(%q) = %q, want %q", name, got, tool)
	}
}
