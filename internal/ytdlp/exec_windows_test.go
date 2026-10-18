//go:build windows

package ytdlp

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Windows kill waits. taskkill is fast, so these bounds only guard against
// hangs.
const (
	winKillGrace = 10 * time.Second
	winKillPoll  = 50 * time.Millisecond
)

// TestProcAttr pins the spawn attributes: the console window is hidden and
// CREATE_NO_WINDOW (0x08000000) prevents a console flash while the TUI runs.
func TestProcAttr(t *testing.T) {
	attr := procAttr()
	if attr == nil {
		t.Fatal("procAttr() = nil, want Win32 process attributes")
	}
	if !attr.HideWindow {
		t.Error("procAttr().HideWindow = false, want true")
	}
	if attr.CreationFlags != 0x08000000 {
		t.Errorf("procAttr().CreationFlags = %#x, want CREATE_NO_WINDOW (0x08000000)", attr.CreationFlags)
	}
}

// TestKillTreeNonPositivePidWindows pins the no-op guard: pid 0 must never
// be passed to taskkill.
func TestKillTreeNonPositivePidWindows(t *testing.T) {
	for _, pid := range []int{0, -1, -12345} {
		if err := killTree(pid); err != nil {
			t.Errorf("killTree(%d) = %v, want nil", pid, err)
		}
	}
}

// TestTaskkillBin pins the executable resolution order: the fully qualified
// binary under SystemRoot when it exists, the bare name otherwise.
func TestTaskkillBin(t *testing.T) {
	t.Run("fully qualified when the file exists", func(t *testing.T) {
		root := t.TempDir()
		dir := filepath.Join(root, "System32")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create System32: %v", err)
		}
		want := filepath.Join(dir, "taskkill.exe")
		if err := os.WriteFile(want, []byte("stub"), 0o644); err != nil {
			t.Fatalf("write stub taskkill: %v", err)
		}

		t.Setenv("SystemRoot", root)
		if got := taskkillBin(); got != want {
			t.Errorf("taskkillBin() = %q, want %q", got, want)
		}
	})

	t.Run("bare name when the file is missing", func(t *testing.T) {
		t.Setenv("SystemRoot", t.TempDir())
		if got := taskkillBin(); got != "taskkill" {
			t.Errorf("taskkillBin() = %q, want the bare name %q for a PATH fallback", got, "taskkill")
		}
	})

	t.Run("builtin root when SystemRoot is unset", func(t *testing.T) {
		t.Setenv("SystemRoot", "")

		fallback := filepath.Join(`C:\Windows`, "System32", "taskkill.exe")
		got := taskkillBin()
		if _, err := os.Stat(fallback); err == nil {
			if got != fallback {
				t.Errorf("taskkillBin() = %q, want the builtin fallback %q", got, fallback)
			}
			return
		}
		if got != "taskkill" {
			t.Errorf("taskkillBin() = %q, want the bare name when %q does not exist", got, fallback)
		}
	})
}

// TestKillTreeOnGoneProcess pins the documented best-effort contract: a
// taskkill that runs but finds nothing (exit status 128 for an unknown pid)
// is not a kill failure.
func TestKillTreeOnGoneProcess(t *testing.T) {
	// This pid is far above any live process, so taskkill reports "not
	// found" without touching anything real.
	const gonePid = 1 << 30

	if err := killTree(gonePid); err != nil {
		t.Errorf("killTree(%d) = %v, want nil for a process that is already gone", gonePid, err)
	}
}

// TestKillTreeReportsUnlaunchableTaskkill pins the one real failure mode:
// when taskkill itself cannot be launched, the caller must fall back to
// canceling the run context.
func TestKillTreeReportsUnlaunchableTaskkill(t *testing.T) {
	t.Setenv("SystemRoot", t.TempDir()) // no System32\taskkill.exe
	t.Setenv("PATH", t.TempDir())       // and no taskkill on PATH either

	err := killTree(1 << 30)
	if err == nil {
		t.Fatal("killTree() = nil, want a launch failure when taskkill is unavailable")
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		t.Errorf("killTree() error = %v, want a launch failure rather than an exit status", err)
	}
}

// TestKillTreeTerminatesProcessTree starts a cmd.exe whose child is a
// uniquely named copy of ping.exe, waits for the grandchild to appear, then
// verifies that killTree removes the whole tree - the taskkill /T /F
// contract that keeps ffmpeg from surviving a canceled yt-dlp.
func TestKillTreeTerminatesProcessTree(t *testing.T) {
	pingSrc := stagedPingSource(t)

	dir := t.TempDir()
	name := fmt.Sprintf("ytkill%d.exe", os.Getpid())
	copyExecutable(t, pingSrc, filepath.Join(dir, name))

	// cmd runs the child from its working directory; the relative name
	// keeps spaces out of the command line.
	cmd := exec.Command("cmd", "/c", `.\`+name+" -n 60 127.0.0.1")
	cmd.Dir = dir
	cmd.SysProcAttr = procAttr()
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start the staged child process: %v", err)
	}

	reaped := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(reaped)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		select {
		case <-reaped:
		case <-time.After(winKillGrace):
		}
		_ = exec.Command(taskkillBin(), "/F", "/IM", name).Run()
	})

	// Wait for the grandchild so the tree is known to be alive when
	// killTree runs.
	deadline := time.Now().Add(winKillGrace)
	for !imageRunning(t, name) {
		if time.Now().After(deadline) {
			t.Skipf("staged child %s never appeared; cannot exercise tree termination", name)
		}
		time.Sleep(winKillPoll)
	}

	if err := killTree(cmd.Process.Pid); err != nil {
		t.Fatalf("killTree(%d) = %v, want nil", cmd.Process.Pid, err)
	}

	select {
	case <-reaped:
	case <-time.After(winKillGrace):
		t.Fatalf("parent process %d survived killTree", cmd.Process.Pid)
	}

	deadline = time.Now().Add(winKillGrace)
	for imageRunning(t, name) {
		if time.Now().After(deadline) {
			t.Fatalf("grandchild %s survived killTree", name)
		}
		time.Sleep(winKillPoll)
	}

	if err := killTree(cmd.Process.Pid); err != nil {
		t.Errorf("second killTree(%d) = %v, want nil", cmd.Process.Pid, err)
	}
}

// stagedPingSource returns a system ping.exe to copy under a unique name.
func stagedPingSource(t *testing.T) string {
	t.Helper()

	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	src := filepath.Join(root, "System32", "ping.exe")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("ping.exe not found under %s: %v", root, err)
	}
	return src
}

// copyExecutable copies src to dst, skipping the test when either side is
// unavailable.
func copyExecutable(t *testing.T, src, dst string) {
	t.Helper()

	data, err := os.ReadFile(src)
	if err != nil {
		t.Skipf("cannot read %s: %v", src, err)
	}
	if err := os.WriteFile(dst, data, 0o755); err != nil {
		t.Skipf("cannot stage %s: %v", dst, err)
	}
}

// imageRunning reports whether a process with the given image name is
// visible to tasklist. The output is matched textually so the check does not
// depend on the system locale; a missing tasklist skips the test.
func imageRunning(t *testing.T, imageName string) bool {
	t.Helper()

	out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq "+imageName, "/FO", "CSV", "/NH").CombinedOutput()
	if err != nil && len(out) == 0 {
		t.Skipf("tasklist unavailable: %v", err)
	}
	return strings.Contains(strings.ToLower(string(out)), strings.ToLower(imageName))
}
