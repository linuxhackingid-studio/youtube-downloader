//go:build unix

package ytdlp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// killTreeGrace bounds how long a killed process group may take to vanish;
// SIGKILL is immediate, so this only absorbs scheduler and reap latency.
const killTreeGrace = 2 * time.Second

// killTreePoll is the interval between "is the group gone yet" checks.
const killTreePoll = 25 * time.Millisecond

// TestKillTreeNonPositivePid pins the degenerate pid handling: zero (and
// any negative pid) must be a no-op, because syscall.Kill(-0, SIGKILL)
// would signal the test's own process group.
func TestKillTreeNonPositivePid(t *testing.T) {
	for _, pid := range []int{0, -1} {
		if err := killTree(pid); err != nil {
			t.Errorf("killTree(%d) = %v, want nil", pid, err)
		}
	}
}

// TestKillTreeTerminatesProcessGroup starts a shell that keeps a
// background child alive ("sh -c 'sleep 30 & wait'") under procAttr() and
// verifies that killTree removes the shell, the background child, and the
// process group itself: killTree signals the group -pid, so no orphan may
// survive.
func TestKillTreeTerminatesProcessGroup(t *testing.T) {
	// The background sleep keeps a grandchild in the same process group;
	// signaling only the shell's pid would leave that grandchild running.
	cmd := exec.Command("sh", "-c", "sleep 30 & wait")
	cmd.SysProcAttr = procAttr()
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start sh: %v", err)
	}

	pid := cmd.Process.Pid
	reaped := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(reaped)
	}()
	t.Cleanup(func() {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		_ = cmd.Process.Kill()
		<-reaped
	})

	// procAttr() must have made the child its own group leader; without
	// Setpgid, -pid would not name any group and the check below would be
	// vacuous.
	if err := syscall.Kill(-pid, 0); err != nil {
		t.Fatalf("kill(-%d, 0) = %v, want nil: procAttr() did not put the child in its own process group", pid, err)
	}

	if err := killTree(pid); err != nil {
		t.Fatalf("killTree(%d) = %v, want nil", pid, err)
	}

	deadline := time.Now().Add(killTreeGrace)
	for {
		err := syscall.Kill(-pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("process group %d survived killTree: kill(-%d, 0) = %v", pid, pid, err)
		}
		time.Sleep(killTreePoll)
	}

	// A second killTree finds nothing left, and ESRCH counts as success.
	if err := killTree(pid); err != nil {
		t.Errorf("killTree(%d) on a dead group = %v, want nil", pid, err)
	}
}

// escapedPipeGrace bounds how long Handle.Exit may stay open after Kill in
// the escaped-pipe test. It is far shorter than downloadGrace, so a hung
// exit fails the test instead of being absorbed by the suite's tolerance.
const escapedPipeGrace = 3 * time.Second

// escapedPipeGap is the shortened pipeCloseGrace used by the escaped-pipe
// test, so the watchdog fires in milliseconds.
const escapedPipeGap = 100 * time.Millisecond

// TestStartDownloadKillUnblocksEscapedPipe pins the pipe-close watchdog end
// to end: a grandchild that escaped the process group with setsid keeps the
// write ends of both output pipes open past Kill, and Handle.Exit must still
// close promptly because the watchdog force-closes the read ends. Without it
// the scanners would stay blocked until the escapee exits (30s here),
// because they drain both pipes before cmd.Wait runs.
func TestStartDownloadKillUnblocksEscapedPipe(t *testing.T) {
	if _, err := exec.LookPath("setsid"); err != nil {
		t.Skipf("setsid is unavailable: %v", err) // macOS and some BSDs lack it
	}

	// The inner sh runs in a brand new session, so killTree's process-group
	// SIGKILL cannot reach it, and it inherits the script's stdout and
	// stderr - the two pipes StartDownload reads. It reports its own pid so
	// the test can prove it survived the kill.
	marker := filepath.Join(t.TempDir(), "escaped.pid")
	body := fmt.Sprintf("setsid sh -c \"echo \\$\\$ > '%s'; exec sleep 30\" &\nwait\n", marker)
	path := writeScriptTool(t, "fake-escaped-pipe", body, "exit /b 0\r\n")

	t.Cleanup(func() {
		// Best effort: a failed run may still have left the escapee behind.
		data, err := os.ReadFile(marker)
		if err != nil {
			return
		}
		if pid, convErr := strconv.Atoi(strings.TrimSpace(string(data))); convErr == nil && pid > 0 {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})

	origGap := pipeCloseGrace
	pipeCloseGrace = escapedPipeGap
	// Cleanups run in reverse order: the handle is canceled first, so by the
	// time this restores the production grace, the watchdog has taken its
	// copy of the shortened one (the test body waits for Exit first).
	t.Cleanup(func() { pipeCloseGrace = origGap })

	h, err := New(path, "").StartDownload(context.Background(), downloadOpts(t, "vid1"))
	if err != nil {
		t.Fatalf("StartDownload() error = %v", err)
	}
	t.Cleanup(func() { _ = h.Kill() })

	// Drain events: a full channel would block the scanners and mask the
	// pipe close this test is about.
	go func() {
		for range h.Events {
		}
	}()

	escaped := awaitEscapedPid(t, marker)

	if err := h.Kill(); err != nil {
		t.Fatalf("Kill() = %v, want nil", err)
	}

	// The escapee must have outlived killTree, otherwise the pipes would
	// close on their own and the watchdog would not be exercised.
	if err := syscall.Kill(escaped, 0); err != nil {
		t.Fatalf("escaped process %d did not survive Kill (%v); the test needs a pipe holder outside the killed group", escaped, err)
	}

	select {
	case exit := <-h.Exit:
		if exit == nil {
			t.Error("Exit = nil after Kill, want a non-nil error")
		}
	case <-time.After(escapedPipeGrace):
		t.Fatalf("Handle.Exit did not close within %v of Kill while process %d held the output pipes", escapedPipeGrace, escaped)
	}
}

// awaitEscapedPid waits for the scripted download to report the pid of the
// process that escaped its process group, and fails the test when it never
// appears.
func awaitEscapedPid(t *testing.T, marker string) int {
	t.Helper()

	deadline := time.Now().Add(downloadGrace)
	for {
		if data, err := os.ReadFile(marker); err == nil {
			pid, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if convErr != nil || pid <= 0 {
				t.Fatalf("escaped pid marker %q holds %q, want a positive pid", marker, data)
			}
			return pid
		}
		if time.Now().After(deadline) {
			t.Fatalf("the escaped process did not report its pid within %v", downloadGrace)
		}
		time.Sleep(killTreePoll)
	}
}
