package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"linuxhackingid.com/youtube-downloader/internal/tui"
)

func main() {
	p := tea.NewProgram(tui.New())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "lhi-youtube-downloader:", err)
		os.Exit(1)
	}
}
