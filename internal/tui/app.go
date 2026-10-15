package tui

import (
	"context"
	"fmt"
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

// Init starts the setup check together with its spinner.
func (m appModel) Init() tea.Cmd {
	return m.check.Init()
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
		return m, tea.Quit
	}

	switch m.screen {
	case scrCheck:
		switch k {
		case "q":
			return m, tea.Quit
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
			return m, tea.Quit
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
			return m, tea.Quit
		case "esc", "r":
			m.form = newFormModel(m.cfg)
			m = m.clearFormErr()
			m.screen = scrForm
			return m, m.form.Init()
		}
		return m, nil
	}

	return m.forward(msg)
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

// saveCmd persists the configuration off the event loop. A failed save must
// not block the workflow; the values stay in memory.
func saveCmd(cfg config.Config) tea.Cmd {
	return func() tea.Msg {
		_ = cfg.Save()
		return nil
	}
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
		m.done = newDoneModel(m.cfg, m.mgr)
		m.screen = scrDone
	}
	return m, cmd
}

// forward delivers a non-navigation message to the active screen's
// sub-model. Spinner ticks, tool-check results and download ticks all travel
// this path; the done screen consumes nothing.
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

// fetchCmd runs the playlist fetch off the event loop. The closure only
// captures the client, the validated configuration, the request sequence
// number and the caller-provided context; it never touches model state.
func fetchCmd(client *ytdlp.Client, cfg config.Config, seq int, ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		pl, err := client.FetchPlaylist(ctx, cfg.PlaylistURL, cfg.CookiesBrowser)
		return playlistMsg{pl: pl, err: err, seq: seq}
	}
}

// View renders the header, the active screen, and, while the cancel
// confirmation is armed on the download screen, the confirmation box.
func (m appModel) View() tea.View {
	width, height := m.width, m.height
	if width <= 0 {
		width = defaultViewWidth
	}
	if height <= 0 {
		height = defaultViewHeight
	}

	content := StyleTitle.Render("lhi-youtube-downloader") +
		StyleMuted.Render(fmt.Sprintf("  %s", screenName(m.screen))) +
		"\n\n"

	switch m.screen {
	case scrCheck:
		content += m.check.View(width)
	case scrForm:
		content += m.form.View(width, height-3)
	case scrLoading:
		content += m.loading.View(width)
	case scrPick:
		content += m.pick.View(width, height-3)
	case scrDownload:
		content += m.dl.View(width, height-3)
	case scrDone:
		content += m.done.View(width, height-3)
	}

	if m.confirmCancel && m.screen == scrDownload {
		content += "\n\n" + RenderBox(
			"Cancel all downloads?",
			"y = cancel everything    n = keep downloading",
			minInt(56, m.width-4),
		)
	}

	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

// screenName returns the human-readable name of a screen for the header.
func screenName(s screen) string {
	switch s {
	case scrCheck:
		return "setup check"
	case scrForm:
		return "configuration"
	case scrLoading:
		return "loading playlist"
	case scrPick:
		return "select videos"
	case scrDownload:
		return "downloading"
	case scrDone:
		return "finished"
	default:
		return ""
	}
}

// minInt returns the smaller of a and b.
func minInt(a, b int) int {
	return min(a, b)
}
