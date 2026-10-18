package jobs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"linuxhackingid.com/youtube-downloader/internal/ytdlp"
)

// newTransitionManager returns a manager with a live background context and
// no client, for exercising the job-transition helpers directly. Nothing is
// started, so no goroutine runs and no filesystem is touched.
func newTransitionManager(t *testing.T, videos ...ytdlp.Video) *Manager {
	t.Helper()

	m := NewManager(nil, Options{Videos: videos})
	m.ctx = context.Background()
	return m
}

// TestMarkCanceledLeavesTerminalStates checks that only a queued job is
// rewritten as canceled: jobs that already reached done, skipped, failed, or
// canceled keep their status. Pinned Revision contract: a no-op rewrite of a
// terminal job never bumps Revision, an actual state change always does, so
// the five jobs below produce exactly one bump.
func TestMarkCanceledLeavesTerminalStates(t *testing.T) {
	video := ytdlp.Video{ID: "v"}
	m := newTransitionManager(t, video, video, video, video, video)

	statuses := []Status{StatusQueued, StatusDone, StatusSkipped, StatusFailed, StatusCanceled}
	for i, st := range statuses {
		m.jobs[i].status = st
	}
	m.rev = 0

	for _, j := range m.jobs {
		m.markCanceled(j)
	}

	if m.jobs[0].status != StatusCanceled {
		t.Errorf("queued job status = %v after markCanceled, want canceled", m.jobs[0].status)
	}
	for i := 1; i < len(statuses); i++ {
		if m.jobs[i].status != statuses[i] {
			t.Errorf("job %d status = %v after markCanceled, want it unchanged at %v", i, m.jobs[i].status, statuses[i])
		}
	}
	if m.rev != 1 {
		t.Errorf("rev = %d after markCanceled over one real transition, want 1", m.rev)
	}
}

// TestFailJobOutcomes checks the two outcomes of a job that never started a
// download: canceled while cancellation is pending, failed otherwise.
func TestFailJobOutcomes(t *testing.T) {
	err := errors.New("client exploded")

	t.Run("live manager fails the job", func(t *testing.T) {
		m := newTransitionManager(t, ytdlp.Video{ID: "v"})
		m.failJob(m.jobs[0], err)

		if m.jobs[0].status != StatusFailed {
			t.Errorf("status = %v, want failed", m.jobs[0].status)
		}
		if m.jobs[0].errText != "client exploded" {
			t.Errorf("errText = %q, want %q", m.jobs[0].errText, "client exploded")
		}
	})

	t.Run("canceled context cancels the job", func(t *testing.T) {
		m := newTransitionManager(t, ytdlp.Video{ID: "v"})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		m.ctx = ctx

		m.failJob(m.jobs[0], err)

		if m.jobs[0].status != StatusCanceled {
			t.Errorf("status = %v, want canceled", m.jobs[0].status)
		}
		if m.jobs[0].errText != "" {
			t.Errorf("errText = %q, want empty for a canceled job", m.jobs[0].errText)
		}
	})

	t.Run("cancel flag cancels the job", func(t *testing.T) {
		m := newTransitionManager(t, ytdlp.Video{ID: "v"})
		m.canceled = true

		m.failJob(m.jobs[0], err)

		if m.jobs[0].status != StatusCanceled {
			t.Errorf("status = %v, want canceled", m.jobs[0].status)
		}
	})
}

// TestBeginJobTransitions covers every guard of beginJob: the happy claim,
// a second claim of the same job, a canceled job, a canceled manager and a
// canceled context.
func TestBeginJobTransitions(t *testing.T) {
	t.Run("claims a queued job once", func(t *testing.T) {
		m := newTransitionManager(t, ytdlp.Video{ID: "v"})
		j := m.jobs[0]

		if !m.beginJob(j) {
			t.Fatal("beginJob(queued) = false, want true")
		}
		if j.status != StatusRunning {
			t.Errorf("status = %v, want running", j.status)
		}
		if m.beginJob(j) {
			t.Error("second beginJob(already running) = true, want false")
		}
	})

	t.Run("refuses a canceled job", func(t *testing.T) {
		m := newTransitionManager(t, ytdlp.Video{ID: "v"})
		j := m.jobs[0]
		j.status = StatusCanceled
		m.rev = 0

		if m.beginJob(j) {
			t.Error("beginJob(canceled job) = true, want false")
		}
		if j.status != StatusCanceled {
			t.Errorf("status = %v, want canceled kept", j.status)
		}
		if m.rev != 0 {
			t.Errorf("rev = %d after beginJob on an already-canceled job, want 0 (no state change)", m.rev)
		}

		// A repeated refusal must stay invisible to Revision too.
		if m.beginJob(j) {
			t.Error("second beginJob(canceled job) = true, want false")
		}
		if m.rev != 0 {
			t.Errorf("rev = %d after a second refusal, want 0", m.rev)
		}
	})

	t.Run("refuses after manager cancellation", func(t *testing.T) {
		m := newTransitionManager(t, ytdlp.Video{ID: "v"})
		m.canceled = true

		if m.beginJob(m.jobs[0]) {
			t.Error("beginJob() = true after Cancel, want false")
		}
		if m.jobs[0].status != StatusCanceled {
			t.Errorf("status = %v, want canceled", m.jobs[0].status)
		}
	})

	t.Run("refuses after context cancellation", func(t *testing.T) {
		m := newTransitionManager(t, ytdlp.Video{ID: "v"})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		m.ctx = ctx

		if m.beginJob(m.jobs[0]) {
			t.Error("beginJob() = true with a canceled context, want false")
		}
	})
}

// TestHandleEventArchiveSkip pins the already-archived marker handling: the
// marker marks the job skipped at 100 percent regardless of the stage, and
// a canceled job keeps its status while still being flagged. The detection is
// anchored to the "[download]" diagnostic prefix, so lookalike text without
// that prefix must not spoof a skip.
func TestHandleEventArchiveSkip(t *testing.T) {
	// The shape real yt-dlp emits: "[download] <label> has already been
	// recorded in the archive".
	const marker = "[download] Video v " + archiveSkipMsg

	t.Run("running job becomes skipped", func(t *testing.T) {
		m := newTransitionManager(t, ytdlp.Video{ID: "v"})
		j := m.jobs[0]
		j.status = StatusRunning
		m.rev = 0

		m.handleEvent(j, ytdlp.ProgressEvent{VideoID: "v", Stage: ytdlp.StageDownloading, Line: marker})

		if !j.archiveSeen {
			t.Error("archiveSeen = false, want true")
		}
		if j.status != StatusSkipped {
			t.Errorf("status = %v, want skipped", j.status)
		}
		if j.percent != 100 {
			t.Errorf("percent = %v, want 100", j.percent)
		}
		if m.rev != 2 {
			t.Errorf("rev = %d, want 2 (log append plus status change)", m.rev)
		}
		if logs := m.Logs(); len(logs) != 1 || logs[0] != marker {
			t.Errorf("Logs() = %q, want exactly the marker line", logs)
		}
	})

	t.Run("canceled job keeps its status but is flagged", func(t *testing.T) {
		m := newTransitionManager(t, ytdlp.Video{ID: "v"})
		j := m.jobs[0]
		j.status = StatusCanceled

		m.handleEvent(j, ytdlp.ProgressEvent{VideoID: "v", Stage: ytdlp.StageDownloading, Line: marker})

		if !j.archiveSeen {
			t.Error("archiveSeen = false, want true")
		}
		if j.status != StatusCanceled {
			t.Errorf("status = %v, want canceled kept", j.status)
		}
		if j.percent != -1 {
			t.Errorf("percent = %v, want the queued sentinel -1", j.percent)
		}
	})

	t.Run("prefix match is case-insensitive", func(t *testing.T) {
		m := newTransitionManager(t, ytdlp.Video{ID: "v"})
		j := m.jobs[0]

		m.handleEvent(j, ytdlp.ProgressEvent{
			VideoID: "v",
			Stage:   ytdlp.StageDownloading,
			Line:    "  " + strings.ToUpper("[download]") + " Video v " + archiveSkipMsg + "  ",
		})

		if !j.archiveSeen {
			t.Error("archiveSeen = false for an uppercased prefix, want true")
		}
		if j.status != StatusSkipped || j.percent != 100 {
			t.Errorf("job = {status:%v percent:%v}, want skipped at 100", j.status, j.percent)
		}
	})

	t.Run("unanchored lookalike is ignored", func(t *testing.T) {
		m := newTransitionManager(t, ytdlp.Video{ID: "v"})
		j := m.jobs[0]
		j.status = StatusRunning

		m.handleEvent(j, ytdlp.ProgressEvent{
			VideoID: "v",
			Stage:   ytdlp.StageDownloading,
			Percent: 10,
			Line:    "payload: " + archiveSkipMsg,
		})

		if j.archiveSeen {
			t.Error("archiveSeen = true for an unanchored lookalike, want false")
		}
		if j.status != StatusRunning {
			t.Errorf("status = %v, want running kept", j.status)
		}
		if j.percent != 10 {
			t.Errorf("percent = %v, want 10 from the downloading event", j.percent)
		}
	})
}

// TestHandleEventStages covers the per-stage transitions and the -1
// sentinel rule: unknown numerics never overwrite known values.
func TestHandleEventStages(t *testing.T) {
	m := newTransitionManager(t, ytdlp.Video{ID: "v"}, ytdlp.Video{ID: "w"})
	j := m.jobs[0]

	m.handleEvent(j, ytdlp.ProgressEvent{
		VideoID:    "v",
		Stage:      ytdlp.StageDownloading,
		Percent:    42.5,
		SpeedBps:   1024,
		ETASeconds: 7,
		Line:       "DL|v|downloading|1|2|NA|1024|7|42.5",
	})
	if j.status != StatusRunning || j.percent != 42.5 || j.speed != 1024 || j.eta != 7 {
		t.Fatalf("after downloading event: %+v, want running at 42.5 percent", j)
	}

	m.handleEvent(j, ytdlp.ProgressEvent{
		VideoID:    "v",
		Stage:      ytdlp.StageDownloading,
		Percent:    -1,
		SpeedBps:   -1,
		ETASeconds: -1,
		Line:       "DL|v|downloading|NA|NA|NA|NA|NA|NA",
	})
	if j.percent != 42.5 || j.speed != 1024 || j.eta != 7 {
		t.Errorf("sentinel event overwrote known values: %+v", j)
	}

	m.handleEvent(j, ytdlp.ProgressEvent{VideoID: "v", Stage: ytdlp.StagePostprocessing, Line: "PP|v|processing"})
	if j.status != StatusMerging {
		t.Errorf("status after postprocessing = %v, want merging", j.status)
	}

	m.handleEvent(j, ytdlp.ProgressEvent{VideoID: "v", Stage: ytdlp.StageFinished, Line: "PP|v|finished"})
	if j.status != StatusMerging {
		t.Errorf("status after finished event = %v, want merging kept until Exit", j.status)
	}

	canceled := m.jobs[1]
	canceled.status = StatusCanceled
	m.handleEvent(canceled, ytdlp.ProgressEvent{
		VideoID: "w",
		Stage:   ytdlp.StageDownloading,
		Percent: 80,
		Line:    "DL|w|downloading|80|100|NA|5|2|80.0",
	})
	if canceled.status != StatusCanceled || canceled.percent != -1 {
		t.Errorf("canceled job was updated: %+v", canceled)
	}

	before := len(m.Logs())
	m.handleEvent(canceled, ytdlp.ProgressEvent{VideoID: "w", Stage: ytdlp.StageFinished, Line: "   "})
	if got := len(m.Logs()); got != before {
		t.Errorf("blank line added a log entry: len(Logs()) = %d, want %d", got, before)
	}
}

// TestFinishJobOutcomes covers every exit of finishJob: clean success with
// and without the archive marker, cancellation signaled through the manager
// flag, the context, or the exit error itself, and a genuine failure.
func TestFinishJobOutcomes(t *testing.T) {
	t.Run("clean exit with archive marker becomes skipped", func(t *testing.T) {
		m := newTransitionManager(t, ytdlp.Video{ID: "v"})
		j := m.jobs[0]
		j.archiveSeen = true

		m.finishJob(j, nil)

		if j.status != StatusSkipped || j.percent != 100 {
			t.Errorf("job = {status:%v percent:%v}, want skipped at 100", j.status, j.percent)
		}
	})

	t.Run("clean exit becomes done", func(t *testing.T) {
		m := newTransitionManager(t, ytdlp.Video{ID: "v"})
		j := m.jobs[0]

		m.finishJob(j, nil)

		if j.status != StatusDone || j.percent != 100 {
			t.Errorf("job = {status:%v percent:%v}, want done at 100", j.status, j.percent)
		}
	})

	t.Run("cancel flag wins over the exit error", func(t *testing.T) {
		m := newTransitionManager(t, ytdlp.Video{ID: "v"})
		m.canceled = true
		j := m.jobs[0]

		m.finishJob(j, errors.New("exit status 1"))

		if j.status != StatusCanceled {
			t.Errorf("status = %v, want canceled", j.status)
		}
		if j.errText != "" {
			t.Errorf("errText = %q, want empty", j.errText)
		}
	})

	t.Run("canceled context wins over the exit error", func(t *testing.T) {
		m := newTransitionManager(t, ytdlp.Video{ID: "v"})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		m.ctx = ctx
		j := m.jobs[0]

		m.finishJob(j, errors.New("signal: killed"))

		if j.status != StatusCanceled {
			t.Errorf("status = %v, want canceled", j.status)
		}
	})

	t.Run("context.Canceled exit error maps to canceled", func(t *testing.T) {
		m := newTransitionManager(t, ytdlp.Video{ID: "v"})
		j := m.jobs[0]

		m.finishJob(j, context.Canceled)

		if j.status != StatusCanceled {
			t.Errorf("status = %v, want canceled", j.status)
		}
	})

	t.Run("genuine failure keeps the bounded error text", func(t *testing.T) {
		m := newTransitionManager(t, ytdlp.Video{ID: "v"})
		j := m.jobs[0]

		m.finishJob(j, errors.New("exit status 3"))

		if j.status != StatusFailed {
			t.Errorf("status = %v, want failed", j.status)
		}
		if j.errText != "exit status 3" {
			t.Errorf("errText = %q, want %q", j.errText, "exit status 3")
		}
	})
}

// TestStartWithNilContextAndNilClient covers two Start guards in one run: a
// nil context is replaced by a background context, and a manager without a
// client fails every queued job with a readable reason instead of hanging.
func TestStartWithNilContextAndNilClient(t *testing.T) {
	m := NewManager(nil, Options{Videos: []ytdlp.Video{{ID: "v1"}, {ID: "v2"}}})

	m.Start(nil)

	select {
	case <-m.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("manager did not finish within 10s")
	}

	for _, snap := range m.Snapshot() {
		if snap.Status != StatusFailed {
			t.Errorf("job %s status = %v, want failed", snap.Video.ID, snap.Status)
		}
		if snap.Err != "download client unavailable" {
			t.Errorf("job %s Err = %q, want %q", snap.Video.ID, snap.Err, "download client unavailable")
		}
	}
}

// TestCancelBeforeStartMarksQueuedCanceled covers the Cancel-while-not-yet-
// started path: Done closes immediately, and the later Start only has to
// observe the cancellation instead of spawning workers.
func TestCancelBeforeStartMarksQueuedCanceled(t *testing.T) {
	m := NewManager(nil, Options{Videos: []ytdlp.Video{{ID: "v1"}, {ID: "v2"}}})

	m.Cancel()

	select {
	case <-m.Done():
	default:
		t.Fatal("Done() is still open right after Cancel() before Start()")
	}

	m.Start(context.Background())

	select {
	case <-m.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("manager did not finish within 10s")
	}

	sum := m.Summary()
	if sum.Total != 2 || sum.Canceled != 2 || sum.Done != 0 || sum.Failed != 0 {
		t.Errorf("Summary() = %+v, want Total 2, Canceled 2", sum)
	}
}

// TestStartUnusableClientFailsJobsAndLogsMkdirError drives runJob and
// failJob through a client whose binary does not exist, with a target
// directory that cannot be created. Both failures must surface: the jobs as
// failed with a bounded message, the directory problem as a log line.
func TestStartUnusableClientFailsJobsAndLogsMkdirError(t *testing.T) {
	dir := t.TempDir()

	// A regular file where the target directory should be makes MkdirAll fail.
	target := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(target, []byte("blocker"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", target, err)
	}

	client := ytdlp.New(filepath.Join(dir, "lhi-absent-ytdlp-binary"), "")
	m := NewManager(client, Options{
		PlaylistURL:      "https://www.youtube.com/playlist?list=PLtest",
		TargetDir:        target,
		Quality:          ytdlp.QBest,
		FilenameTemplate: "%(id)s.%(ext)s",
		Concurrency:      2,
		Videos:           []ytdlp.Video{{Index: 1, ID: "v1"}, {Index: 2, ID: "v2"}},
	})

	m.Start(context.Background())

	select {
	case <-m.Done():
	case <-time.After(30 * time.Second):
		t.Fatal("manager did not finish within 30s")
	}

	sum := m.Summary()
	if sum.Total != 2 || sum.Failed != 2 {
		t.Fatalf("Summary() = %+v, want Total 2, Failed 2", sum)
	}

	for _, snap := range m.Snapshot() {
		if snap.Status != StatusFailed {
			t.Errorf("job %s status = %v, want failed", snap.Video.ID, snap.Status)
		}
		if snap.Err == "" {
			t.Errorf("job %s Err is empty, want the start failure", snap.Video.ID)
		}
	}

	logs := strings.Join(m.Logs(), "\n")
	if !strings.Contains(logs, "cannot create target directory") {
		t.Errorf("Logs() missing the directory failure; got:\n%s", logs)
	}
}
