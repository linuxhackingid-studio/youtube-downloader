// Command ytd is the composition root of lhi-youtube-downloader, a terminal
// UI that drives yt-dlp to download YouTube playlists.
//
// Version resolution precedence, highest first:
//
//  1. the package variable version, injected by release builds with
//     go build -ldflags "-X main.version=1.2.3"; keep that variable name
//     stable, the -X target main.version is part of the build contract,
//  2. the main module version reported by runtime/debug.ReadBuildInfo,
//     when it is non-empty and not "(devel)",
//  3. the recorded VCS revision, rendered as "devel+<first 12 chars>",
//  4. the literal "devel".
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"

	tea "charm.land/bubbletea/v2"

	"linuxhackingid.com/youtube-downloader/internal/tui"
)

// progName is the installed name of the binary.
const progName = "ytd"

// develVersion labels builds that carry no release version.
const develVersion = "devel"

// version is empty in development builds and is meant to be set at build time
// with -ldflags "-X main.version=1.2.3". Do not rename it: the -X target
// main.version is the documented release contract.
var version = ""

const usageText = `lhi-youtube-downloader - TUI for downloading YouTube playlists via yt-dlp

Usage:
  ytd [flags]

Flags:
  -help, -h  print this help text and exit
  -version   print version information and exit

Examples:
  ytd           start the interactive TUI
  ytd -version  print the version and exit
  ytd -help     print this help and exit
`

func main() {
	os.Exit(run(os.Args[1:]))
}

// run parses args, dispatches the requested mode and returns the process exit
// code: 0 on success, 1 when the TUI fails to run, 2 on a usage error.
func run(args []string) int {
	fs := flag.NewFlagSet(progName, flag.ContinueOnError)
	// The FlagSet itself renders nothing: diagnostics are emitted below so
	// help reaches stdout exactly once and errors reach stderr exactly once.
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}

	showVersion := fs.Bool("version", false, "print version information and exit")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(os.Stdout, usageText)
			return 0
		}
		fmt.Fprintf(os.Stderr, "%s: %v\n\n", progName, err)
		fmt.Fprint(os.Stderr, usageText)
		return 2
	}

	if *showVersion {
		fmt.Printf("lhi-youtube-downloader %s\n", resolveVersion())
		return 0
	}

	p := tea.NewProgram(tui.New())
	final, err := p.Run()

	// Take the children down, however Run returned. Bubbletea handles
	// tea.QuitMsg and tea.InterruptMsg inside its own event loop, so on a
	// SIGTERM, a systemd stop or a non-TTY Ctrl+C the model's Update never
	// runs an exit path; only this call kills the yt-dlp and ffmpeg process
	// trees before the process ends. A nil or foreign final model simply has
	// no Shutdown method and the assertion yields false.
	if s, ok := final.(interface{ Shutdown() }); ok {
		s.Shutdown()
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", progName, err)
		return 1
	}
	return 0
}

// resolveVersion returns the version label for this build, applying the
// precedence documented on the package comment.
func resolveVersion() string {
	if version != "" {
		return version
	}

	bi, ok := debug.ReadBuildInfo()
	if !ok || bi == nil {
		return develVersion
	}
	if v := bi.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	for _, s := range bi.Settings {
		if s.Key == "vcs.revision" && s.Value != "" {
			rev := s.Value
			if len(rev) > 12 {
				rev = rev[:12]
			}
			return develVersion + "+" + rev
		}
	}
	return develVersion
}
