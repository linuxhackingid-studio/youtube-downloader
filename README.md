# lhi-youtube-downloader

A Windows-friendly terminal UI (Go + Bubble Tea v2) that automates downloading videos from a YouTube playlist — including unlisted playlists — by wrapping [yt-dlp](https://github.com/yt-dlp/yt-dlp). Built for the LinuxHackingID workflow ("lhi").

## Table of Contents

- [Features](#features)
- [Prerequisites](#prerequisites)
- [Build and run](#build-and-run)
- [Usage](#usage)
- [Configuration reference](#configuration-reference)
- [Under the hood](#under-the-hood)
- [Notes and caveats](#notes-and-caveats)
- [Development](#development)
- [Legal](#legal)

## Features

- Playlist URL input, including unlisted playlists.
- Target folder picker (typed path; defaults to `%USERPROFILE%\Downloads\YouTube`).
- Quality selector: Best available / 2160p / 1440p / 1080p / 720p / 480p / 360p / Audio only (MP3).
- Scrollable video list with checkbox select/deselect: `space` toggles, `a`/`n` select all/none.
- Concurrent downloads with a bounded worker pool: 1–10 workers, default 3.
- Resume/skip support via a yt-dlp download archive (`.yt-dlp-archive.txt` in the target folder) — re-runs only fetch new videos.
- Cookies-from-browser support (chrome, chromium, edge, firefox, brave, opera, vivaldi, whale, safari) for private and age-restricted content.
- Optional playlist range filter (start/end by 1-based index).
- Customizable yt-dlp output filename template.
- Live per-video progress bars, overall progress, and a log tail.
- Cancel-all with confirmation.
- Session configuration persisted to `%AppData%\lhi-youtube-downloader\config.json`.

## Prerequisites

- **Go 1.26.3 or newer** to build (the module's `go.mod` declares `go 1.26.3`).
- **yt-dlp and FFmpeg must be installed.** The app detects both on startup and shows an install guide if either is missing.

Install them with Windows Package Manager (winget):

```powershell
winget install yt-dlp.yt-dlp
winget install Gyan.FFmpeg
```

`Gyan.FFmpeg` is the standard full build. As an alternative, `yt-dlp.FFmpeg` installs an FFmpeg build bundled for yt-dlp:

```powershell
winget install yt-dlp.FFmpeg
```

After installing, restart the app (or press `r` on the setup screen to re-check). Verify manually:

```powershell
yt-dlp --version
ffmpeg -version
```

## Build and run

```powershell
go build -o bin\ytd.exe .
.\bin\ytd.exe
```

Or run it directly:

```powershell
go run .
```

The first build downloads the TUI dependencies through the Go module proxy.

## Usage

The app walks through six screens:

1. **Setup check** — probes `yt-dlp` and `ffmpeg` on `PATH` and reports their versions and locations. Press `enter` to continue (enabled only when both tools are found), `r` to re-check, `q` to quit.
2. **Configuration** — `tab`/`↑`/`↓` move between fields, `←`/`→` change the quality and cookies selectors, `enter` advances focus and on the last field ("Filename template") starts the playlist fetch, `esc` goes back. The form is saved to `config.json` on every submit.
3. **Playlist fetch** — a spinner while yt-dlp lists the playlist in flat mode. Press `esc` to abandon the fetch and return to the form, `q` to quit.
4. **Video list** — `↑`/`↓` (or `k`/`j`) move, `space` toggles the highlighted video, `a`/`n` select all/none, `enter` starts the download (at least one video must be selected), `esc` goes back to the form.
5. **Downloading** — per-video progress with status markers and a right-aligned detail column (percent, speed, ETA), an overall progress bar, and the tail of the yt-dlp log. Press `esc` (or `ctrl+c`) to arm the cancel confirmation: `y` cancels everything, `n` keeps downloading.
6. **Summary** — result counts (completed / skipped / failed / canceled / total), the output folder, and up to 10 failed videos with a retry hint. Press `enter` (or `q`) to quit, `r` (or `esc`) to go back to the configuration and fetch more — already-archived videos are skipped automatically.

`ctrl+c` quits immediately from any other screen.

## Configuration reference

Settings are stored as JSON at `%AppData%\lhi-youtube-downloader\config.json` (technically `os.UserConfigDir()/lhi-youtube-downloader/config.json`) and are rewritten every time the configuration form is submitted. A missing, unreadable, or corrupt file is not an error: the app silently falls back to the defaults below.

| Field (`config.json`) | Meaning | Default |
| --- | --- | --- |
| `playlist_url` | Playlist (or single-video) URL to fetch; must be `http(s)`. | Empty (entered in the form) |
| `target_dir` | Folder downloads are written to; the archive file lives here too. | `%USERPROFILE%\Downloads\YouTube` |
| `quality` | Output quality as an integer: `0` Best available, `1` 2160p, `2` 1440p, `3` 1080p, `4` 720p, `5` 480p, `6` 360p, `7` Audio only (MP3). | `0` (Best available) |
| `filename_template` | yt-dlp output template. Must contain `%(ext)s`, stay relative (no absolute, rooted, or `..` paths), and be at most 512 characters. | `%(playlist_index)03d - %(title)s [%(id)s].%(ext)s` |
| `cookies_browser` | Browser to read cookies from: `""` (none), `chrome`, `chromium`, `edge`, `firefox`, `brave`, `opera`, `vivaldi`, `whale`, `safari`. | `""` (none) |
| `concurrency` | Number of parallel downloads; clamped to 1–10. | `3` |
| `range_start` | 1-based first playlist index to show; `0` = no lower bound. | `0` (unset) |
| `range_end` | 1-based last playlist index to show; `0` = no upper bound. | `0` (unset) |

Constraints enforced on submit: `range_start` must be `<=` `range_end` when both are set, and the quality/cookies values must be among the supported options (unknown values are normalized to Best / none in the form).

## Under the hood

- **One yt-dlp process per video.** Each selected video runs `yt-dlp <playlist-url> -I <index>` so playlist metadata — and `%(playlist_index)s` in the filename template — stay correct.
- **Deterministic invocation.** Every run passes `--ignore-config --no-warnings --no-color --newline`, plus `--progress-delta 0.25` to throttle progress output to roughly four lines per second.
- **Machine-readable progress.** Progress is parsed from `--progress-template` records (`DL|…` download lines, `PP|…` postprocess lines) into live per-video bars, an overall bar, and a bounded log ring (last 200 lines, sampled every 250 ms).
- **Quality mapping.** Best uses `-f "bv*+ba/b"`; capped qualities use `bv*[height<=?N]+ba/b[height<=?N]/b`; audio-only uses `-x --audio-format mp3 --audio-quality 0`. Video downloads merge to MP4 when FFmpeg is available.
- **Resume/skip.** `--download-archive <target-dir>\.yt-dlp-archive.txt` — yt-dlp appends one `youtube <videoid>` line per finished video. Before starting, the app reads the file and marks already-downloaded videos as skipped without spawning a process; yt-dlp's own archive check covers the rest.
- **Cancellation.** On Windows each download is killed with `taskkill /T /F` so FFmpeg children die with yt-dlp; the process tree is walked while the parent is still alive.
- **No console flashes.** yt-dlp and FFmpeg are spawned with a hidden window and `CREATE_NO_WINDOW`.
- **Timeouts and hygiene.** Tool version probes are bounded (10 s each) and playlist fetches time out after 180 s so a hung yt-dlp cannot wedge the TUI. Output lines are bounded and sanitized before rendering.

## Notes and caveats

- **Rate limiting.** YouTube may throttle or block aggressive concurrency. Start with the default of 3 workers and increase only if downloads hold up.
- **Browser cookies.** Chrome and Edge cookie extraction may require the browser to be fully closed, and may fail on recent Chrome versions that use app-bound encryption — this is a yt-dlp limitation, not specific to this app. If extraction fails, try another browser or close the target browser first.
- **Rendering.** Windows Terminal is recommended for the best TUI rendering (progress bars, colors, box drawing).
- **Unlisted ≠ public.** An unlisted playlist is still private content. See [Legal](#legal).

## Development

Run the full test suite:

```powershell
go test ./...
```

Unit and integration tests need no network access: they build deterministic fake `yt-dlp`/`ffmpeg` executables from `internal/jobs/testdata/fakeytdlp` and `internal/tui/testdata/fakeffmpeg`. The end-to-end test drives the real TUI headlessly via [teatest](https://pkg.go.dev/github.com/charmbracelet/x/exp/teatest/v2) (it self-skips on non-Windows platforms).

Project structure:

| Package | Responsibility |
| --- | --- |
| `internal/tools` | Detects yt-dlp and FFmpeg and provides Windows install guidance. |
| `internal/ytdlp` | yt-dlp/FFmpeg wrapper: argument building, playlist fetching, progress parsing. |
| `internal/config` | Loads, validates, and persists `config.json`. |
| `internal/jobs` | Bounded-concurrency download orchestrator with snapshots for the TUI. |
| `internal/tui` | Bubble Tea screens: setup check, form, loading, picker, download, summary. |

## Legal

Download only content you own or are permitted to save; an unlisted playlist is not a license to redistribute. Respect YouTube's Terms of Service and applicable copyright law. yt-dlp and FFmpeg are separate projects with their own licenses and are not distributed with this app.
