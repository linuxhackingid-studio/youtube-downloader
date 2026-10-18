//go:build unix

package tools

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestFallbackDirsUnixDedupeAndOrder pins the unix fallback list after
// filtering: $HOME/.local/bin is included when PATH does not cover it, the
// result is de-duplicated, and it keeps its priority position before the
// system directories.
func TestFallbackDirsUnixDedupeAndOrder(t *testing.T) {
	home := t.TempDir()
	localBin := filepath.Join(home, ".local", "bin")
	mustMkdirAll(t, localBin)
	t.Setenv("HOME", home)

	t.Run("local bin is included and ordered before system dirs", func(t *testing.T) {
		t.Setenv("PATH", filepath.Join(home, "no-such-path-entry"))

		got := fallbackDirs()
		if !slices.Contains(got, localBin) {
			t.Fatalf("fallbackDirs() = %v, want it to contain %q", got, localBin)
		}

		seen := make(map[string]struct{}, len(got))
		for _, dir := range got {
			if dir == "" {
				t.Error("fallbackDirs() returned an empty directory")
			}
			if _, dup := seen[dir]; dup {
				t.Errorf("fallbackDirs() returned the duplicate %q", dir)
			}
			seen[dir] = struct{}{}

			info, err := os.Stat(dir)
			if err != nil || !info.IsDir() {
				t.Errorf("fallbackDirs() returned %q, which is not an existing directory: %v", dir, err)
			}
		}

		if i, j := slices.Index(got, localBin), slices.Index(got, "/usr/bin"); i >= 0 && j >= 0 && i > j {
			t.Errorf("fallbackDirs() = %v, want %q before %q", got, localBin, "/usr/bin")
		}
	})

	t.Run("directories on PATH are dropped", func(t *testing.T) {
		t.Setenv("PATH", localBin)

		got := fallbackDirs()
		if slices.Contains(got, localBin) {
			t.Errorf("fallbackDirs() = %v, want %q excluded while it is on PATH", got, localBin)
		}
		if i := slices.Index(got, "/usr/bin"); i < 0 {
			t.Skipf("/usr/bin is not an existing directory here; got %v", got)
		}
	})
}
