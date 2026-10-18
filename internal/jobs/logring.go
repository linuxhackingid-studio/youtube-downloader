package jobs

import "strings"

const (
	// maxLogLines is the capacity of the global log ring buffer.
	maxLogLines = 200
	// maxLogLineLen bounds each stored log line in bytes so a single
	// oversized progress line cannot inflate the ring buffer's memory
	// footprint. This is a byte bound only; the render side sanitizes
	// and truncates for display.
	maxLogLineLen = 4096
)

// appendLog adds one line to the bounded global log ring. Each stored line
// is capped at maxLogLineLen bytes.
func (m *Manager) appendLog(line string) {
	if len(line) > maxLogLineLen {
		// Cap by bytes, then drop an incomplete UTF-8 sequence split by
		// the cut so the ring never retains stray continuation bytes.
		line = strings.ToValidUTF8(line[:maxLogLineLen], "")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.logs = append(m.logs, line)
	if over := len(m.logs) - maxLogLines; over > 0 {
		n := copy(m.logs, m.logs[over:])
		m.logs = m.logs[:n]
	}
	m.rev++
}
