// Tests for the platform helpers: the friendly labels, the per-OS launcher
// mapping, and the launch-error contract of OpenFolder. No test opens a real
// file-manager window: the launcher is either stubbed through the launchFn
// seam or asked to start a binary that cannot exist.

package platform

import (
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLabelContainsGOOSAndGOARCH(t *testing.T) {
	label := Label()
	if label == "" {
		t.Fatal("Label() is empty")
	}
	if !strings.Contains(label, runtime.GOARCH) {
		t.Errorf("Label() = %q, want it to contain the architecture %q", label, runtime.GOARCH)
	}
	if !strings.Contains(label, Name()) {
		t.Errorf("Label() = %q, want it to contain Name() = %q", label, Name())
	}
	if got := Label(); got != labelFor(runtime.GOOS, runtime.GOARCH) {
		t.Errorf("Label() = %q, want %q", got, labelFor(runtime.GOOS, runtime.GOARCH))
	}
}

func TestNameFor(t *testing.T) {
	tests := []struct {
		goos string
		want string
	}{
		{"windows", "Windows"},
		{"darwin", "macOS"},
		{"linux", "Linux"},
		{"plan9", "plan9"}, // unknown systems pass through unchanged
	}
	for _, tt := range tests {
		if got := nameFor(tt.goos); got != tt.want {
			t.Errorf("nameFor(%q) = %q, want %q", tt.goos, got, tt.want)
		}
	}
}

func TestLabelFor(t *testing.T) {
	tests := []struct {
		goos, goarch, want string
	}{
		{"windows", "amd64", "Windows (amd64)"},
		{"darwin", "arm64", "macOS (arm64)"},
		{"linux", "amd64", "Linux (amd64)"},
	}
	for _, tt := range tests {
		if got := labelFor(tt.goos, tt.goarch); got != tt.want {
			t.Errorf("labelFor(%q, %q) = %q, want %q", tt.goos, tt.goarch, got, tt.want)
		}
	}
}

func TestOpenCommand(t *testing.T) {
	// A path with spaces and no shell quoting: the launcher must receive it
	// as one argument for the open to work.
	dir := filepath.Join("C:", "Users", "Some User", "Downloads", "YouTube")

	tests := []struct {
		goos     string
		wantName string
	}{
		{"windows", "explorer.exe"},
		{"darwin", "open"},
		{"linux", "xdg-open"},
		{"freebsd", "xdg-open"}, // default branch
	}
	for _, tt := range tests {
		name, args := openCommand(tt.goos, dir)
		if name != tt.wantName {
			t.Errorf("openCommand(%q) name = %q, want %q", tt.goos, name, tt.wantName)
		}
		if len(args) != 1 || args[0] != dir {
			t.Errorf("openCommand(%q) args = %#v, want exactly one argument %q", tt.goos, args, dir)
		}
	}
}

// launcherCall records one stubbed launch.
type launcherCall struct {
	name string
	args []string
}

// stubLauncher replaces launchFn for the duration of the test and returns the
// recorded calls. err is returned to the caller of the launcher, so a failing
// launch can be simulated without depending on the host's installed binaries.
func stubLauncher(t *testing.T, err error) *[]launcherCall {
	t.Helper()

	calls := new([]launcherCall)
	restore := launchFn
	launchFn = func(name string, args []string) error {
		*calls = append(*calls, launcherCall{name: name, args: args})
		return err
	}
	t.Cleanup(func() { launchFn = restore })
	return calls
}

func TestOpenFolderInvokesHostLauncher(t *testing.T) {
	dir := t.TempDir()
	calls := stubLauncher(t, nil)

	if err := OpenFolder(dir); err != nil {
		t.Fatalf("OpenFolder(%q) = %v, want nil", dir, err)
	}

	wantName, wantArgs := openCommand(runtime.GOOS, dir)
	if len(*calls) != 1 {
		t.Fatalf("launcher called %d times, want exactly 1", len(*calls))
	}
	got := (*calls)[0]
	if got.name != wantName {
		t.Errorf("launcher name = %q, want %q", got.name, wantName)
	}
	if len(got.args) != 1 || got.args[0] != wantArgs[0] {
		t.Errorf("launcher args = %#v, want %#v", got.args, wantArgs)
	}
}

// TestOpenFolderNonexistentDirPassedThrough pins the delegation contract:
// OpenFolder never stats the directory, it hands the trimmed path to the
// launcher and lets the file manager decide. On Windows explorer.exe starts
// successfully even when the directory is missing, so asserting a launch
// error here would be wrong on this machine - the assertion that holds
// everywhere is that the path reaches the launcher unchanged.
func TestOpenFolderNonexistentDirPassedThrough(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "definitely-missing")
	calls := stubLauncher(t, nil)

	if err := OpenFolder(dir); err != nil {
		t.Fatalf("OpenFolder(missing %q) = %v, want nil (the file manager reports the missing directory)", dir, err)
	}
	if len(*calls) != 1 || (*calls)[0].args[0] != dir {
		t.Fatalf("launcher calls = %#v, want the missing path %q passed through", *calls, dir)
	}
}

func TestOpenFolderEmptyPath(t *testing.T) {
	calls := stubLauncher(t, nil)

	err := OpenFolder("   ")
	if err == nil {
		t.Fatal("OpenFolder(\"   \") = nil, want an error for an empty path")
	}
	if len(*calls) != 0 {
		t.Errorf("launcher called %d times for an empty path, want 0", len(*calls))
	}
}

func TestOpenFolderPropagatesLaunchError(t *testing.T) {
	sentinel := errors.New("launcher exploded")
	stubLauncher(t, sentinel)

	err := OpenFolder(t.TempDir())
	if !errors.Is(err, sentinel) {
		t.Fatalf("OpenFolder() = %v, want it to wrap %v", err, sentinel)
	}
}

// TestLaunchDetachedMissingBinary exercises the real exec path, including the
// platform-specific detachment flags: starting a binary that cannot exist is
// the one launch failure that is deterministic on every host, and it never
// opens a window.
func TestLaunchDetachedMissingBinary(t *testing.T) {
	err := launchDetached("platform-test-no-such-binary-xyz", []string{"."})
	if err == nil {
		t.Fatal("launchDetached(no such binary) = nil, want a launch error")
	}
}
