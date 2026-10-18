package ytdlp

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// pipeCloseGrace bounds how long a download's pipe watchdog waits after
// cancellation before it force-closes the output pipes. Canceling a run
// kills the process tree, but a process that escaped that tree can keep the
// write ends of the pipes open; the scanners drain both pipes before
// cmd.Wait runs, so without the watchdog they would block until the escapee
// finally exits, and Handle.Exit would stay open that whole time. It is a
// variable so tests can shorten the wait; production runs never change it.
var pipeCloseGrace = 10 * time.Second

// Handle is one running yt-dlp download.
//
// Events streams progress until yt-dlp exits and both output pipes are
// exhausted. Sends to it block once its buffer is full, so consumers must
// drain it until it is closed. Exit is buffered with capacity one and
// carries exactly one value - nil for a successful run, an *ExitError for a
// failed one, or the context error when the download was canceled - before
// it is closed. Pid is the process id of yt-dlp itself.
type Handle struct {
	Events <-chan ProgressEvent
	Exit   <-chan error
	Pid    int

	cmd    *exec.Cmd
	cancel context.CancelFunc
	once   sync.Once

	// reaped reports that cmd.Wait has returned, so the process id in Pid
	// may since have been recycled by the operating system. Both Kill and
	// the command's Cancel hook skip their kill while it is set: the
	// download no longer owns the pid, and a signal would walk an unrelated
	// process tree instead.
	reaped atomic.Bool
}

// StartDownload launches yt-dlp for o and returns a handle to its output.
// The download is bound to ctx: canceling ctx, or calling Kill, terminates
// the process tree and surfaces the cancellation through Exit.
//
// The returned handle is non-nil if and only if the error is nil.
func (c *Client) StartDownload(ctx context.Context, o DownloadOpts) (*Handle, error) {
	runCtx, cancel := context.WithCancel(ctx)

	events := make(chan ProgressEvent, eventsCapacity)
	exit := make(chan error, 1)

	// The handle is built before the command starts so the Cancel hook
	// below can consult its reaped flag.
	h := &Handle{
		Events: events,
		Exit:   exit,
		cancel: cancel,
	}

	cmd := exec.CommandContext(runCtx, c.Path, c.BuildDownloadArgs(o)...)
	cmd.SysProcAttr = procAttr()

	// CommandContext's default Cancel kills only yt-dlp itself, which would
	// orphan a running ffmpeg on Unix; killTree takes down the whole process
	// tree instead. The hook skips the kill once the wait goroutine has
	// reaped the process, because then the pid may already name an unrelated
	// process (exec itself calls Cancel when the context is canceled at any
	// point, including after Wait).
	//
	// WaitDelay bounds the standard reap path only. It arms exec's internal
	// timer once the context is canceled or the process exits, force-kills
	// the process, and closes the pipes of the copying goroutines exec
	// starts for non-file Stdout or Stderr. StdoutPipe and StderrPipe
	// return *os.File pipes, so no copying goroutines exist and that timer
	// has no pipes to close: it cannot unblock the scanners. The watchdog
	// started after cmd.Start is what does that.
	cmd.Cancel = func() error {
		if cmd.Process == nil || h.reaped.Load() {
			return nil
		}
		return killTree(cmd.Process.Pid)
	}
	cmd.WaitDelay = 10 * time.Second

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("open yt-dlp stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		// Close the already-opened stdout pipe so the read end is not
		// retained after the call returns.
		_ = stdout.Close()
		cancel()
		return nil, fmt.Errorf("open yt-dlp stderr: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start yt-dlp: %w", err)
	}
	h.Pid = cmd.Process.Pid
	h.cmd = cmd

	ring := &stderrRing{}

	var scanners sync.WaitGroup
	scanners.Add(2)
	go scanOutput(stdout, events, o.Video.ID, true, nil, &scanners)
	go scanOutput(stderr, events, o.Video.ID, false, ring, &scanners)

	// done closes when the download is over: both scanners have stopped, the
	// process has been reaped, and the outcome has been sent on Exit. It is
	// a separate channel rather than Exit itself because Exit carries
	// exactly one value, which only the caller may receive.
	done := make(chan struct{})

	go func() {
		defer cancel()

		// Drain both pipes before Wait: Wait closes them as soon as the
		// process is reaped, which would truncate buffered output.
		scanners.Wait()

		waitErr := cmd.Wait()
		// The pid is free for reuse from here on; record that before
		// anything can observe the outcome. A signal sent in the few
		// instructions between the kernel reaping the process and this
		// store can still reach a fresh owner of the pid: closing that
		// window needs a pidfd-style handle the standard library does not
		// expose portably.
		h.reaped.Store(true)

		switch {
		case waitErr == nil:
			exit <- nil
		case runCtx.Err() != nil:
			exit <- runCtx.Err()
		default:
			code := 1
			var execErr *exec.ExitError
			if errors.As(waitErr, &execErr) {
				code = execErr.ExitCode()
			}
			exit <- &ExitError{Code: code, Stderr: lastNonEmpty(ring.snapshot())}
		}

		close(exit)
		close(events)
		close(done)
	}()

	// The pipe-close watchdog. A process that escaped the process group
	// (setsid on Unix, a failed taskkill on Windows) can keep the pipes'
	// write ends open after cancellation; the scanners block on those pipes
	// and are drained before cmd.Wait, so nothing else would ever unblock
	// them. The watchdog closes the read ends once pipeCloseGrace has
	// elapsed after cancellation: the blocked reads then fail with
	// os.ErrClosed ("file already closed") and the scanners finish. Closing
	// an *os.File only makes concurrent reads return that error, so this
	// cannot corrupt a scanner.
	//
	// The goroutine never outlives the download: it returns as soon as done
	// closes, both when the download finished before the grace period and
	// when it finished during it.
	go func() {
		select {
		case <-done:
			// A healthy download: no timer is armed and nothing is closed.
			return
		case <-runCtx.Done():
		}

		timer := time.NewTimer(pipeCloseGrace)
		defer timer.Stop()

		select {
		case <-done:
			// The pipes drained within the grace period after all.
			return
		case <-timer.C:
		}

		// Errors are ignored: a second close in cmd.Wait reports ErrClosed,
		// and the scanners are the only readers left.
		_ = stdout.Close()
		_ = stderr.Close()
	}()

	return h, nil
}

// Kill terminates the download's process tree. It is idempotent and safe
// for concurrent use, and never waits for the download to finish - the
// outcome still arrives through Exit.
//
// killTree runs before the run context is canceled so the process tree is
// walked while the parent is still alive: taskkill /T /F on Windows and a
// process-group SIGKILL on Unix both need yt-dlp to still exist in order to
// find its children (ffmpeg). Canceling the context afterwards is the
// fallback - it is also what runs the command's Cancel hook, which is a
// no-op once the tree is already gone. Kill always returns nil: by the time
// it is called the process may already be gone, which is not a failure.
//
// A reaped download is never signaled: its pid may belong to an unrelated
// process by now. Kill still cancels the run context in that case, and the
// sync.Once keeps the call idempotent either way.
func (h *Handle) Kill() error {
	if h == nil {
		return nil
	}

	h.once.Do(func() {
		if h.cmd != nil && h.cmd.Process != nil && !h.reaped.Load() {
			_ = killTree(h.Pid)
		}

		h.cancel()
	})

	return nil
}
