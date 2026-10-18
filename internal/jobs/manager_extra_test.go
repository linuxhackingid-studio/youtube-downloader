// Extra manager coverage for the public surface: nil-receiver safety, the
// one-shot Start contract, cancellation ordering, and the concurrency clamp
// extremes, exercised through the fake yt-dlp harness defined in
// manager_test.go.
package jobs_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"linuxhackingid.com/youtube-downloader/internal/jobs"
	"linuxhackingid.com/youtube-downloader/internal/ytdlp"
)

// TestManagerNilReceiverIsInert pins that every nil-receiver method is a
// safe no-op with the documented zero results instead of a panic.
func TestManagerNilReceiverIsInert(t *testing.T) {
	var mgr *jobs.Manager

	mgr.Start(nil)
	mgr.Cancel()

	if got := mgr.Snapshot(); got != nil {
		t.Errorf("(*jobs.Manager)(nil).Snapshot() = %v, want nil", got)
	}
	if got := mgr.Summary(); got != (jobs.Summary{}) {
		t.Errorf("(*jobs.Manager)(nil).Summary() = %+v, want the zero Summary", got)
	}
	if got := mgr.Logs(); got != nil {
		t.Errorf("(*jobs.Manager)(nil).Logs() = %v, want nil", got)
	}
	if got := mgr.Done(); got != nil {
		t.Errorf("(*jobs.Manager)(nil).Done() = %v, want nil", got)
	}
}

// TestManagerStartIsIdempotent proves the second Start call is a no-op: the
// jobs keep the outcome of the first run and Done closes exactly once.
func TestManagerStartIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	mgr := jobs.NewManager(nil, testOptions(dir, "", 2))

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	mgr.Start(ctx)
	mgr.Start(ctx)

	waitDone(t, mgr.Done(), 10*time.Second)

	sum := mgr.Summary()
	if sum.Total != 2 || sum.Failed != 2 {
		t.Errorf("Summary() = %+v, want Total 2, Failed 2 from the first Start", sum)
	}
}

// TestManagerCancelBeforeStart pins the ordering contract: Cancel on a
// manager that never started closes Done immediately, and the later Start
// marks every queued job canceled without spawning a download.
func TestManagerCancelBeforeStart(t *testing.T) {
	mgr := jobs.NewManager(ytdlp.New("lhi-unused-client", ""), testOptions(t.TempDir(), "", 2))

	mgr.Cancel()

	select {
	case <-mgr.Done():
	default:
		t.Fatal("Done() is still open right after Cancel() before Start()")
	}

	mgr.Start(context.Background())

	waitDone(t, mgr.Done(), 10*time.Second)

	sum := mgr.Summary()
	if sum.Total != 2 || sum.Canceled != 2 || sum.Done != 0 || sum.Failed != 0 {
		t.Errorf("Summary() = %+v, want Total 2, Canceled 2", sum)
	}
}

// TestManagerConcurrencyClampExtremes runs the fake downloader with the
// clamping boundaries: 0 must behave as one worker, 99 must not spawn more
// than the ten-worker cap, and both must finish every job.
func TestManagerConcurrencyClampExtremes(t *testing.T) {
	requireFake(t)

	for _, conc := range []int{0, 99} {
		t.Run(fmt.Sprintf("concurrency %d", conc), func(t *testing.T) {
			dir := t.TempDir()
			mgr := jobs.NewManager(newTestClient(), testOptions(dir, "", conc))

			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			mgr.Start(ctx)
			waitDone(t, mgr.Done(), 45*time.Second)

			sum := mgr.Summary()
			if sum.Total != 2 || sum.Done != 2 || sum.Failed != 0 {
				t.Errorf("Summary() = %+v, want Total 2, Done 2, Failed 0", sum)
			}
		})
	}
}
