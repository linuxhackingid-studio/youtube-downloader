package platform

import (
	"testing"
)

// TestOpenFolderUnknownGOOSStillLaunches pins the fallback mapping at the
// openFolder level: a GOOS without a dedicated branch is opened with
// xdg-open rather than refused, and the empty-path guard runs before any
// launcher is chosen.
func TestOpenFolderUnknownGOOSStillLaunches(t *testing.T) {
	calls := stubLauncher(t, nil)

	dir := t.TempDir()
	if err := openFolder(dir, "freebsd"); err != nil {
		t.Fatalf("openFolder(%q, freebsd) = %v, want nil", dir, err)
	}
	if len(*calls) != 1 {
		t.Fatalf("launcher called %d times, want exactly 1", len(*calls))
	}
	got := (*calls)[0]
	if got.name != "xdg-open" {
		t.Errorf("launcher name = %q, want %q for an unmapped GOOS", got.name, "xdg-open")
	}
	if len(got.args) != 1 || got.args[0] != dir {
		t.Errorf("launcher args = %#v, want exactly [%q]", got.args, dir)
	}

	if err := openFolder(" \t ", "freebsd"); err == nil {
		t.Error("openFolder(blank, freebsd) = nil, want an empty-path error")
	}
	if len(*calls) != 1 {
		t.Errorf("launcher called %d times after a blank path, want still 1", len(*calls))
	}
}
