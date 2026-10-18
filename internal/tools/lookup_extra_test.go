package tools

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// mustMkdirAll creates dir or fails the test, so the directory tests can
// build their fake layouts without repeating error handling.
func mustMkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", dir, err)
	}
}

// TestExpandDirPattern covers the three shapes of a fallback pattern: a
// plain path is returned as-is, a wildcard is expanded by filepath.Glob in
// sorted order, and an unusable pattern (empty or malformed) yields nothing.
func TestExpandDirPattern(t *testing.T) {
	root := t.TempDir()
	mustMkdirAll(t, filepath.Join(root, "Python311", "Scripts"))
	mustMkdirAll(t, filepath.Join(root, "Python310", "Scripts"))

	t.Run("wildcard expands in sorted order", func(t *testing.T) {
		want := []string{
			filepath.Join(root, "Python310", "Scripts"),
			filepath.Join(root, "Python311", "Scripts"),
		}
		if got := expandDirPattern(filepath.Join(root, "Python*", "Scripts")); !slices.Equal(got, want) {
			t.Errorf("expandDirPattern() = %v, want %v", got, want)
		}
	})

	t.Run("plain path is returned unchanged", func(t *testing.T) {
		plain := filepath.Join(root, "plain")
		if got := expandDirPattern(plain); !slices.Equal(got, []string{plain}) {
			t.Errorf("expandDirPattern(%q) = %v, want the pattern itself", plain, got)
		}
	})

	t.Run("empty pattern yields nothing", func(t *testing.T) {
		if got := expandDirPattern(""); got != nil {
			t.Errorf("expandDirPattern(\"\") = %v, want nil", got)
		}
	})

	t.Run("malformed glob yields nothing", func(t *testing.T) {
		if got := expandDirPattern(filepath.Join(root, "[*")); got != nil {
			t.Errorf("expandDirPattern(bad pattern) = %v, want nil", got)
		}
	})

	t.Run("wildcard without matches yields nothing", func(t *testing.T) {
		if got := expandDirPattern(filepath.Join(root, "Rust*", "bin")); len(got) != 0 {
			t.Errorf("expandDirPattern(no matches) = %v, want empty", got)
		}
	})
}

// TestChildDir pins the guard that keeps an unset environment variable from
// turning into a CWD-relative pattern.
func TestChildDir(t *testing.T) {
	if got := childDir(""); got != "" {
		t.Errorf("childDir(\"\") = %q, want empty", got)
	}
	if got := childDir("", "a", "b"); got != "" {
		t.Errorf("childDir(\"\", a, b) = %q, want empty", got)
	}
	if got, want := childDir("base"), "base"; got != want {
		t.Errorf("childDir(\"base\") = %q, want %q", got, want)
	}
	if got, want := childDir("base", "a", "b"), filepath.Join("base", "a", "b"); got != want {
		t.Errorf("childDir(\"base\", a, b) = %q, want %q", got, want)
	}
}

// TestCompact pins that only non-empty patterns are kept, in order.
func TestCompact(t *testing.T) {
	if got := compact(); len(got) != 0 {
		t.Errorf("compact() = %v, want empty", got)
	}
	if got := compact("", "", ""); len(got) != 0 {
		t.Errorf("compact(blanks) = %v, want empty", got)
	}
	if got, want := compact("a", "", "b", ""), []string{"a", "b"}; !slices.Equal(got, want) {
		t.Errorf("compact(a, \"\", b, \"\") = %v, want %v", got, want)
	}
}

// TestProbeExecutableGuards pins the degenerate inputs: an empty directory
// or an empty name can never resolve to a path.
func TestProbeExecutableGuards(t *testing.T) {
	dir := t.TempDir()

	if got, ok := probeExecutable("", "name"); ok {
		t.Errorf("probeExecutable(\"\", name) = %q, true; want false", got)
	}
	if got, ok := probeExecutable(dir, ""); ok {
		t.Errorf("probeExecutable(dir, \"\") = %q, true; want false", got)
	}
	if got, ok := probeExecutable("", ""); ok {
		t.Errorf("probeExecutable(\"\", \"\") = %q, true; want false", got)
	}
}
