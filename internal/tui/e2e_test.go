// End-to-end test for the real TUI: teatest drives the application model
// returned by New() while fake yt-dlp and ffmpeg executables stand in for the
// real tools, so the whole workflow runs without network access, without a
// real yt-dlp installation, and without an interactive terminal.
//
// The sandbox is assembled for the host operating system: the fakes get the
// platform's executable suffix, and the fake user directories are exposed
// through the environment variables os.UserHomeDir and os.UserConfigDir read
// on that platform (%USERPROFILE% and %AppData% on Windows, $HOME and
// $HOME/Library/Application Support on macOS, $HOME plus XDG_CONFIG_HOME on
// Linux and other Unix systems). The journeys themselves are OS-independent,
// so the suite runs on Windows, Linux and macOS alike.
//
// The two subtests share one sandbox and must run in order: the second
// resumes against the config and archive written by the first.

package tui

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	teatest "github.com/charmbracelet/x/exp/teatest/v2"
)

const (
	// e2ePlaylistURL is typed into the form. The fake yt-dlp answers every
	// flat-playlist request with the same deterministic three-entry JSON.
	e2ePlaylistURL = "https://www.youtube.com/playlist?list=PLtest"

	// WaitFor bounds, per step. Process spawns (tool check, playlist fetch,
	// three downloads) are the slow parts; everything else renders within a
	// frame or two.
	e2eWaitFrames = 5 * time.Second
	e2eWaitFetch  = 15 * time.Second
	e2eWaitFinish = 30 * time.Second
)

// exeSuffix returns the executable suffix of the host platform: ".exe" on
// Windows, empty elsewhere. go build appends it to the -o path, and the tool
// lookup expects the same name it would find for a real installation.
func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// fakeTools is the sandbox built for one TestE2E run: the directory holding
// the fake executables plus the fake user directories the application is
// pointed at through the environment.
type fakeTools struct {
	binDir    string // PATH entry: the fake yt-dlp and ffmpeg live here
	homeDir   string // fake user home: the default TargetDir sits under it
	appData   string // fake %AppData% (Windows config root)
	configDir string // user config dir the app resolves on this host
}

// hostConfigDir mirrors the directory os.UserConfigDir resolves to once
// installEnv has pointed the process at the sandbox: %AppData% on Windows,
// $HOME/Library/Application Support on macOS, and XDG_CONFIG_HOME - which
// installEnv pins to $HOME/.config - on Linux and other Unix systems. It is
// the single source of truth for the config path the download journey
// asserts.
func hostConfigDir(homeDir, appData string) string {
	const appDir = "lhi-youtube-downloader"
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(appData, appDir)
	case "darwin":
		return filepath.Join(homeDir, "Library", "Application Support", appDir)
	default:
		return filepath.Join(homeDir, ".config", appDir)
	}
}

// buildFakeTools builds the fake yt-dlp (shared with the jobs integration
// tests) and the fake ffmpeg into a temporary directory. A build failure
// skips the calling test: the harness must never fail because the environment
// cannot run `go build`.
func buildFakeTools(t *testing.T) *fakeTools {
	t.Helper()

	binDir := t.TempDir()
	root := e2eModuleRoot(t)

	build := func(exe, pkg string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "go", "build", "-o", filepath.Join(binDir, exe), pkg)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("cannot build %s (test harness unavailable): %v\n%s", pkg, err, out)
		}
	}
	build("yt-dlp"+exeSuffix(), "./internal/jobs/testdata/fakeytdlp")
	build("ffmpeg"+exeSuffix(), "./internal/tui/testdata/fakeffmpeg")

	homeDir := filepath.Join(binDir, "home")
	appData := filepath.Join(binDir, "appdata")
	return &fakeTools{
		binDir:    binDir,
		homeDir:   homeDir,
		appData:   appData,
		configDir: hostConfigDir(homeDir, appData),
	}
}

// installEnv points the process environment at the fakes and the temporary
// user directories. It must be called before New(): config.Load resolves the
// config path from os.UserConfigDir and config.Default resolves the default
// target directory from os.UserHomeDir, so the sandbox pins whichever
// variables those functions read on this host. The FAKE_* knobs are pinned so
// an ambient value cannot make the shared fake yt-dlp nondeterministic.
func (f *fakeTools) installEnv(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", f.binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	switch runtime.GOOS {
	case "windows":
		t.Setenv("AppData", f.appData)
		t.Setenv("USERPROFILE", f.homeDir)
	case "darwin":
		// os.UserHomeDir reads $HOME; os.UserConfigDir goes straight to
		// $HOME/Library/Application Support and ignores XDG_CONFIG_HOME.
		t.Setenv("HOME", f.homeDir)
	default:
		// os.UserConfigDir prefers an absolute XDG_CONFIG_HOME over
		// $HOME/.config, which is exactly where hostConfigDir points.
		t.Setenv("HOME", f.homeDir)
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(f.homeDir, ".config"))
	}
	t.Setenv("FAKE_SLEEP_MS", "0")
	t.Setenv("FAKE_STDERR", "0")
	t.Setenv("FAKE_EXIT", "0")
}

// e2eModuleRoot walks up from the test's working directory (the package
// directory) to the directory holding go.mod, which is the working directory
// `go build` needs for the module-relative fake package paths.
func e2eModuleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Skipf("cannot determine working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Skipf("go.mod not found at or above %s", dir)
		}
		dir = parent
	}
}

// e2eEnter and e2eTab build the key press messages the TUI expects. Both are
// special C0 keys, so only Code is set (Text stays empty).
func e2eEnter() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyEnter} }
func e2eTab() tea.KeyPressMsg   { return tea.KeyPressMsg{Code: tea.KeyTab} }

// waitForOutput blocks until every want string appeared in the program output
// accumulated since the previous waitForOutput call.
func waitForOutput(t *testing.T, tm *teatest.TestModel, d time.Duration, wants ...string) {
	t.Helper()
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		for _, want := range wants {
			if !bytes.Contains(out, []byte(want)) {
				return false
			}
		}
		return true
	}, teatest.WithDuration(d), teatest.WithCheckInterval(50*time.Millisecond))
}

// e2eSubmitForm advances focus from the playlist URL through the seven
// remaining fields to the filename template and presses enter, which is the
// form's submit trigger.
func e2eSubmitForm(tm *teatest.TestModel) {
	for i := 0; i < 7; i++ {
		tm.Send(e2eTab())
	}
	tm.Send(e2eEnter())
}

// e2eQuit presses enter on the done screen (quit), waits for the program to
// finish, and returns the final application model for state assertions. The
// model keeps the done screen state, so it can be inspected after quitting.
func e2eQuit(t *testing.T, tm *teatest.TestModel) appModel {
	t.Helper()
	tm.Send(e2eEnter())
	tm.WaitFinished(t, teatest.WithFinalTimeout(10*time.Second))
	fm := tm.FinalModel(t)
	app, ok := fm.(appModel)
	if !ok {
		t.Fatalf("final model has type %T, want tui.appModel", fm)
	}
	return app
}

// nonBlankLines returns the non-blank lines of s, trimmed of the trailing
// newline yt-dlp archive files end with.
func nonBlankLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

// e2eDownloadJourney is the happy path: check -> form -> fetch -> pick ->
// download all three fake videos -> done.
func e2eDownloadJourney(t *testing.T, f *fakeTools) {
	tm := teatest.NewTestModel(t, New(), teatest.WithInitialTermSize(120, 40))

	// Setup check: both fake tools must be found and reported OK.
	waitForOutput(t, tm, e2eWaitFrames, "lhi-youtube-downloader", "yt-dlp", "ffmpeg", "[OK]")

	// Enter opens the configuration form.
	tm.Send(e2eEnter())
	waitForOutput(t, tm, e2eWaitFrames, "Download configuration", "Playlist URL")

	// Type the playlist URL into the focused first field, advance to the
	// last field and submit.
	tm.Type(e2ePlaylistURL)
	e2eSubmitForm(tm)

	// The fake playlist lists three entries; the pick screen shows them
	// with every video selected by default.
	waitForOutput(t, tm, e2eWaitFetch, "Videos", "First Video")

	// Enter starts the downloads (all three selected).
	tm.Send(e2eEnter())
	waitForOutput(t, tm, e2eWaitFrames, "Downloading")

	// All three fake downloads exit 0; the app lands on the done screen.
	waitForOutput(t, tm, e2eWaitFinish, "All downloads finished")

	app := e2eQuit(t, tm)
	if app.screen != scrDone {
		t.Errorf("final screen = %v, want scrDone", app.screen)
	}
	if got := app.done.summary.Done; got != 3 {
		t.Errorf("summary.Done = %d, want 3", got)
	}
	if got := app.done.summary.Skipped; got != 0 {
		t.Errorf("summary.Skipped = %d, want 0 on a fresh archive", got)
	}
	if got := app.done.summary.Total; got != 3 {
		t.Errorf("summary.Total = %d, want 3", got)
	}

	// On disk: one archive line per successful fake download. The fake
	// derives the logged id from the -I index, not from the playlist JSON
	// id, so the third entry (playlist_index 7, id vid003) is recorded as
	// vid007. The app is correct here; the id divergence is a property of
	// the shared fakeytdlp.
	archive := filepath.Join(f.homeDir, "Downloads", "YouTube", ".yt-dlp-archive.txt")
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatalf("download archive was not written: %v", err)
	}
	if lines := nonBlankLines(string(data)); len(lines) != 3 {
		t.Errorf("archive has %d lines, want 3 (one per fake download):\n%s", len(lines), data)
	}
	for _, want := range []string{"youtube vid001", "youtube vid002", "youtube vid007"} {
		if n := strings.Count(string(data), want); n != 1 {
			t.Errorf("archive contains %q %d times, want exactly 1:\n%s", want, n, data)
		}
	}

	// The configuration must have been persisted: the resume journey
	// relies on it, and it proves saveCmd ran.
	cfgPath := filepath.Join(f.configDir, "config.json")
	cfgData, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("config was not persisted: %v", err)
	}
	if !strings.Contains(string(cfgData), e2ePlaylistURL) {
		t.Errorf("config.json does not contain the submitted playlist URL:\n%s", cfgData)
	}
}

// e2eResumeJourney proves config persistence and archive-based resume: the
// URL is prefilled from the config written by the download journey, and the
// archive written there makes the manager skip already-downloaded videos
// before spawning any process for them.
func e2eResumeJourney(t *testing.T, f *fakeTools) {
	tm := teatest.NewTestModel(t, New(), teatest.WithInitialTermSize(120, 40))

	waitForOutput(t, tm, e2eWaitFrames, "lhi-youtube-downloader", "[OK]")

	tm.Send(e2eEnter())
	waitForOutput(t, tm, e2eWaitFrames, "Download configuration", "Playlist URL")
	// The URL field is already populated from config.json, so no typing is
	// needed: advance to the last field and submit.
	e2eSubmitForm(tm)

	waitForOutput(t, tm, e2eWaitFetch, "Videos", "First Video")
	tm.Send(e2eEnter())
	waitForOutput(t, tm, e2eWaitFinish, "All downloads finished")

	app := e2eQuit(t, tm)
	if app.screen != scrDone {
		t.Errorf("final screen = %v, want scrDone", app.screen)
	}

	// The archive holds vid001, vid002 and vid007 (see the download
	// journey), while the playlist offers vid001, vid002 and vid003. The
	// manager skips the two ids it finds archived and downloads vid003
	// again, which the fake logs as vid007 because its id derives from the
	// -I index (7) rather than the playlist JSON id.
	if got := app.done.summary.Skipped; got != 2 {
		t.Errorf("summary.Skipped = %d, want 2 (vid001 and vid002 are archived)", got)
	}
	if got := app.done.summary.Done; got != 1 {
		t.Errorf("summary.Done = %d, want 1 (vid003 is not archived)", got)
	}
	if got := app.done.summary.Total; got != 3 {
		t.Errorf("summary.Total = %d, want 3", got)
	}

	// The re-download appends a second vid007 line to the archive.
	archive := filepath.Join(f.homeDir, "Downloads", "YouTube", ".yt-dlp-archive.txt")
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatalf("download archive missing after resume: %v", err)
	}
	if n := strings.Count(string(data), "youtube vid007"); n != 2 {
		t.Errorf("archive contains 'youtube vid007' %d times, want 2 (initial download + re-download):\n%s", n, data)
	}
}

// TestE2E drives the real TUI end to end against fake yt-dlp and ffmpeg
// binaries. It runs on Windows, Linux and macOS; only -short skips it.
func TestE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test builds binaries and spawns processes")
	}

	f := buildFakeTools(t)
	f.installEnv(t)

	if t.Run("download journey", func(t *testing.T) {
		e2eDownloadJourney(t, f)
	}) {
		t.Run("resume and skip journey", func(t *testing.T) {
			e2eResumeJourney(t, f)
		})
	} else {
		t.Log("resume journey not run: download journey failed and left incomplete sandbox files")
	}
}
