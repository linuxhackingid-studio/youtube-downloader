package jobs

import (
	"bufio"
	"os"
	"strings"
)

// archiveSkipMsg is the marker yt-dlp prints when a video is already listed
// in the download archive.
const archiveSkipMsg = "has already been recorded in the archive"

// archiveSkipPrefix is the diagnostic prefix yt-dlp puts in front of its skip
// notices, for example "[download] <title> has already been recorded in the
// archive".
const archiveSkipPrefix = "[download]"

// archiveSkipLine reports whether a raw yt-dlp output line is an
// already-recorded-in-the-archive notice. Detection is deliberately anchored:
// the trimmed line must start with the "[download]" diagnostic prefix and
// contain archiveSkipMsg, so unrelated download output that merely quotes the
// phrase cannot spoof a skip.
func archiveSkipLine(raw string) bool {
	line := strings.TrimSpace(raw)
	if len(line) < len(archiveSkipPrefix) || !strings.EqualFold(line[:len(archiveSkipPrefix)], archiveSkipPrefix) {
		return false
	}
	return strings.Contains(strings.ToLower(line), archiveSkipMsg)
}

// readArchive loads the set of "youtube" video IDs already recorded in a
// yt-dlp archive file. A missing or unreadable file yields an empty set so
// downloads proceed; malformed lines are ignored.
func readArchive(path string) map[string]bool {
	ids := make(map[string]bool)

	f, err := os.Open(path)
	if err != nil {
		return ids
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 2 && strings.EqualFold(fields[0], "youtube") {
			ids[fields[1]] = true
		}
	}
	return ids
}
