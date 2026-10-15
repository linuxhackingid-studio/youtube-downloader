package ytdlp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// DownloadOpts describes one yt-dlp download: which playlist entry to
// fetch, where to put it, and how to name and format the result.
type DownloadOpts struct {
	PlaylistURL      string  // playlist or watch URL passed to yt-dlp
	Video            Video   // entry to download; Index selects it with -I
	TargetDir        string  // -P: directory downloads are written to
	Quality          Quality // quality cap and audio/video mode
	FilenameTemplate string  // -o: yt-dlp output template
	CookiesBrowser   string  // --cookies-from-browser value; empty to omit
	ArchivePath      string  // --download-archive file; empty to omit
}

// Constants used by StartDownload. The progress templates produce exactly
// the DL and PP lines ParseProgressLine understands; yt-dlp emits them
// without the leading "download:"/"postprocess:" selector.
const (
	downloadProgressTemplate    = "download:DL|%(info.id)s|%(progress.status)s|%(progress.downloaded_bytes)s|%(progress.total_bytes)s|%(progress.total_bytes_estimate)s|%(progress.speed)s|%(progress.eta)s|%(progress.percent)s"
	postprocessProgressTemplate = "postprocess:PP|%(info.id)s|%(progress.status)s"

	eventsCapacity   = 256         // buffered progress events per download
	scanInitialBuf   = 64 * 1024   // initial output scanner buffer
	scanMaxTokenSize = 1024 * 1024 // largest accepted output line
	stderrTailLines  = 40          // non-empty stderr lines retained

	// progressDelta throttles yt-dlp progress emission to roughly four
	// lines per second per process, matching the TUI's 250ms tick. The
	// yt-dlp default (0) emits progress unthrottled. yt-dlp parses the
	// value as a float, so the fractional seconds are accepted.
	progressDelta = "0.25"
)

// BuildDownloadArgs returns the yt-dlp argv for o, without the executable
// path. The function is pure: it reads only o and the receiver's configured
// paths and never touches process state.
func (c *Client) BuildDownloadArgs(o DownloadOpts) []string {
	args := []string{
		"--ignore-config",
		"--no-warnings",
		"--no-color",
		"--newline",
		"--progress-delta", progressDelta,
		"--progress-template", downloadProgressTemplate,
		"--progress-template", postprocessProgressTemplate,
		"-P", o.TargetDir,
		"-o", o.FilenameTemplate,
		"-f", o.Quality.FormatSelector(),
	}

	if !o.Quality.IsAudio() && c.FFmpegPath != "" {
		args = append(args, "--merge-output-format", "mp4")
	}
	if o.Quality.IsAudio() {
		args = append(args, "-x", "--audio-format", "mp3", "--audio-quality", "0")
	}
	if o.ArchivePath != "" {
		args = append(args, "--download-archive", o.ArchivePath)
	}
	if c.FFmpegPath != "" {
		args = append(args, "--ffmpeg-location", c.FFmpegPath)
	}
	if o.CookiesBrowser != "" {
		args = append(args, "--cookies-from-browser", o.CookiesBrowser)
	}
	if o.Video.Index > 0 {
		args = append(args, "-I", strconv.Itoa(o.Video.Index))
	}

	return append(args, o.PlaylistURL)
}

// Handle is one running yt-dlp download.
//
// Events streams progress until yt-dlp exits and both output pipes are
// exhausted. Sends to it block once its buffer is full, so consumers must
// drain it until it is closed. Exit is buffered with capacity one and
// carries exactly one value — nil for a successful run, an *ExitError for a
// failed one, or the context error when the download was canceled — before
// it is closed. Pid is the process id of yt-dlp itself.
type Handle struct {
	Events <-chan ProgressEvent
	Exit   <-chan error
	Pid    int

	cmd    *exec.Cmd
	cancel context.CancelFunc
	once   sync.Once
}

// StartDownload launches yt-dlp for o and returns a handle to its output.
// The download is bound to ctx: canceling ctx, or calling Kill, terminates
// the process and surfaces the cancellation through Exit.
//
// The returned handle is non-nil if and only if the error is nil.
func (c *Client) StartDownload(ctx context.Context, o DownloadOpts) (*Handle, error) {
	runCtx, cancel := context.WithCancel(ctx)

	cmd := exec.CommandContext(runCtx, c.Path, c.BuildDownloadArgs(o)...)
	cmd.SysProcAttr = procAttr()

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

	events := make(chan ProgressEvent, eventsCapacity)
	exit := make(chan error, 1)
	ring := &stderrRing{}

	var scanners sync.WaitGroup
	scanners.Add(2)
	go scanOutput(stdout, events, o.Video.ID, true, nil, &scanners)
	go scanOutput(stderr, events, o.Video.ID, false, ring, &scanners)

	h := &Handle{
		Events: events,
		Exit:   exit,
		Pid:    cmd.Process.Pid,
		cmd:    cmd,
		cancel: cancel,
	}

	go func() {
		defer cancel()

		// Drain both pipes before Wait: Wait closes them as soon as the
		// process is reaped, which would truncate buffered output.
		scanners.Wait()
		waitErr := cmd.Wait()

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
	}()

	return h, nil
}

// Kill terminates the download's process tree. It is idempotent and safe
// for concurrent use, and never waits for the download to finish — the
// outcome still arrives through Exit. On Windows it runs
// taskkill /T /F so that ffmpeg children die with yt-dlp; elsewhere it
// signals the process directly. Kill always returns nil: by the time it is
// called the process may already be gone, which is not a failure.
func (h *Handle) Kill() error {
	if h == nil {
		return nil
	}

	h.once.Do(func() {
		// Terminate the process tree before canceling the run context:
		// taskkill /T must be able to enumerate yt-dlp's children (ffmpeg)
		// while the parent is still alive. The cancel below is the
		// fallback — and the only mechanism — when taskkill is missing.
		if h.cmd != nil && h.cmd.Process != nil {
			if runtime.GOOS == "windows" {
				taskkill := exec.Command(taskkillBin(), "/T", "/F", "/PID", strconv.Itoa(h.Pid))
				taskkill.SysProcAttr = procAttr()
				_, _ = taskkill.CombinedOutput()
			} else if err := h.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				// Best effort: a process that already exited reports
				// os.ErrProcessDone, and any other failure is equally
				// unrecoverable here. Exit reports the download's outcome.
			}
		}

		h.cancel()
	})

	return nil
}

// taskkillBin resolves the taskkill.exe used to terminate a download's
// process tree. The fully qualified path under %SystemRoot% is preferred:
// a PATH lookup can be missing or poisoned in stripped environments. When
// the file does not exist, the bare name is returned so exec falls back to
// a PATH search, which itself falls back to canceling the context.
func taskkillBin() string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	bin := filepath.Join(root, "System32", "taskkill.exe")
	if _, err := os.Stat(bin); err != nil {
		return "taskkill"
	}
	return bin
}

// scanOutput reads r line by line and forwards every non-empty line to
// events until the stream ends. When parse is true, lines matching a
// progress template become parsed events; every other line becomes a raw
// StageInitializing event with unknown numeric fields (-1). When ring is
// non-nil, lines are also retained in it for error reporting.
func scanOutput(r io.Reader, events chan<- ProgressEvent, videoID string, parse bool, ring *stderrRing, wg *sync.WaitGroup) {
	defer wg.Done()

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, scanInitialBuf), scanMaxTokenSize)

	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		if ring != nil {
			ring.add(line)
		}

		if !parse {
			events <- rawEvent(videoID, line)
			continue
		}

		ev, ok := ParseProgressLine(line)
		if !ok {
			events <- rawEvent(videoID, line)
			continue
		}
		if ev.VideoID == "" {
			ev.VideoID = videoID
		}
		ev.Line = line
		events <- ev
	}

	if sc.Err() != nil {
		// The scanner gave up — for example on a line longer than the
		// buffer limit. Keep draining so yt-dlp never blocks writing to a
		// full pipe while Wait reaps it.
		_, _ = io.Copy(io.Discard, r)
	}
}

// rawEvent builds the sentinel event forwarded for output lines that are
// not machine-readable progress.
func rawEvent(videoID, line string) ProgressEvent {
	return ProgressEvent{
		VideoID:    videoID,
		Stage:      StageInitializing,
		Percent:    -1,
		SpeedBps:   -1,
		ETASeconds: -1,
		BytesDone:  -1,
		BytesTotal: -1,
		Line:       line,
	}
}

// stderrRing keeps the tail of a download's stderr so a failed run can
// report what yt-dlp complained about. A single scanner goroutine writes,
// and the wait goroutine reads once the scanners have stopped; the mutex
// makes the handoff explicit.
type stderrRing struct {
	mu    sync.Mutex
	lines []string // at most stderrTailLines entries, oldest first
}

// add appends one non-empty, trimmed line, dropping the oldest line when
// the ring is full.
func (r *stderrRing) add(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.lines) < stderrTailLines {
		r.lines = append(r.lines, line)
		return
	}
	copy(r.lines, r.lines[1:])
	r.lines[len(r.lines)-1] = line
}

// snapshot returns a copy of the retained lines, oldest first.
func (r *stderrRing) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]string, len(r.lines))
	copy(out, r.lines)
	return out
}
