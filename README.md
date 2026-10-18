# lhi-youtube-downloader

A terminal UI (Go + [Bubble Tea v2](https://github.com/charmbracelet/bubbletea)) that wraps [yt-dlp](https://github.com/yt-dlp/yt-dlp) to download videos from a YouTube playlist or single video, unlisted playlists included. It runs on **Windows, Linux and macOS**: the setup screen probes yt-dlp and ffmpeg on `PATH` *and* in the well-known install locations of the host system, prints a platform line such as `Linux (arm64)`, and shows the matching per-OS install guide whenever a tool is missing.

## Contents

- [Features](#features)
- [Requirements](#requirements)
- [Install the required tools](#install-the-required-tools)
- [Build from source](#build-from-source)
- [Quick start](#quick-start)
- [Configuration](#configuration)
- [Troubleshooting](#troubleshooting)
- [Development](#development)
- [Project layout](#project-layout)
- [Coding rules](#coding-rules)
- [Legal](#legal)

## Features

- Playlist or single-video URL input, unlisted playlists included.
- Target folder field; defaults to `Downloads/YouTube` under your home directory.
- Quality selector: Best available / 2160p / 1440p / 1080p / 720p / 480p / 360p / Audio only (MP3).
- Scrollable video list, everything selected by default: `space` toggles one row, `a`/`n` select all/none, `/` filters by title or video id.
- Optional playlist range filter (`range_start` / `range_end` by 1-based index).
- Concurrent downloads with a bounded worker pool: 1-10 workers, default 3.
- Resume and skip support via a yt-dlp download archive (`.yt-dlp-archive.txt` in the target folder): re-runs only fetch new videos.
- Cookies-from-browser support (`chrome`, `chromium`, `edge`, `firefox`, `brave`, `opera`, `vivaldi`, `whale`, `safari`) for private and age-restricted content.
- Customizable yt-dlp output filename template.
- Live per-video progress bars, an overall bar, a 200-line log ring (the last 4 non-empty lines are on screen), and a window title that doubles as a progress indicator.
- Cancel-all with a `y`/`n` confirmation.
- Persisted settings in a per-OS config file; the file is portable across operating systems.
- Cross-platform process handling: no console flashes on Windows, no orphaned ffmpeg processes anywhere.

## Requirements

- **Go 1.26.3 or newer** to build (the module declares `go 1.26.3` in `go.mod`). Pre-built binaries need no Go installation.
- **yt-dlp and ffmpeg installed and runnable.** The app does not bundle them and cannot download anything without them. yt-dlp is required for everything; ffmpeg is required for merging video+audio streams and for MP3 conversion.
- A terminal around 80x24 or larger for the intended layout. The frame adapts to whatever it gets: content is capped at 100 columns, every line is clipped to the terminal width, and an oversized body degrades into truncated lines instead of wrapping.
- Windows, Linux or macOS. The CI matrix builds and tests all three.

## Install the required tools

Install them with your platform's package manager. The app detects both tools at startup; when one is missing it renders an install guide with the same commands for the OS it is running on, so you can also just start the app and follow what it prints.

### Windows (winget)

```powershell
winget install yt-dlp.yt-dlp
winget install Gyan.FFmpeg
```

`Gyan.FFmpeg` is the standard full build. As a faster alternative, `yt-dlp.FFmpeg` installs an FFmpeg build bundled for yt-dlp:

```powershell
winget install yt-dlp.FFmpeg
```

### macOS (Homebrew)

```sh
brew install yt-dlp
brew install ffmpeg
```

No `sudo` is needed. Without Homebrew, download the standalone builds and place them in a directory on your `PATH` (for example `/usr/local/bin`), then `chmod +x` them:

- `yt-dlp_macos` from the [yt-dlp releases page](https://github.com/yt-dlp/yt-dlp/releases/latest)
- an ffmpeg build from [evermeet.cx](https://evermeet.cx/ffmpeg/)

### Linux

Debian / Ubuntu:

```sh
sudo apt update && sudo apt install yt-dlp ffmpeg
```

Distribution packages can lag behind upstream. For the freshest yt-dlp:

```sh
sudo apt install pipx && pipx install yt-dlp
python3 -m pip install -U "yt-dlp[default]"
```

Fedora (enable [RPM Fusion](https://rpmfusion.org/Configuration) first, then):

```sh
sudo dnf install ffmpeg yt-dlp
```

Arch Linux:

```sh
sudo pacman -S yt-dlp ffmpeg
```

No ffmpeg package for your distribution? Extract a static build from the [BtbN FFmpeg-Builds](https://github.com/BtbN/FFmpeg-Builds/releases) release archive into `~/.local/bin`.

### Verify

After installing, restart the app (or press `r` on the setup screen to re-check), or verify manually:

```sh
yt-dlp --version
ffmpeg -version
```

## Build from source

Linux and macOS:

```sh
go build -o bin/ytd ./cmd/ytd
./bin/ytd
```

Windows (PowerShell / cmd):

```powershell
go build -o bin\ytd.exe ./cmd/ytd
.\bin\ytd.exe
```

Or run without building:

```sh
go run ./cmd/ytd
```

The first build downloads the TUI dependencies through the Go module proxy. The same commands work on all three platforms; only the output name differs (`bin/ytd` vs `bin\ytd.exe`).

Injected release version:

```sh
go build -ldflags "-X main.version=1.2.3" -o bin/ytd ./cmd/ytd
```

`-X main.version=...` is a stable build contract (see the package comment in `cmd/ytd/main.go`): when the variable is empty, the reported version falls back to the module version, then the recorded VCS revision as `devel+<first 12 chars>`, then the literal `devel`.

Command-line flags:

| Flag | Effect |
| --- | --- |
| `-version` | Print `lhi-youtube-downloader <version>` and exit 0. |
| `-help`, `-h` | Print the usage text and exit 0. |
| (none) | Start the interactive TUI. |

Exit codes: `0` success, `1` the TUI failed to run, `2` a usage error (unknown flag; the error and usage text are printed to stderr).

## Quick start

Start `ytd` and walk through six screens. The footer on every screen is a key legend generated from the live model.

### 1. Setup check

Probes yt-dlp and ffmpeg concurrently (each with a 10-second timeout), then shows one of:

- `[OK]  yt-dlp  <version>  (<path>)`
- `[MISSING] yt-dlp - <reason>`

Below the tool panel a `Platform` line names the host, for example `Windows (amd64)`, `macOS (arm64)` or `Linux (amd64)`. While a tool is missing, the screen renders the install guide for this OS.

| Key | Action |
| --- | --- |
| `enter` | Continue to the configuration (only when both tools show `[OK]`) |
| `r` | Re-run the tool check |
| `q`, `ctrl+c` | Quit |

### 2. Configuration

One field per row, grouped into sections: **Source** (Playlist URL), **Output** (Target folder), **Download** (Quality, Cookies from browser, Concurrent downloads), **Selection** (Range start, Range end), **File name** (Filename template). On short terminals (body under 26 rows) the fields switch to a one-line-per-field layout so the last fields stay reachable.

The focused field is marked with the form's accent triangle (`▶`) and its label is highlighted; the Quality and Cookies selectors render as `< value >` while focused.

| Key | Action |
| --- | --- |
| `tab`, `down` | Next field |
| `shift+tab`, `up` | Previous field |
| `left`, `right` | Cycle the focused selector (Quality, Cookies from browser) |
| `enter` | Next field; on the last field (Filename template) validate and fetch the playlist |
| `esc` | Back to the setup check |

On submit the form runs fast, field-local checks first: Playlist URL required, Target folder required, a blank Concurrent downloads entry (or 0) means 3, and Range start must not exceed Range end. On a field error, focus jumps to the first offending field and nothing is saved or fetched. On success the settings are written to the config file and the playlist fetch starts.

### 3. Loading playlist

A spinner with the elapsed time while yt-dlp lists the playlist in flat mode. The fetch is bounded by a 180-second timeout so a hung yt-dlp cannot wedge the TUI.

| Key | Action |
| --- | --- |
| `esc` | Cancel the fetch and return to the form |
| `q`, `ctrl+c` | Quit |

A failed fetch or an empty result returns to the form with an error banner (for example `no videos found (check URL, range and cookies)`).

### 4. Select videos

The header shows `Videos - <selected> of <shown>` (plus a `(range a-b)` suffix when the range filter is active). Every offered video starts selected; the checkboxes mark what will be downloaded. The inline `/` filter matches titles and video ids case-insensitively (substring) and only narrows the rows on screen: it never changes any selection.

| Key | Action |
| --- | --- |
| `up`, `k` | Cursor up |
| `down`, `j` | Cursor down |
| `pgup`, `pgdown` | Move one viewport page |
| `home`, `g` | First row |
| `end`, `G` | Last row |
| `space` | Toggle the highlighted video |
| `a` | Select all offered videos |
| `n` | Select none |
| `/` | Focus the filter field (type to filter title or id) |
| `enter` | Start the download; while the filter is focused, keep the filter and return to the list |
| `esc` | Back to the form; while the filter is focused, clear the filter and leave search |
| mouse wheel | Scroll three rows per notch |

`enter` with nothing selected shows the hint `select at least one video` instead of starting.

### 5. Downloading

Header counters (`done`, `running`, `queued`, `failed`, `skipped`, elapsed), an overall progress bar, one row per job with a status marker and a right-aligned detail column (percent, mini bar, speed, ETA), and the tail of the yt-dlp log. The terminal window title reads `lhi-youtube-downloader - 42% (3/10)`. Job rows are bounded by the terminal height (`+N more` when they do not fit); the log panel is dropped on very short terminals so the jobs stay visible.

| Key | Action |
| --- | --- |
| `esc` | Arm the cancel confirmation: `y` cancels everything, `n` keeps downloading |
| `ctrl+c` | Same confirmation on this screen (everywhere else `ctrl+c` quits immediately) |

### 6. Finished

Result counts (completed, skipped, failed, canceled, selected, plus elapsed time), the output folder, and up to 10 failed videos with a retry hint.

| Key | Action |
| --- | --- |
| `o` | Open the output folder in the system file manager |
| `r`, `esc` | Back to the configuration to download more (archived videos are skipped automatically) |
| `enter`, `q` | Quit |

## Configuration

Settings are stored as JSON in the per-user configuration directory. The exact path is `os.UserConfigDir()/lhi-youtube-downloader/config.json`:

| OS | Path |
| --- | --- |
| Windows | `%AppData%\lhi-youtube-downloader\config.json` |
| macOS | `~/Library/Application Support/lhi-youtube-downloader/config.json` |
| Linux and other Unix | `$XDG_CONFIG_HOME/lhi-youtube-downloader/config.json`, defaulting to `~/.config/lhi-youtube-downloader/config.json` |

The app rewrites the file every time the configuration form is submitted, atomically (temporary file plus rename), so a crash never leaves a partially written file. A missing, unreadable or corrupt file is not an error: the app silently falls back to the defaults, and a partial JSON file overrides the defaults field by field. Loading reads at most 1 MiB: no valid configuration comes near that size, so an oversized file is refused and the app starts from the defaults, exactly as it does for a corrupt file.

Example (Windows path; on Linux and macOS `target_dir` would be e.g. `/home/you/Downloads/YouTube`):

```json
{
  "playlist_url": "https://www.youtube.com/playlist?list=PLxxxxxxxxxxxxxxxx",
  "target_dir": "C:\\Users\\you\\Downloads\\YouTube",
  "quality": 0,
  "filename_template": "%(playlist_index)03d - %(title)s [%(id)s].%(ext)s",
  "cookies_browser": "",
  "concurrency": 3,
  "range_start": 0,
  "range_end": 0
}
```

| Field | Type | Default | Meaning and validation |
| --- | --- | --- | --- |
| `playlist_url` | string | empty | Playlist or single-video URL. May be empty on disk; when set it must parse as an `http(s)` URL with a host. The form requires it before fetching. |
| `target_dir` | string | `<home>/Downloads/YouTube` (fallback `YouTubeDownloads` when the home directory cannot be resolved) | Folder downloads are written to; the archive file `.yt-dlp-archive.txt` lives here too. Required: a value that cleans to `.` (or empty) is rejected. Created with mode `0o755` when a download starts. |
| `quality` | integer | `0` (Best available) | `0` Best, `1` 2160p, `2` 1440p, `3` 1080p, `4` 720p, `5` 480p, `6` 360p, `7` Audio only (MP3). Values outside 0-7 fail validation; unknown values in a loaded file are normalized to Best in the form. |
| `filename_template` | string | `%(playlist_index)03d - %(title)s [%(id)s].%(ext)s` | yt-dlp `-o` output template. Must contain `%(ext)s`, contain no NUL, be at most 512 characters, and stay relative: no absolute path, no rooted prefix (`/x` or `\x`), no Windows drive prefix (`C:` or `d:`), no `..` segment (including `.. ` variants), and it must pass a local-path check. The checks are OS-independent, so the same config is accepted or rejected identically on every platform. The form field itself accepts up to 300 characters. |
| `cookies_browser` | string | `""` (none) | One of `""`, `chrome`, `chromium`, `edge`, `firefox`, `brave`, `opera`, `vivaldi`, `whale`, `safari`. Any other value fails validation; the form normalizes unknown values to none. |
| `concurrency` | integer | `3` | Parallel download workers. Validation clamps the value to 1-10; the form maps a blank entry (or 0) to 3 and caps input above 10 at 10. |
| `range_start` | integer | `0` (no lower bound) | 1-based inclusive first playlist index offered in the video list. Negative values are clamped to 0. |
| `range_end` | integer | `0` (no upper bound) | 1-based inclusive last index offered. Negative values are clamped to 0. When both bounds are set, `range_start` must be `<=` `range_end`. |

All fields are optional in the file; unknown keys are ignored. The file contains no secrets.

## Troubleshooting

**A tool is installed but the setup screen says `[MISSING]`.** The app searches `PATH` first and then well-known install locations. Restart it or press `r`; if the tool still is not found, confirm it runs from your shell (`yt-dlp --version`). Well-known locations that are probed:

- Windows: `%LOCALAPPDATA%\Microsoft\WinGet\Links`, `%LOCALAPPDATA%\Programs\Python\Python*\Scripts`, `%APPDATA%\Python\Scripts`, `%USERPROFILE%\scoop\shims`, `%ProgramData%\chocolatey\bin` (the Python entry is a glob because the interpreter directory carries a version suffix).
- macOS: `/opt/homebrew/bin` and `/usr/local/bin` first, then `~/.local/bin`, `/usr/bin`, `/bin`, `/snap/bin`, `/var/lib/flatpak/exports/bin`.
- Linux: `~/.local/bin`, `/usr/local/bin`, `/usr/bin`, `/bin`, `/snap/bin`, `/var/lib/flatpak/exports/bin`.

Directories already present on `PATH` are not probed twice, and only existing directories are considered.

**Linux / macOS: the binary exists but is not detected.** The probe requires a regular file with at least one executable permission bit. `chmod +x <path>` fixes a download that lost its permissions (common after extracting a release archive).

**Windows console notes.** Windows Terminal is recommended for the best TUI rendering (progress bars, colors, box drawing). External processes spawn with `CREATE_NO_WINDOW` and a hidden window, so no console flashes appear during downloads. `taskkill.exe` is resolved from `%SystemRoot%\System32` when available, with a `PATH` fallback.

**Nothing is left running after cancel or quit.** Canceling (or quitting while a download runs) terminates the whole process tree: `taskkill /T /F /PID` on Windows, a process-group `SIGKILL` on Linux and macOS. Children are killed while yt-dlp is still alive so its ffmpeg children cannot be orphaned; a 10-second pipe-close watchdog force-closes the output pipes if a process that escaped the kill still holds them open. After the TUI returns, one final cleanup runs too, so a SIGTERM, a systemd stop or a non-TTY Ctrl+C cannot leave children behind either.

**Rate limiting.** YouTube may throttle or block aggressive concurrency. Start with the default of 3 workers and raise it only if downloads hold up.

**Browser cookies.** Chrome and Edge cookie extraction may require the browser to be fully closed, and may fail on recent Chrome versions that use app-bound encryption. This is a yt-dlp limitation, not specific to this app. If extraction fails, try another browser or close the target browser first.

**The video list is empty.** Use `esc` to return to the form and check the URL, the range bounds and the cookies setting; the banner names the failing stage.

**Progress looks stale.** The download screen samples the manager every 250 ms, and only re-renders when the manager's revision counter changes, so between two ticks nothing is redrawn. yt-dlp progress output is throttled to roughly four lines per second per process.

## Development

Prerequisites: Go 1.26.3 or newer, `gofmt`, and (for `make lint`) [golangci-lint](https://golangci-lint.run) v2. The Makefile wraps the common tasks and picks `bin/ytd.exe` on Windows; it assumes a POSIX shell (Git Bash, MSYS, WSL).

| Target | Command it runs |
| --- | --- |
| `make build` | `go build -o bin/ytd ./cmd/ytd` (host GOOS/GOARCH) |
| `make build-all` | Cross-compile `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64` into `bin/ytd-<os>-<arch>[.exe]` |
| `make test` | `go test ./... -count=1` |
| `make test-race` | `go test ./... -race -count=1` (use on Linux / macOS) |
| `make vet` | `go vet ./...` |
| `make lint` | `golangci-lint run` |
| `make fmt` | `gofmt -w .` |
| `make checklines` | `go run ./scripts/checklines` (hygiene guard, see below) |
| `make check` | gofmt check, `vet`, `test`, `checklines` |
| `make clean` | Remove `bin/` |

Test commands without make:

```sh
go test ./... -count=1              # unit + integration tests, no network needed
go test ./... -short                # skips the headless end-to-end test
go test ./... -race -count=1        # race detector, Linux / macOS
go test ./internal/tui -run TestE2E # just the end-to-end journey
```

How the tests are built:

- Unit tests live next to each package and mock at the seams (for example the `platform` launcher function).
- Integration tests build deterministic fake executables from `internal/jobs/testdata/fakeytdlp` and `internal/tui/testdata/fakeffmpeg` with `go build`, so manager and client behaviour is exercised without network access or a real yt-dlp install. If the harness cannot build, the test self-skips rather than failing.
- The end-to-end test (`internal/tui/e2e_test.go`) drives the real TUI headlessly with [teatest](https://pkg.go.dev/github.com/charmbracelet/x/exp/teatest/v2), against the fake toolchain and a sandboxed home/config directory. It runs on Windows, Linux and macOS alike, and only `-short` skips it.

CI (`.github/workflows/ci.yml`) runs four jobs on every push and pull request:

- **checks**: `gofmt -l`, `go vet ./...`, `go run ./scripts/checklines`.
- **test**: matrix over `ubuntu-latest`, `macos-latest`, `windows-latest`; build, vet, `go test ./... -count=1`, plus `-race` on the non-Windows runners.
- **cross-build**: `CGO_ENABLED=0` build of the five release targets.
- **lint**: golangci-lint with the pinned version (v2.15.0) and the config in `.golangci.yml` (errcheck, govet, ineffassign, misspell, revive, staticcheck, unused).

## Project layout

```text
cmd/ytd/             entry point: flags, version resolution, exit codes
internal/config/     config.json load, save (atomic), validation
internal/jobs/       bounded-concurrency download manager, archive reader, log ring
internal/platform/   OS label and native "open folder" launcher
internal/tools/      yt-dlp / ffmpeg detection, version probes, install guide
internal/tui/        Bubble Tea models: six screens, frame, styles, widgets
internal/ytdlp/      yt-dlp client: argument building, playlist fetch, progress parsing, process control
scripts/checklines/  repository hygiene guard (800-line cap, dash rule)
.github/workflows/   CI matrix
Makefile             build, test, lint and hygiene targets
```

| Package | Responsibility |
| --- | --- |
| `cmd/ytd` | Composition root. Parses `-version` / `-help`, resolves the version label, constructs the TUI and maps failures to exit codes. |
| `internal/config` | Defines the on-disk JSON schema, loads it with graceful defaults, saves it atomically, and validates/normalizes every field OS-independently. |
| `internal/jobs` | Runs a pre-selected video list through a fixed worker pool, tracks per-job state, pre-skips archived videos, and exposes thread-safe snapshots, summaries, a revision counter and a bounded log ring for the TUI. |
| `internal/platform` | Maps `runtime.GOOS` to a friendly label (`Windows (amd64)`) and opens the output folder with the native file manager. |
| `internal/tools` | Locates yt-dlp and ffmpeg on `PATH` plus well-known install directories, probes their versions under a timeout, and renders the per-OS install guide. |
| `internal/tui` | Bubble Tea v2 models for the six screens, the surrounding frame, key legends, layout clamping, theme handling and shared widgets. |
| `internal/ytdlp` | The only package that speaks yt-dlp: builds argument lists, fetches playlist metadata in flat JSON mode, parses the progress templates, and owns process lifetime including process-tree kills. |
| `scripts/checklines` | Stdlib-only guard that fails the build when a file exceeds 800 lines or a source/doc/workflow file contains an em dash or en dash. |

## Coding rules

- **Maximum 800 lines per file.** Enforced by `go run ./scripts/checklines` (also available as `make checklines`).
- **No em dash (U+2014) or en dash (U+2013)** anywhere in `.go`, `.md`, `.yml` or `.yaml` files, also enforced by `checklines`. Use ASCII hyphens.
- **gofmt, `go vet` and golangci-lint must pass.** `make check` runs gofmt check, vet, the test suite and the hygiene guard.
- **Stdlib first.** The module has exactly three direct dependencies (`charm.land/bubbletea/v2`, `charm.land/bubbles/v2`, `charm.land/lipgloss/v2`), and repository tooling such as `scripts/checklines` is stdlib-only. New dependencies need a strong reason.
- **Conventional Commits** for commit messages.
- Changes should come with tests; keep the CI matrix (three operating systems) green.

## Legal

Download only content you own or are permitted to save; an unlisted playlist is not a license to redistribute. Respect YouTube's Terms of Service and applicable copyright law. yt-dlp and FFmpeg are separate projects with their own licenses and are not distributed with this app.
