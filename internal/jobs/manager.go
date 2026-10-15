// Package jobs implements a bounded-concurrency download orchestrator on top
// of the ytdlp client: it queues a pre-selected list of videos, runs a fixed
// number of downloads at once, tracks per-job progress, and exposes
// thread-safe snapshots for the TUI.
package jobs

import (
	"bufio"
	"context"
	"errors"
	"os"
	"strings"
	"sync"

	"linuxhackingid.com/youtube-downloader/internal/ytdlp"
)

const (
	// maxConcurrency is the upper bound applied to Options.Concurrency.
	maxConcurrency = 10
	// maxLogLines is the capacity of the global log ring buffer.
	maxLogLines = 200
	// maxLogLineLen bounds each stored log line in bytes so a single
	// oversized progress line cannot inflate the ring buffer's memory
	// footprint. This is a byte bound only; the render side sanitizes
	// and truncates for display.
	maxLogLineLen = 4096
	// maxErrLen bounds the length of a failure message kept in a snapshot.
	maxErrLen = 200
	// archiveSkipMsg is the marker yt-dlp prints when a video is already
	// listed in the download archive.
	archiveSkipMsg = "has already been recorded in the archive"
)

// Status is the lifecycle state of a single download job.
type Status int

// Job states, in lifecycle order.
const (
	StatusQueued Status = iota
	StatusRunning
	StatusMerging
	StatusDone
	StatusSkipped
	StatusFailed
	StatusCanceled
)

// String returns the lowercase wire name of the status.
func (s Status) String() string {
	switch s {
	case StatusQueued:
		return "queued"
	case StatusRunning:
		return "running"
	case StatusMerging:
		return "merging"
	case StatusDone:
		return "done"
	case StatusSkipped:
		return "skipped"
	case StatusFailed:
		return "failed"
	case StatusCanceled:
		return "canceled"
	default:
		return "unknown"
	}
}

// JobSnapshot is an immutable copy of one job's state.
type JobSnapshot struct {
	Video      ytdlp.Video
	Status     Status
	Percent    float64 // 0..100; -1 unknown; 100 when done
	SpeedBps   float64 // -1 unknown
	ETASeconds float64 // -1 unknown
	Err        string  // short error text when failed
}

// Summary aggregates the state of every job managed by a Manager.
type Summary struct {
	Done     int
	Skipped  int
	Failed   int
	Canceled int
	Total    int
}

// Options configures a Manager run.
type Options struct {
	PlaylistURL      string
	TargetDir        string
	Quality          ytdlp.Quality
	FilenameTemplate string
	CookiesBrowser   string
	ArchivePath      string        // "" disables archive
	Concurrency      int           // clamped 1..10
	Videos           []ytdlp.Video // pre-selected list
}

// job is the mutable state of a single queued download. Every field is
// guarded by its owning Manager's mutex.
type job struct {
	video       ytdlp.Video
	status      Status
	percent     float64
	speed       float64
	eta         float64
	errText     string
	archiveSeen bool // yt-dlp reported the video as already archived
}

// Manager runs downloads for a pre-selected list of videos with bounded
// concurrency. All methods are safe for concurrent use; a zero-value Manager
// is not usable, construct one with NewManager.
type Manager struct {
	client *ytdlp.Client
	opts   Options

	done chan struct{}

	startOnce  sync.Once
	cancelOnce sync.Once
	doneOnce   sync.Once

	mu      sync.Mutex
	jobs    []*job
	logs    []string
	handles map[*ytdlp.Handle]struct{}

	ctx      context.Context
	cancel   context.CancelFunc
	started  bool
	canceled bool

	wg sync.WaitGroup
}

// NewManager returns a Manager for the given client and options. No
// goroutines are started and no filesystem changes are made until Start is
// called.
func NewManager(client *ytdlp.Client, opts Options) *Manager {
	m := &Manager{
		client:  client,
		opts:    opts,
		done:    make(chan struct{}),
		handles: make(map[*ytdlp.Handle]struct{}),
	}
	m.jobs = make([]*job, 0, len(opts.Videos))
	for _, v := range opts.Videos {
		m.jobs = append(m.jobs, &job{
			video:   v,
			status:  StatusQueued,
			percent: -1,
			speed:   -1,
			eta:     -1,
		})
	}
	return m
}

// Start begins downloading the queued videos. It does not block; progress is
// reported through Snapshot, Summary, and Logs, and completion through Done.
// Only the first call has any effect. Start creates TargetDir with mode 0o755
// and skips videos already present in ArchivePath without spawning a process
// for them.
func (m *Manager) Start(ctx context.Context) {
	if m == nil {
		return
	}
	m.startOnce.Do(func() {
		if ctx == nil {
			ctx = context.Background()
		}

		m.mu.Lock()
		m.ctx, m.cancel = context.WithCancel(ctx)
		m.started = true
		canceled := m.canceled
		m.mu.Unlock()

		if canceled {
			m.mu.Lock()
			for _, j := range m.jobs {
				if j.status == StatusQueued {
					j.status = StatusCanceled
				}
			}
			m.mu.Unlock()
			m.closeDone()
			return
		}

		if m.client == nil {
			m.mu.Lock()
			for _, j := range m.jobs {
				if j.status == StatusQueued {
					j.status = StatusFailed
					j.errText = "download client unavailable"
				}
			}
			m.mu.Unlock()
			m.closeDone()
			return
		}

		if dir := strings.TrimSpace(m.opts.TargetDir); dir != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				m.appendLog("jobs: cannot create target directory: " + err.Error())
			}
		}

		archived := map[string]bool{}
		if m.opts.ArchivePath != "" {
			archived = readArchive(m.opts.ArchivePath)
		}

		m.mu.Lock()
		pending := make([]*job, 0, len(m.jobs))
		for _, j := range m.jobs {
			if id := strings.TrimSpace(j.video.ID); id != "" && archived[id] {
				j.status = StatusSkipped
				j.percent = 100
				continue
			}
			pending = append(pending, j)
		}
		m.mu.Unlock()

		conc := m.opts.Concurrency
		if conc < 1 {
			conc = 1
		}
		if conc > maxConcurrency {
			conc = maxConcurrency
		}

		queue := make(chan *job)
		m.wg.Add(1)
		go m.feed(queue, pending)

		for i := 0; i < conc; i++ {
			m.wg.Add(1)
			go m.worker(queue)
		}

		go func() {
			m.wg.Wait()
			m.closeDone()
		}()
	})
}

// Snapshot returns a deep copy of every job's current state. It is safe to
// call from any goroutine at any time.
func (m *Manager) Snapshot() []JobSnapshot {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]JobSnapshot, 0, len(m.jobs))
	for _, j := range m.jobs {
		out = append(out, JobSnapshot{
			Video:      j.video,
			Status:     j.status,
			Percent:    j.percent,
			SpeedBps:   j.speed,
			ETASeconds: j.eta,
			Err:        j.errText,
		})
	}
	return out
}

// Summary returns aggregate job counts. It is safe to call at any time.
func (m *Manager) Summary() Summary {
	if m == nil {
		return Summary{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	s := Summary{Total: len(m.jobs)}
	for _, j := range m.jobs {
		switch j.status {
		case StatusDone:
			s.Done++
		case StatusSkipped:
			s.Skipped++
		case StatusFailed:
			s.Failed++
		case StatusCanceled:
			s.Canceled++
		}
	}
	return s
}

// Done returns a channel that is closed once every worker has finished or
// Cancel has completed. If neither Start nor Cancel is ever called the
// channel stays open forever.
func (m *Manager) Done() <-chan struct{} {
	if m == nil {
		return nil
	}
	return m.done
}

// Cancel stops all downloads: it kills every running handle, cancels the
// internal context derived from the Start context, and marks queued jobs as
// canceled. Handles are killed while the run context is still live so
// taskkill /T /F can enumerate yt-dlp's children; canceling the context
// first would let os/exec reap yt-dlp before its ffmpeg children are found.
// Cancel is idempotent and safe to call from any goroutine.
func (m *Manager) Cancel() {
	if m == nil {
		return
	}
	m.cancelOnce.Do(func() {
		m.mu.Lock()
		m.canceled = true
		cancel := m.cancel
		started := m.started
		handles := make([]*ytdlp.Handle, 0, len(m.handles))
		for h := range m.handles {
			handles = append(handles, h)
		}
		m.mu.Unlock()

		// Kill outside the lock, context still live: Handle.Kill runs
		// taskkill /T /F before canceling its own run context, so the
		// process tree is walked while the parent is still alive.
		for _, h := range handles {
			if h != nil {
				h.Kill()
			}
		}

		if cancel != nil {
			cancel()
		}

		m.mu.Lock()
		for _, j := range m.jobs {
			if j.status == StatusQueued {
				j.status = StatusCanceled
			}
		}
		m.mu.Unlock()

		if !started {
			m.closeDone()
		}
	})
}

// Logs returns up to the 200 most recent progress lines, oldest first. It is
// safe to call from any goroutine.
func (m *Manager) Logs() []string {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]string, len(m.logs))
	copy(out, m.logs)
	return out
}

// feed pushes pending jobs onto the queue and closes it when drained. Once
// the context is canceled it marks every remaining job as canceled instead
// of handing it to a worker.
func (m *Manager) feed(queue chan<- *job, pending []*job) {
	defer m.wg.Done()
	defer close(queue)

	for _, j := range pending {
		if m.ctx.Err() != nil {
			m.markCanceled(j)
			continue
		}
		select {
		case <-m.ctx.Done():
			m.markCanceled(j)
		case queue <- j:
		}
	}
}

// worker consumes jobs until the queue is closed.
func (m *Manager) worker(queue <-chan *job) {
	defer m.wg.Done()

	for j := range queue {
		if m.ctx.Err() != nil {
			m.markCanceled(j)
			continue
		}
		m.runJob(j)
	}
}

// runJob downloads a single video and applies its final status.
func (m *Manager) runJob(j *job) {
	if !m.beginJob(j) {
		return
	}

	dlOpts := ytdlp.DownloadOpts{
		PlaylistURL:      m.opts.PlaylistURL,
		Video:            j.video,
		TargetDir:        m.opts.TargetDir,
		Quality:          m.opts.Quality,
		FilenameTemplate: m.opts.FilenameTemplate,
		CookiesBrowser:   m.opts.CookiesBrowser,
		ArchivePath:      m.opts.ArchivePath,
	}

	h, err := m.client.StartDownload(m.ctx, dlOpts)
	if err != nil {
		m.failJob(j, err)
		return
	}
	if h == nil {
		m.failJob(j, errors.New("download handle unavailable"))
		return
	}

	m.mu.Lock()
	m.handles[h] = struct{}{}
	cancelRequested := m.canceled
	m.mu.Unlock()

	// Close the race with Cancel: Cancel snapshots the handle set and
	// kills what it saw before canceling the context, so a handle
	// registered after that snapshot would still observe a live context
	// while the kill loop runs. The m.canceled read is therefore taken in
	// the same critical section as the registration; relying on ctx.Err()
	// alone would let os/exec hard-kill yt-dlp without taskkill /T and
	// orphan its ffmpeg children.
	if cancelRequested || m.ctx.Err() != nil {
		h.Kill()
	}

	for ev := range h.Events {
		m.handleEvent(j, ev)
	}

	m.mu.Lock()
	delete(m.handles, h)
	m.mu.Unlock()

	var exitErr error
	if ch := h.Exit; ch != nil {
		if v, ok := <-ch; ok {
			exitErr = v
		}
	}
	m.finishJob(j, exitErr)
}

// beginJob transitions a queued job to running. It reports false when the
// job was already canceled or claimed, or when cancellation was requested.
func (m *Manager) beginJob(j *job) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.canceled || m.ctx.Err() != nil || j.status == StatusCanceled {
		j.status = StatusCanceled
		return false
	}
	if j.status != StatusQueued {
		return false
	}
	j.status = StatusRunning
	return true
}

// markCanceled marks a job as canceled unless it already reached a terminal
// state.
func (m *Manager) markCanceled(j *job) {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch j.status {
	case StatusDone, StatusSkipped, StatusFailed:
		return
	}
	j.status = StatusCanceled
}

// handleEvent applies one progress event to a job. Every raw line is also
// appended to the global log ring.
func (m *Manager) handleEvent(j *job, ev ytdlp.ProgressEvent) {
	if line := strings.TrimSpace(ev.Line); line != "" {
		m.appendLog(line)
	}

	if strings.Contains(strings.ToLower(ev.Line), archiveSkipMsg) {
		m.mu.Lock()
		j.archiveSeen = true
		if j.status != StatusCanceled {
			j.status = StatusSkipped
			j.percent = 100
		}
		m.mu.Unlock()
		return
	}

	switch ev.Stage {
	case ytdlp.StageDownloading:
		m.mu.Lock()
		if j.status != StatusCanceled {
			j.status = StatusRunning
			if ev.Percent >= 0 {
				j.percent = ev.Percent
			}
			if ev.SpeedBps >= 0 {
				j.speed = ev.SpeedBps
			}
			if ev.ETASeconds >= 0 {
				j.eta = ev.ETASeconds
			}
		}
		m.mu.Unlock()

	case ytdlp.StageMerging, ytdlp.StagePostprocessing:
		m.mu.Lock()
		if j.status != StatusCanceled {
			j.status = StatusMerging
		}
		m.mu.Unlock()

	case ytdlp.StageFinished:
		// Keep the current state; the final status is applied once the
		// events channel closes and Exit is available.
	}
}

// finishJob applies the outcome of a finished download. The events channel
// must already be closed and the handle deregistered.
func (m *Manager) finishJob(j *job, exitErr error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// A clean exit wins over a late Cancel: the events channel was fully
	// drained and yt-dlp exited 0, so the download really completed. A
	// kill can never produce exitErr == nil, so a Cancel racing this
	// status write must not rewrite a success as canceled.
	if exitErr == nil {
		if j.archiveSeen {
			j.status = StatusSkipped
		} else {
			j.status = StatusDone
		}
		j.percent = 100
		return
	}

	// m.canceled is checked alongside m.ctx because Cancel kills handles
	// before canceling the context: a handle killed by taskkill can
	// surface an *ExitError before ctx cancellation lands, and that exit
	// must still resolve to canceled rather than failed.
	if m.canceled || m.ctx.Err() != nil || j.status == StatusCanceled ||
		errors.Is(exitErr, context.Canceled) || errors.Is(exitErr, context.DeadlineExceeded) {
		j.status = StatusCanceled
		return
	}

	j.status = StatusFailed
	j.errText = shortError(exitErr)
}

// failJob marks a job failed or canceled when it never started a download.
func (m *Manager) failJob(j *job, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.canceled || m.ctx.Err() != nil {
		j.status = StatusCanceled
		return
	}
	j.status = StatusFailed
	j.errText = shortError(err)
}

// appendLog adds one line to the bounded global log ring. Each stored line
// is capped at maxLogLineLen bytes.
func (m *Manager) appendLog(line string) {
	if len(line) > maxLogLineLen {
		// Cap by bytes, then drop an incomplete UTF-8 sequence split by
		// the cut so the ring never retains stray continuation bytes.
		line = strings.ToValidUTF8(line[:maxLogLineLen], "")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.logs = append(m.logs, line)
	if over := len(m.logs) - maxLogLines; over > 0 {
		n := copy(m.logs, m.logs[over:])
		m.logs = m.logs[:n]
	}
}

// closeDone closes the Done channel exactly once.
func (m *Manager) closeDone() {
	m.doneOnce.Do(func() {
		close(m.done)
	})
}

// readArchive loads the set of "youtube" video IDs already recorded in a
// yt-dlp archive file. A missing or unreadable file yields an empty set so
// downloads proceed; malformed lines are ignored.
func readArchive(path string) map[string]bool {
	ids := make(map[string]bool)

	f, err := os.Open(path)
	if err != nil {
		return ids
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 2 && strings.EqualFold(fields[0], "youtube") {
			ids[fields[1]] = true
		}
	}
	return ids
}

// shortError flattens an error into a single bounded line for the TUI.
func shortError(err error) string {
	if err == nil {
		return ""
	}
	s := strings.TrimSpace(err.Error())
	if r := []rune(s); len(r) > maxErrLen {
		s = string(r[:maxErrLen])
	}
	return s
}
