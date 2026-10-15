// Command fakeytdlp is a deterministic stand-in for the yt-dlp executable,
// built by the jobs integration tests so the client and manager can be
// exercised end to end without network access or a real yt-dlp install.
//
// Environment knobs:
//
//	FAKE_PLAYLIST_EXIT  when set, playlist mode prints an error on stderr and
//	                    exits with this code instead of emitting JSON
//	FAKE_SLEEP_MS       delay (ms) before any download output is written
//	FAKE_STDERR         "1" emits one warning line on stderr
//	FAKE_EXIT           download-mode exit code (default 0)
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// playlistJSON is the exact flat-playlist payload FetchPlaylist must parse.
// vid002 intentionally carries null title/url/duration and vid003 a
// playlist_index that differs from its array position.
const playlistJSON = `{"id":"PLtest","title":"Test Playlist","entries":[{"id":"vid001","title":"First Video","url":"https://www.youtube.com/watch?v=vid001","duration":61,"playlist_index":1},{"id":"vid002","title":null,"url":null,"duration":null,"playlist_index":2},{"id":"vid003","title":"Third","url":"https://www.youtube.com/watch?v=vid003","duration":125.5,"playlist_index":7}]}`

func main() {
	args := os.Args[1:]

	if hasArg(args, "--flat-playlist") {
		if v, ok := os.LookupEnv("FAKE_PLAYLIST_EXIT"); ok && strings.TrimSpace(v) != "" {
			fmt.Fprintln(os.Stderr, "ERROR: fake playlist failure")
			os.Exit(envInt("FAKE_PLAYLIST_EXIT", 1))
		}
		fmt.Println(playlistJSON)
		return
	}

	// Download mode: -I <n> selects the video, mirroring yt-dlp.
	n := 1
	for i, a := range args {
		if a == "-I" && i+1 < len(args) {
			if v, err := strconv.Atoi(args[i+1]); err == nil {
				n = v
			}
			break
		}
	}
	id := fmt.Sprintf("vid%03d", n)

	if ms := envInt("FAKE_SLEEP_MS", 0); ms > 0 {
		time.Sleep(time.Duration(ms) * time.Millisecond)
	}

	emit := func(line string) {
		fmt.Println(line)
		time.Sleep(30 * time.Millisecond)
	}
	emit("DL|" + id + "|downloading|100|200|NA|1000|5|50.0")
	emit("PP|" + id + "|processing")
	emit("DL|" + id + "|finished|200|200|NA|2000|0|100.0")
	emit("PP|" + id + "|finished")
	fmt.Println("[download] Destination: " + id + ".mp4")

	if os.Getenv("FAKE_STDERR") == "1" {
		fmt.Fprintln(os.Stderr, "WARNING: fake stderr line")
	}

	code := envInt("FAKE_EXIT", 0)
	if code == 0 {
		if archive := argValue(args, "--download-archive"); archive != "" {
			appendArchive(archive, id)
		}
	}
	os.Exit(code)
}

// hasArg reports whether args contains flag.
func hasArg(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

// argValue returns the argument following flag, or "" when absent.
func argValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// appendArchive records "youtube <id>" in the yt-dlp archive file, matching
// the line format real yt-dlp writes. Failures are ignored: yt-dlp treats
// archive problems as warnings, not download failures.
func appendArchive(path, id string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	_, _ = f.WriteString("youtube " + id + "\n")
	_ = f.Close()
}

// envInt parses the named environment variable as an integer, falling back
// to def when it is unset or malformed.
func envInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
