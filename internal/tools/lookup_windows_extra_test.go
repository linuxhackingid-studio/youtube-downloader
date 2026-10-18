//go:build windows

package tools

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// windowsFallbackRoot builds one fake Windows install layout under a fresh
// temp root and returns the root together with the directories of the
// fallback list.
type windowsFallbackRoot struct {
	root          string
	winGet        string
	python310     string
	python311     string
	roamingPython string
	scoop         string
	chocolatey    string
}

// newWindowsFallbackRoot creates the temp root but no directories; callers
// create the subset they need.
func newWindowsFallbackRoot(t *testing.T) windowsFallbackRoot {
	t.Helper()

	root := t.TempDir()
	local := filepath.Join(root, "local")
	roaming := filepath.Join(root, "roaming")
	profile := filepath.Join(root, "profile")
	programData := filepath.Join(root, "programdata")

	t.Setenv("LOCALAPPDATA", local)
	t.Setenv("APPDATA", roaming)
	t.Setenv("USERPROFILE", profile)
	t.Setenv("ProgramData", programData)

	return windowsFallbackRoot{
		root:          root,
		winGet:        filepath.Join(local, "Microsoft", "WinGet", "Links"),
		python310:     filepath.Join(local, "Programs", "Python", "Python310", "Scripts"),
		python311:     filepath.Join(local, "Programs", "Python", "Python311", "Scripts"),
		roamingPython: filepath.Join(roaming, "Python", "Scripts"),
		scoop:         filepath.Join(profile, "scoop", "shims"),
		chocolatey:    filepath.Join(programData, "chocolatey", "bin"),
	}
}

// TestFallbackDirsWindowsOrderAndDedupe pins the concrete probe order, the
// glob ordering of the version-suffixed Python directories, the exclusion
// of directories PATH already covers (including repeated and empty PATH
// elements), and the existence filter.
func TestFallbackDirsWindowsOrderAndDedupe(t *testing.T) {
	layout := newWindowsFallbackRoot(t)
	for _, dir := range []string{
		layout.winGet, layout.python310, layout.python311,
		layout.roamingPython, layout.scoop, layout.chocolatey,
	} {
		mustMkdirAll(t, dir)
	}

	t.Run("full layout in probe order", func(t *testing.T) {
		// The trailing separator adds an empty PATH element, which is
		// skipped; chocolatey is on PATH and must be excluded.
		t.Setenv("PATH", layout.chocolatey+string(os.PathListSeparator))

		want := []string{
			layout.winGet,
			layout.python310,
			layout.python311,
			layout.roamingPython,
			layout.scoop,
		}
		if got := fallbackDirs(); !slices.Equal(got, want) {
			t.Errorf("fallbackDirs() = %v, want %v", got, want)
		}
	})

	t.Run("directories on PATH are dropped", func(t *testing.T) {
		t.Setenv("PATH", layout.winGet+string(os.PathListSeparator)+layout.winGet)

		want := []string{
			layout.python310,
			layout.python311,
			layout.roamingPython,
			layout.scoop,
			layout.chocolatey,
		}
		if got := fallbackDirs(); !slices.Equal(got, want) {
			t.Errorf("fallbackDirs() = %v, want %v", got, want)
		}
	})

	t.Run("missing directories are skipped", func(t *testing.T) {
		layout := newWindowsFallbackRoot(t)
		mustMkdirAll(t, layout.chocolatey)
		t.Setenv("PATH", filepath.Join(layout.root, "empty-path-entry"))

		if got, want := fallbackDirs(), []string{layout.chocolatey}; !slices.Equal(got, want) {
			t.Errorf("fallbackDirs() = %v, want only %v", got, want)
		}
	})
}

// TestFallbackPatternsWithoutUserProfile covers the os.UserHomeDir fallback
// path: with USERPROFILE empty the scoop entry is dropped instead of
// degrading into a relative pattern.
func TestFallbackPatternsWithoutUserProfile(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LOCALAPPDATA", filepath.Join(root, "local"))
	t.Setenv("APPDATA", filepath.Join(root, "roaming"))
	t.Setenv("USERPROFILE", "")
	t.Setenv("ProgramData", filepath.Join(root, "programdata"))

	patterns := fallbackPatterns()
	if len(patterns) != 4 {
		t.Errorf("fallbackPatterns() = %v, want 4 entries with no user profile", patterns)
	}
	for _, pattern := range patterns {
		if !filepath.IsAbs(pattern) {
			t.Errorf("fallbackPatterns() returned the relative pattern %q", pattern)
		}
		if strings.Contains(pattern, "scoop") {
			t.Errorf("fallbackPatterns() kept a scoop pattern without a user profile: %q", pattern)
		}
	}
}

// TestIsExecutableWindows pins the extension rules: a file without an
// extension is not executable, an extension missing from PATHEXT is not
// executable, and matching is case-insensitive.
func TestIsExecutableWindows(t *testing.T) {
	t.Setenv("PATHEXT", ".CMD")

	dir := t.TempDir()
	files := map[string]bool{
		"no-extension": false,
		"tool.exe":     false, // .exe is not in this PATHEXT
		"tool.cmd":     true,
		"TOOL.CMD":     true,
	}
	for name, want := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("@echo off\r\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(%q): %v", path, err)
		}
		if got := isExecutable(path); got != want {
			t.Errorf("isExecutable(%q) = %v, want %v", name, got, want)
		}
	}

	if err := os.MkdirAll(filepath.Join(dir, "subdir.cmd"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if isExecutable(filepath.Join(dir, "subdir.cmd")) {
		t.Error("isExecutable(directory) = true, want false")
	}
}

// TestProbeExecutableExactNameWindows pins the already-suffixed lookup: a
// name that already carries an executable extension is found through the
// exact path, before the extension loop could append a second suffix.
func TestProbeExecutableExactNameWindows(t *testing.T) {
	dir := t.TempDir()
	want := filepath.Join(dir, "yt-dlp.exe")
	if err := os.WriteFile(want, []byte("stub"), 0o755); err != nil {
		t.Fatalf("WriteFile(%q): %v", want, err)
	}

	got, ok := probeExecutable(dir, "yt-dlp.exe")
	if !ok {
		t.Fatalf("probeExecutable(%q, %q) = not found, want %q", dir, "yt-dlp.exe", want)
	}
	if got != want {
		t.Errorf("probeExecutable() = %q, want the exact file %q", got, want)
	}
}

// TestToolsProcAttrWindows pins the spawn attributes shared by every tool
// probe: hidden window and CREATE_NO_WINDOW (0x08000000).
func TestToolsProcAttrWindows(t *testing.T) {
	attr := procAttr()
	if attr == nil {
		t.Fatal("procAttr() = nil, want process attributes")
	}
	if !attr.HideWindow {
		t.Error("procAttr().HideWindow = false, want true")
	}
	if want := uint32(0x08000000); attr.CreationFlags != want {
		t.Errorf("procAttr().CreationFlags = %#x, want %#x", attr.CreationFlags, want)
	}
}
