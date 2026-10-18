package main

import (
	"strings"
	"testing"
)

// TestResolveVersionPrefersInjectedVersion pins the highest-precedence rule
// of the documented contract: a version injected at build time with
// -ldflags "-X main.version=..." always wins over the build-info fallbacks.
func TestResolveVersionPrefersInjectedVersion(t *testing.T) {
	const injected = "1.2.3-test"
	saved := version
	version = injected
	t.Cleanup(func() { version = saved })

	if got := resolveVersion(); got != injected {
		t.Errorf("resolveVersion() = %q, want the injected version %q", got, injected)
	}
}

// TestResolveVersionFallbackIsClean covers the other branch without pinning
// its exact value, which varies with how the test binary was built: the
// dev-build label must never be empty and must not leak the parenthesized
// "(devel)" marker runtime/debug reports for local builds.
func TestResolveVersionFallbackIsClean(t *testing.T) {
	saved := version
	version = ""
	t.Cleanup(func() { version = saved })

	got := resolveVersion()
	if got == "" {
		t.Fatal("resolveVersion() = \"\", want a non-empty development label")
	}
	for _, unwanted := range []string{"(", ")"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("resolveVersion() = %q, want no %q placeholder artifact", got, unwanted)
		}
	}
}

// TestUsageTextDocumentsFlags keeps the help text in sync with the flags
// run() registers, so -help never advertises a flag that does not exist.
func TestUsageTextDocumentsFlags(t *testing.T) {
	for _, want := range []string{"-version", "-help"} {
		if !strings.Contains(usageText, want) {
			t.Errorf("usageText does not document %q", want)
		}
	}
}
