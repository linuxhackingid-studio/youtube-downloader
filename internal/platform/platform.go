// Package platform adapts the application to the operating system it runs on:
// a human-readable platform label for the setup screen and a native "open
// this folder" launcher for the finished screen. It depends on the standard
// library only and builds on Windows, macOS, Linux and the other Unix
// systems.
package platform

import "runtime"

// Name returns the friendly name of the operating system this binary runs on:
// "Windows", "macOS" or "Linux". Unknown systems keep their GOOS value, so
// the label is never empty.
func Name() string {
	return nameFor(runtime.GOOS)
}

// Label returns the platform name together with the architecture, for
// example "Windows (amd64)" or "macOS (arm64)". The setup screen shows it so
// that a report of a platform-specific problem can name both without the user
// digging for them.
func Label() string {
	return labelFor(runtime.GOOS, runtime.GOARCH)
}

// nameFor maps a GOOS value to its friendly name so every mapping is testable
// on any machine. Unmapped systems pass through unchanged.
func nameFor(goos string) string {
	switch goos {
	case "windows":
		return "Windows"
	case "darwin":
		return "macOS"
	case "linux":
		return "Linux"
	default:
		return goos
	}
}

// labelFor combines the friendly OS name with the architecture, e.g.
// "Linux (arm64)".
func labelFor(goos, goarch string) string {
	return nameFor(goos) + " (" + goarch + ")"
}
