// Command fakeffmpeg is a deterministic stand-in for the ffmpeg executable,
// built by the TUI end-to-end test so tools.Check can resolve a fake binary
// next to the fake yt-dlp. It prints a plausible first version line to stdout
// and exits 0, which is everything the check consumes (LookPath plus the
// first stdout line).
package main

import "fmt"

func main() {
	fmt.Println("ffmpeg version 9.0.2-fake-ffmpeg Copyright (c) 2000-2026")
}
