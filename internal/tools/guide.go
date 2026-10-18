package tools

import "runtime"

// InstallGuide returns a multi-line, user-facing installation guide for
// the missing tools, tailored to the operating system this binary runs
// on. Callers render it verbatim inside a narrow bordered box, so every
// line stays within 72 columns.
func InstallGuide() string {
	return guideFor(runtime.GOOS)
}

// guideFor returns the install guide for a GOOS value so that tests can
// assert every variant on any machine. Unknown systems get the Linux
// guide, since it documents the widest range of package managers.
func guideFor(goos string) string {
	switch goos {
	case "windows":
		return guideWindows
	case "darwin":
		return guideDarwin
	default:
		return guideLinux
	}
}

// guideWindows documents the Windows Package Manager path. The FFmpeg
// note calls out the standard full build and the yt-dlp-specific
// alternative that is faster to install.
const guideWindows = `Missing required tools. Install with Windows Package Manager (winget):

  winget install yt-dlp.yt-dlp
  winget install Gyan.FFmpeg

FFmpeg note: "Gyan.FFmpeg" is the standard full build.
The "yt-dlp.FFmpeg" package installs an FFmpeg build bundled for yt-dlp
as an alternative:

  winget install yt-dlp.FFmpeg

After installing, restart this app (or press R to re-check).
Verify manually:  yt-dlp --version     ffmpeg -version`

// guideDarwin documents Homebrew, the default on macOS, and the
// standalone downloads to use when Homebrew is unavailable. No command
// needs sudo.
const guideDarwin = `Missing required tools. Install them with Homebrew:

  brew install yt-dlp
  brew install ffmpeg

No sudo is needed. Without Homebrew, download the standalone
builds:

  - yt-dlp_macos from the yt-dlp releases page:
    https://github.com/yt-dlp/yt-dlp/releases/latest
  - an ffmpeg build from evermeet.cx:
    https://evermeet.cx/ffmpeg/

Place the binaries in a directory on your PATH (for example
/usr/local/bin) and make them executable with chmod +x.

After installing, restart this app (or press R to re-check).
Verify manually:  yt-dlp --version     ffmpeg -version`

// guideLinux documents the distribution package managers, the fresher
// pipx/pip routes for yt-dlp, and the static FFmpeg archive to use when
// no ffmpeg package is available.
const guideLinux = `Missing required tools. Install them with your package manager:

Debian / Ubuntu:

  sudo apt update && sudo apt install yt-dlp ffmpeg

Those packages can lag behind upstream. For the freshest yt-dlp:

  sudo apt install pipx && pipx install yt-dlp
  python3 -m pip install -U "yt-dlp[default]"

Fedora (enable RPM Fusion first, then):

  sudo dnf install ffmpeg yt-dlp

RPM Fusion release package: https://rpmfusion.org/Configuration

Arch Linux:

  sudo pacman -S yt-dlp ffmpeg

No ffmpeg package? Extract a static build from the BtbN
FFmpeg-Builds release archive into ~/.local/bin:

  https://github.com/BtbN/FFmpeg-Builds/releases

After installing, restart this app (or press R to re-check).
Verify manually:  yt-dlp --version     ffmpeg -version`
