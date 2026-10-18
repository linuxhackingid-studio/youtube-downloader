package tui

import (
	"context"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"linuxhackingid.com/youtube-downloader/internal/config"
	"linuxhackingid.com/youtube-downloader/internal/jobs"
	"linuxhackingid.com/youtube-downloader/internal/ytdlp"
)

// screen identifies the active screen of the application.
type screen int

// Screen states, in workflow order.
const (
	scrCheck screen = iota
	scrForm
	scrLoading
	scrPick
	scrDownload
	scrDone
)

// playlistMsg carries the outcome of an asynchronous playlist fetch. seq ties
// the result to the fetch that produced it so superseded results are ignored.
type playlistMsg struct {
	pl  *ytdlp.Playlist
	err error
	seq int
}

const (
	// fetchTimeout bounds one playlist fetch so a hung yt-dlp cannot wedge
	// the TUI on the loading screen forever.
	fetchTimeout = 180 * time.Second

	// defaultViewWidth and defaultViewHeight are used for rendering before
	// the first tea.WindowSizeMsg arrives.
	defaultViewWidth  = 80
	defaultViewHeight = 24
)

// appModel is the root model: it owns every screen sub-model and all
// navigation between them. The zero value is not usable; construct it with
// New.
type appModel struct {
	screen screen
	width  int
	height int

	cfg    config.Config
	client *ytdlp.Client

	check   checkModel
	form    formModel
	loading loadingModel
	pick    pickModel
	dl      downloadModel
	done    doneModel

	mgr           *jobs.Manager
	confirmCancel bool

	// fetchSeq and fetchCancel track the in-flight playlist fetch: seq
	// identifies the current request, cancel abandons it on esc.
	fetchSeq    int
	fetchCancel context.CancelFunc
}

// New returns the root model, starting on the setup check. Configuration
// loading is graceful: a missing or corrupt file yields defaults, never an
// error.
func New() tea.Model {
	cfg, _ := config.Load()
	return appModel{
		cfg:   cfg,
		check: newCheckModel(),
		form:  newFormModel(cfg),
	}
}

// Init starts the setup check together with its spinner and asks the terminal
// for its background color.
//
// tea.RequestBackgroundColor is a command constructor in bubbletea v2, so it
// travels through tea.Batch like any other command and makes the terminal
// answer with a tea.BackgroundColorMsg. Until that reply arrives the dark
// palette stays active.
func (m appModel) Init() tea.Cmd {
	return tea.Batch(m.check.Init(), tea.RequestBackgroundColor)
}

// Update routes every message: window sizes are stored, global and per-screen
// keys drive navigation, playlist fetch results move the workflow forward,
// and everything else is forwarded to the active screen's sub-model. Window
// size messages are recorded first and then travel the forwarding path like
// any other message, so the active sub-model also learns the terminal size it
// must lay itself out for (and can warm its own caches) without being
// delivered to twice.
func (m appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = size.Width
		m.height = size.Height
	}

	switch msg := msg.(type) {
	case playlistMsg:
		return m.handlePlaylistMsg(msg)

	case formSubmitMsg:
		// The form itself decides when to submit: it emits this only
		// from enter on its last field, after every earlier enter has
		// advanced focus.
		if m.screen != scrForm {
			return m, nil
		}
		return m.submitForm()

	case tea.BackgroundColorMsg:
		// The terminal answered the theme probe. A reply that matches the
		// active palette changes nothing, so the rebuild below runs only on
		// a real flip: ApplyTheme resets the shared styles, the spinners
		// carry the old palette as a copied style, and both the pick row
		// cache and the download screen's memoized frame embed it in
		// rendered escape sequences.
		if msg.IsDark() == ThemeIsDark() {
			return m, nil
		}
		ApplyTheme(msg.IsDark())
		m.check = m.check.Retheme()
		m.loading = m.loading.Retheme()
		m.pick = m.pick.Retheme()
		m.dl.Invalidate()

		// Restart the animation of the visible spinner, if any.
		switch m.screen {
		case scrCheck:
			return m, m.check.Tick()
		case scrLoading:
			return m, m.loading.Tick()
		}
		return m, nil

	case tea.QuitMsg, tea.InterruptMsg:
		// Verified against bubbletea v2.1.0 (tea.go): the event loop consumes
		// QuitMsg and InterruptMsg itself - "case QuitMsg: return model, nil"
		// (line 789) and "case InterruptMsg: return model, ErrInterrupted"
		// (line 792) - so Update is never reached with either message and
		// these cases are defence in depth only. The shutdown that actually
		// runs is the inline one in every exit path below, because a
		// tea.Quit returned from this method never comes back to Update.
		m = m.shutdown()
		return m, tea.Quit

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}

	return m.forward(msg)
}

// handleKey dispatches a key press. Ctrl+C is the only global key: on the
// download screen it arms the cancel confirmation, everywhere else it quits
// immediately. Navigation keys are owned here; unhandled keys fall through to
// the active sub-model.
func (m appModel) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()

	if k == "ctrl+c" {
		if m.screen == scrDownload {
			m.confirmCancel = true
			return m, nil
		}
		return m.quit()
	}

	switch m.screen {
	case scrCheck:
		switch k {
		case "q":
			return m.quit()
		case "enter":
			if m.check.Ready() {
				ytdlpPath, ffmpegPath := m.check.ToolPaths()
				m.client = ytdlp.New(ytdlpPath, ffmpegPath)
				m = m.clearFormErr()
				m.screen = scrForm
				return m, m.form.Init()
			}
		}

	case scrForm:
		// Enter is owned by the form: it advances focus, and only the
		// last field emits formSubmitMsg. No app-level interception.
		if k == "esc" {
			m.screen = scrCheck
			return m, nil
		}

	case scrLoading:
		switch k {
		case "q":
			return m.quit()
		case "esc":
			// Abandon the fetch and cancel its context; a late
			// playlistMsg is dropped by the screen+seq guard.
			if m.fetchCancel != nil {
				m.fetchCancel()
				m.fetchCancel = nil
			}
			m = m.clearFormErr()
			m.screen = scrForm
			return m, m.form.Init()
		}

	case scrPick:
		// While the search field is focused it owns every key, including
		// enter and esc, so filtering cannot accidentally start downloads
		// or navigate away.
		if m.pick.Searching() {
			return m.forward(msg)
		}
		switch k {
		case "enter":
			if m.pick.SelectedCount() == 0 {
				m.pick = m.pick.SetHint("select at least one video")
				return m, nil
			}
			return m.startDownloads()
		case "esc":
			m = m.clearFormErr()
			m.screen = scrForm
			return m, m.form.Init()
		}

	case scrDownload:
		if m.confirmCancel {
			switch k {
			case "y":
				// Cancel runs taskkill per handle; keep it off the
				// event loop so the TUI does not freeze.
				m.confirmCancel = false
				mgr := m.mgr
				return m, func() tea.Msg { mgr.Cancel(); return nil }
			case "n", "esc":
				m.confirmCancel = false
			}
			return m, nil
		}
		if k == "esc" {
			m.confirmCancel = true
			return m, nil
		}

	case scrDone:
		switch k {
		case "q", "enter":
			return m.quit()
		case "esc", "r":
			m.form = newFormModel(m.cfg)
			m = m.clearFormErr()
			m.screen = scrForm
			return m, m.form.Init()
		case "o":
			// Opening the output folder is a fire-and-forget launch: it
			// never blocks the event loop, and a failure is reported on
			// the summary's status line.
			m.done = m.done.OpenFolder()
			return m, nil
		}
		return m, nil
	}

	return m.forward(msg)
}

// quit shuts every child process down and then ends the program. Every exit
// path funnels through it; the shutdown runs before tea.Quit is returned
// because the Bubble Tea event loop swallows QuitMsg without ever calling
// Update (see the QuitMsg case in Update), so this is the only place where
// the download children can still be canceled.
func (m appModel) quit() (tea.Model, tea.Cmd) {
	m = m.shutdown()
	return m, tea.Quit
}

// Shutdown terminates every child process the model started: the jobs manager
// (which kills the yt-dlp and ffmpeg process trees - taskkill /T /F per handle
// on Windows, process-group SIGKILL on Unix) and an in-flight playlist fetch.
// It is safe on a zero model, with no manager and no fetch, and safe to call
// twice: Manager.Cancel is idempotent and a context cancel func may be called
// repeatedly.
//
// cmd/ytd calls it on the model returned by Program.Run, because bubbletea
// consumes QuitMsg and InterruptMsg inside its own event loop (see the QuitMsg
// case in Update): on SIGTERM, a systemd stop or a non-TTY Ctrl+C the app never
// reaches an exit path in Update, and only this call can take the children
// down with the process.
func (m appModel) Shutdown() {
	if m.mgr != nil {
		m.mgr.Cancel()
	}
	if m.fetchCancel != nil {
		m.fetchCancel()
	}
}

// shutdown is the in-app variant of Shutdown: the same cleanup, returning the
// model with the fetch context released. Every exit path inside Update funnels
// through it (via quit), so a quit while the playlist fetch is loading kills
// that yt-dlp child too.
func (m appModel) shutdown() appModel {
	m.Shutdown()
	m.fetchCancel = nil
	return m
}

// handlePlaylistMsg processes the asynchronous fetch result. A result that
// arrives after the user left the loading screen (esc) is stale and ignored.
func (m appModel) handlePlaylistMsg(msg playlistMsg) (tea.Model, tea.Cmd) {
	if m.screen != scrLoading || msg.seq != m.fetchSeq {
		return m, nil
	}
	// The fetch has returned: release its timeout context so the 180s
	// timer cannot outlive the request either way.
	if m.fetchCancel != nil {
		m.fetchCancel()
	}
	m.fetchCancel = nil
	if msg.err != nil {
		m.form = m.form.SetError("fetch failed: " + msg.err.Error())
		m.screen = scrForm
		return m, nil
	}

	m.pick = newPickModel(msg.pl, m.cfg.RangeStart, m.cfg.RangeEnd)
	if m.pick.TotalShown() == 0 {
		m.form = m.form.SetError("no videos found (check URL, range and cookies)")
		m.screen = scrForm
		return m, nil
	}

	// Replay the last-known terminal size into the fresh pick model before it
	// ever renders. Bubble Tea delivers WindowSizeMsg once at startup (and on
	// SIGWINCH, which the app forwards only to the active screen), but the
	// pick model is created here, long after those messages: without this
	// replay its row cache starts cold. View runs on a receiver copy, so the
	// fallback rebuild it would do on the first frame can never reach the
	// stored model and every frame would pay the full O(N) rebuild plus the
	// viewport's line rescan. The replay takes the same warm-up path as a real
	// resize (rebuildRows at the clamped content width, then syncRows). It is
	// skipped before the first WindowSizeMsg, when there is no size to replay.
	if m.width > 0 {
		m.pick, _ = m.pick.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
	}

	m.screen = scrPick
	return m, m.pick.Init()
}

// submitForm validates the form values, persists them, and starts the
// playlist fetch. On validation failure the form stays up with its error
// banner set; on success the banner is cleared. The fetch context is
// registered so esc on the loading screen can abandon it.
func (m appModel) submitForm() (tea.Model, tea.Cmd) {
	c := m.form.Config()
	if err := c.Validate(); err != nil {
		m.form = m.form.SetError(err.Error())
		return m, nil
	}
	m.cfg = c
	m = m.clearFormErr()

	m.fetchSeq++
	seq := m.fetchSeq
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	m.fetchCancel = cancel

	m.loading = newLoadingModel("Fetching playlist...")
	m.screen = scrLoading
	return m, tea.Batch(m.loading.Init(), fetchCmd(m.client, c, seq, ctx), saveCmd(c))
}

// clearFormErr drops the form error banner. It is applied on every transition
// that re-enters the form screen with the previous form instance.
func (m appModel) clearFormErr() appModel {
	m.form = m.form.SetError("")
	return m
}

// startDownloads builds the job manager from the selected videos and moves
// the app to the download screen.
func (m appModel) startDownloads() (tea.Model, tea.Cmd) {
	sel := m.pick.Selected()

	opts := jobs.Options{
		PlaylistURL:      m.cfg.PlaylistURL,
		TargetDir:        m.cfg.TargetDir,
		Quality:          m.cfg.Quality,
		FilenameTemplate: m.cfg.FilenameTemplate,
		CookiesBrowser:   m.cfg.CookiesBrowser,
		ArchivePath:      filepath.Join(m.cfg.TargetDir, ".yt-dlp-archive.txt"),
		Concurrency:      m.cfg.Concurrency,
		Videos:           sel,
	}

	// Cancellation is owned by the manager (Cancel runs taskkill per
	// handle); the parent context only bounds the download lifetime.
	m.mgr = jobs.NewManager(m.client, opts)
	m.mgr.Start(context.Background())

	m.dl = newDownloadModel(m.mgr, m.cfg, len(sel))
	m.screen = scrDownload
	return m, m.dl.Init()
}

// updateDownload forwards msg to the download screen and, once every job has
// reached a terminal state, captures the summary and moves to the done
// screen.
func (m appModel) updateDownload(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.dl, cmd = m.dl.Update(msg)
	if m.screen == scrDownload && m.dl.Finished() {
		m.done = newDoneModel(m.cfg, m.mgr, m.dl.Elapsed())
		m.screen = scrDone
	}
	return m, cmd
}

// forward delivers a non-navigation message to the active screen's
// sub-model. Spinner ticks, tool-check results, download ticks and mouse
// wheel events all travel this path; the done screen consumes nothing.
func (m appModel) forward(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m.screen {
	case scrCheck:
		var cmd tea.Cmd
		m.check, cmd = m.check.Update(msg)
		return m, cmd

	case scrForm:
		var cmd tea.Cmd
		m.form, cmd = m.form.Update(msg)
		return m, cmd

	case scrLoading:
		var cmd tea.Cmd
		m.loading, cmd = m.loading.Update(msg)
		return m, cmd

	case scrPick:
		var cmd tea.Cmd
		m.pick, cmd = m.pick.Update(msg)
		return m, cmd

	case scrDownload:
		return m.updateDownload(msg)
	}

	return m, nil
}
