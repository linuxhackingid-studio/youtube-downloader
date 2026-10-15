// Package jobs_test exercises the jobs manager end to end against the fake
// yt-dlp executable in testdata/fakeytdlp. No network access and no real
// yt-dlp installation are required.
package jobs_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"linuxhackingid.com/youtube-downloader/internal/jobs"
	"linuxhackingid.com/youtube-downloader/internal/ytdlp"
)

const (
	playlistURL      = "https://www.youtube.com/playlist?list=PLtest"
	filenameTemplate = "%(id)s.%(ext)s"
)

var (
	fakePath     string // path to the built fake yt-dlp executable
	fakeBuildErr error  // non-nil when the fake binary could not be built
)

// TestMain builds the fake yt-dlp binary once for the whole package. A build
// failure is recorded and turns every test into a skip: the suite must never
// fail because the harness itself is unavailable.
func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "fakeytdlp-*")
	if err != nil {
		fakeBuildErr = fmt.Errorf("create temp dir: %w", err)
	} else if pkgDir := jobsPackageDir(); pkgDir == "" {
		fakeBuildErr = errors.New("cannot locate the jobs package directory")
	} else {
		out := filepath.Join(tmpDir, "fake-ytdlp.exe")
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		cmd := exec.CommandContext(ctx, "go", "build", "-o", out, "./testdata/fakeytdlp")
		cmd.Dir = pkgDir
		buildOut, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			fakeBuildErr = fmt.Errorf("go build ./testdata/fakeytdlp: %w\n%s", err, buildOut)
		} else {
			fakePath = out
		}
	}

	code := m.Run()

	if tmpDir != "" {
		_ = os.RemoveAll(tmpDir)
	}
	os.Exit(code)
}

// jobsPackageDir returns the directory containing this test file, which is
// also where ./testdata/fakeytdlp lives.
func jobsPackageDir() string {
	if _, file, _, ok := runtime.Caller(0); ok {
		if dir := filepath.Dir(file); filepath.IsAbs(dir) {
			return dir
		}
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return ""
}

// requireFake skips the calling test when the fake binary is unavailable.
func requireFake(t *testing.T) {
	t.Helper()
	if fakeBuildErr != nil {
		t.Skipf("fake yt-dlp unavailable: %v", fakeBuildErr)
	}
	if fakePath == "" {
		t.Skip("fake yt-dlp path is unset")
	}
}

// newTestClient returns a client bound to the fake binary.
func newTestClient() *ytdlp.Client { return ytdlp.New(fakePath, "") }

// testOptions builds the manager options shared by the integration tests.
func testOptions(dir, archivePath string, concurrency int) jobs.Options {
	return jobs.Options{
		PlaylistURL:      playlistURL,
		TargetDir:        dir,
		Quality:          ytdlp.QBest,
		FilenameTemplate: filenameTemplate,
		ArchivePath:      archivePath,
		Concurrency:      concurrency,
		Videos: []ytdlp.Video{
			{Index: 1, ID: "vid001"},
			{Index: 2, ID: "vid002"},
		},
	}
}

// waitDone blocks until done closes or the timeout elapses.
func waitDone(t *testing.T, done <-chan struct{}, timeout time.Duration) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatalf("manager did not finish within %s", timeout)
	}
}

// readArchiveIDs parses "youtube <id>" lines from a yt-dlp archive file.
func readArchiveIDs(t *testing.T, path string) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read archive %s: %v", path, err)
	}
	ids := make(map[string]bool)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.EqualFold(fields[0], "youtube") {
			ids[fields[1]] = true
		}
	}
	return ids
}

// TestFetchPlaylist_Fake checks the client's flat-playlist parsing against
// the fake's canned JSON: fallbacks for null fields and playlist_index used
// as the authoritative position.
func TestFetchPlaylist_Fake(t *testing.T) {
	requireFake(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pl, err := newTestClient().FetchPlaylist(ctx, playlistURL, "")
	if err != nil {
		t.Fatalf("FetchPlaylist: %v", err)
	}
	if pl.ID != "PLtest" {
		t.Errorf("Playlist.ID = %q, want PLtest", pl.ID)
	}
	if pl.Title != "Test Playlist" {
		t.Errorf("Playlist.Title = %q, want %q", pl.Title, "Test Playlist")
	}
	if len(pl.Videos) != 3 {
		t.Fatalf("len(Videos) = %d, want 3", len(pl.Videos))
	}

	v1 := pl.Videos[0]
	if v1.ID != "vid001" || v1.Title != "First Video" || v1.Index != 1 {
		t.Errorf("video 1 = %+v, want id vid001, title First Video, index 1", v1)
	}
	if v1.URL != "https://www.youtube.com/watch?v=vid001" {
		t.Errorf("video 1 URL = %q, want the JSON url kept verbatim", v1.URL)
	}
	if v1.Duration != 61 {
		t.Errorf("video 1 Duration = %v, want 61", v1.Duration)
	}

	v2 := pl.Videos[1]
	if v2.ID != "vid002" {
		t.Errorf("video 2 ID = %q, want vid002", v2.ID)
	}
	if v2.Title != "Video vid002" {
		t.Errorf("video 2 Title = %q, want fallback %q", v2.Title, "Video vid002")
	}
	if !strings.Contains(v2.URL, "watch?v=vid002") {
		t.Errorf("video 2 URL = %q, want fallback watch URL for vid002", v2.URL)
	}
	if v2.Duration != 0 {
		t.Errorf("video 2 Duration = %v, want 0 for null duration", v2.Duration)
	}
	if v2.Index != 2 {
		t.Errorf("video 2 Index = %d, want 2", v2.Index)
	}

	v3 := pl.Videos[2]
	if v3.Title != "Third" {
		t.Errorf("video 3 Title = %q, want Third", v3.Title)
	}
	if v3.Index != 7 {
		t.Errorf("video 3 Index = %d, want 7 (playlist_index, not array position)", v3.Index)
	}
	if v3.Duration != 125.5 {
		t.Errorf("video 3 Duration = %v, want 125.5", v3.Duration)
	}
}

// TestFetchPlaylist_NonZeroExit checks that a failing playlist listing
// surfaces as an *ytdlp.ExitError carrying the code and stderr text.
func TestFetchPlaylist_NonZeroExit(t *testing.T) {
	requireFake(t)
	t.Setenv("FAKE_PLAYLIST_EXIT", "1")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := newTestClient().FetchPlaylist(ctx, playlistURL, "")
	if err == nil {
		t.Fatal("FetchPlaylist succeeded, want error")
	}

	var exitErr *ytdlp.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error type = %T (%v), want *ytdlp.ExitError", err, err)
	}
	if exitErr.Code != 1 {
		t.Errorf("ExitError.Code = %d, want 1", exitErr.Code)
	}
	if !strings.Contains(exitErr.Stderr, "fake playlist failure") {
		t.Errorf("ExitError.Stderr = %q, want fake error text", exitErr.Stderr)
	}
}

// TestStartDownload_Events checks the handle's event stream end to end: the
// fake's DL/PP progress lines, a raw stderr line, the target-URL routing and
// the nil-success sentinel on Exit.
func TestStartDownload_Events(t *testing.T) {
	requireFake(t)
	t.Setenv("FAKE_STDERR", "1")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	h, err := newTestClient().StartDownload(ctx, ytdlp.DownloadOpts{
		PlaylistURL:      playlistURL,
		Video:            ytdlp.Video{Index: 1, ID: "vid001"},
		TargetDir:        t.TempDir(),
		Quality:          ytdlp.QBest,
		FilenameTemplate: filenameTemplate,
	})
	if err != nil {
		t.Fatalf("StartDownload: %v", err)
	}

	var events []ytdlp.ProgressEvent
	for ev := range h.Events {
		events = append(events, ev)
	}

	v, ok := <-h.Exit
	if !ok {
		t.Fatal("Exit closed without delivering a value")
	}
	if v != nil {
		t.Fatalf("Exit = %v, want nil", v)
	}

	var sawDownloading, sawFinished, sawDestination, sawVideoID, sawStderr bool
	for _, ev := range events {
		if ev.Stage == ytdlp.StageDownloading {
			sawDownloading = true
		}
		if ev.Stage == ytdlp.StageFinished {
			sawFinished = true
		}
		if strings.Contains(ev.Line, "Destination") {
			sawDestination = true
		}
		if ev.VideoID == "vid001" {
			sawVideoID = true
		}
		if strings.Contains(ev.Line, "fake stderr line") {
			sawStderr = true
		}
	}

	if !sawDownloading {
		t.Errorf("no StageDownloading event; got %v", events)
	}
	if !sawFinished {
		t.Errorf("no StageFinished event; got %v", events)
	}
	if !sawDestination {
		t.Errorf("no raw line containing %q; got %v", "Destination", events)
	}
	if !sawVideoID {
		t.Errorf("no event routed to VideoID vid001; got %v", events)
	}
	if !sawStderr {
		t.Errorf("no stderr event containing %q; got %v", "fake stderr line", events)
	}
}

// TestManager_HappyPath_and_Archive runs the manager twice: the first run
// downloads both videos and records them in the archive, the second skips
// both because the archive already lists them.
func TestManager_HappyPath_and_Archive(t *testing.T) {
	requireFake(t)

	dir := t.TempDir()
	arch := filepath.Join(dir, "arch.txt")

	mgr := jobs.NewManager(newTestClient(), testOptions(dir, arch, 2))

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	mgr.Start(ctx)
	waitDone(t, mgr.Done(), 45*time.Second)

	sum := mgr.Summary()
	if sum.Total != 2 || sum.Done != 2 || sum.Failed != 0 {
		t.Fatalf("first run Summary = %+v, want Total 2, Done 2, Failed 0", sum)
	}

	ids := readArchiveIDs(t, arch)
	for _, want := range []string{"vid001", "vid002"} {
		if !ids[want] {
			t.Errorf("archive missing %q; parsed ids: %v", want, ids)
		}
	}

	logs := mgr.Logs()
	if len(logs) == 0 {
		t.Error("Logs() is empty after a successful run")
	}
	if len(logs) > 200 {
		t.Errorf("len(Logs()) = %d, want <= 200", len(logs))
	}

	// Second run against the same archive: every video must be skipped
	// without spawning a download.
	mgr2 := jobs.NewManager(newTestClient(), testOptions(dir, arch, 2))

	ctx2, cancel2 := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel2()

	mgr2.Start(ctx2)
	waitDone(t, mgr2.Done(), 45*time.Second)

	sum2 := mgr2.Summary()
	if sum2.Total != 2 || sum2.Skipped != 2 || sum2.Done != 0 {
		t.Fatalf("second run Summary = %+v, want Total 2, Skipped 2, Done 0", sum2)
	}
}

// TestManager_Cancel cancels a running manager mid-download: no job may fail
// and every job must end canceled, with the log ring staying bounded.
func TestManager_Cancel(t *testing.T) {
	requireFake(t)
	t.Setenv("FAKE_SLEEP_MS", "5000")

	dir := t.TempDir()
	mgr := jobs.NewManager(newTestClient(), testOptions(dir, "", 1))

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	mgr.Start(ctx)
	time.Sleep(300 * time.Millisecond)
	mgr.Cancel()
	waitDone(t, mgr.Done(), 15*time.Second)

	sum := mgr.Summary()
	if sum.Failed != 0 {
		t.Errorf("Summary.Failed = %d, want 0 (%+v)", sum.Failed, sum)
	}
	if sum.Canceled < 1 {
		t.Errorf("Summary.Canceled = %d, want >= 1 (%+v)", sum.Canceled, sum)
	}
	if sum.Done+sum.Canceled != 2 {
		t.Errorf("Done+Canceled = %d, want 2 (%+v)", sum.Done+sum.Canceled, sum)
	}

	// The fake sleeps before writing any output and cancellation happens
	// inside that sleep, so the log ring may legitimately be empty here;
	// only its bound is guaranteed.
	logs := mgr.Logs()
	if len(logs) > 200 {
		t.Errorf("len(Logs()) = %d, want <= 200", len(logs))
	}
}

// TestManager_SnapshotShape checks the snapshot contract after a successful
// run: one entry per job, original video identity preserved, and 100 percent
// for done jobs.
func TestManager_SnapshotShape(t *testing.T) {
	requireFake(t)

	dir := t.TempDir()
	mgr := jobs.NewManager(newTestClient(), testOptions(dir, "", 2))

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	mgr.Start(ctx)
	waitDone(t, mgr.Done(), 45*time.Second)

	snaps := mgr.Snapshot()
	if len(snaps) != 2 {
		t.Fatalf("len(Snapshot()) = %d, want 2", len(snaps))
	}

	byID := make(map[string]jobs.JobSnapshot, len(snaps))
	for _, s := range snaps {
		byID[s.Video.ID] = s
	}
	for _, id := range []string{"vid001", "vid002"} {
		s, ok := byID[id]
		if !ok {
			t.Errorf("snapshot has no job with Video.ID %q; ids: %v", id, snapshotIDs(snaps))
			continue
		}
		if s.Status != jobs.StatusDone {
			t.Errorf("job %s Status = %v, want done", id, s.Status)
		}
		if s.Percent != 100 {
			t.Errorf("job %s Percent = %v, want 100", id, s.Percent)
		}
	}
}

// snapshotIDs returns the video ids of the snapshots for diagnostics.
func snapshotIDs(snaps []jobs.JobSnapshot) []string {
	ids := make([]string, 0, len(snaps))
	for _, s := range snaps {
		ids = append(ids, s.Video.ID)
	}
	return ids
}
