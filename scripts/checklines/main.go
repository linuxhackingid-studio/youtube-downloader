// Command checklines enforces repository hygiene rules:
//
//  1. No file in the tree may exceed 800 lines.
//  2. No Go, Markdown, YAML, or workflow file may contain an em dash
//     (U+2014) or an en dash (U+2013).
//
// Generated build and coverage artifacts (*.out, *.prof) are skipped: they
// are git-ignored output rather than repository content, so a developer who
// writes a coverprofile before running this guard does not trip it.
//
// The command is stdlib-only and does not shell out, so it behaves the
// same on Linux, macOS, and Windows.
//
// Usage:
//
//	go run ./scripts/checklines [-root dir] [-quiet]
//
// Exit status is 0 when the tree is clean, 1 when at least one violation
// was found, and 2 for usage or I/O errors.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// maxLines is the maximum number of lines allowed in any single file.
const maxLines = 800

// The dash runes are written as escape sequences on purpose: this file is
// part of the scanned tree and must therefore satisfy its own rule.
var (
	emDash = '\u2014'
	enDash = '\u2013'
)

// skipDirs lists directory names that are excluded from the scan at any depth.
var skipDirs = map[string]bool{
	".git":         true,
	"bin":          true,
	"dist":         true,
	"vendor":       true,
	"node_modules": true,
	".idea":        true,
	".vscode":      true,
}

// skipArtifactExts lists the generated build and coverage artifact
// extensions excluded from the scan. They are git-ignored by convention
// (see .gitignore), so scanning them would only produce false failures for
// a developer who ran a coverage or profiling command in the tree.
var skipArtifactExts = map[string]bool{
	".out":  true,
	".prof": true,
}

// dashExts lists the file extensions subject to the dash rule.
var dashExts = map[string]bool{
	".go":   true,
	".md":   true,
	".yml":  true,
	".yaml": true,
}

// violation describes a single rule break. Paths are rendered relative to
// the module root with forward slashes so output is identical everywhere.
type violation struct {
	path string
	line int // 0 when the violation is not tied to a specific line
	msg  string
}

func (v violation) String() string {
	if v.line > 0 {
		return fmt.Sprintf("%s:%d: %s", v.path, v.line, v.msg)
	}
	return fmt.Sprintf("%s: %s", v.path, v.msg)
}

func main() {
	root := flag.String("root", ".", "directory to start the module root search from")
	quiet := flag.Bool("quiet", false, "print violations only, without the summary")
	flag.Parse()
	os.Exit(run(*root, *quiet))
}

func run(root string, quiet bool) int {
	moduleRoot, err := findModuleRoot(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "checklines: %v\n", err)
		return 2
	}

	viols, scanned, err := scan(moduleRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "checklines: %v\n", err)
		return 2
	}

	if len(viols) == 0 {
		if !quiet {
			fmt.Printf("checklines: OK, %d files scanned, no violations\n", scanned)
		}
		return 0
	}
	if !quiet {
		fmt.Printf("checklines: %d violations in %d files scanned:\n", len(viols), scanned)
	}
	for _, v := range viols {
		fmt.Printf("  %s\n", v)
	}
	return 1
}

// findModuleRoot walks up from start until it finds the directory that
// holds go.mod. Starting from the working directory keeps the tool safe to
// run from any subdirectory of the repository.
func findModuleRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if fi, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil && !fi.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod found in %s or any parent directory", start)
		}
		dir = parent
	}
}

// scan walks the module root and collects violations in walk order, which
// is deterministic because filepath.WalkDir visits entries lexically.
func scan(root string) ([]violation, int, error) {
	var (
		viols   []violation
		scanned int
	)
	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && skipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if skipArtifactExts[strings.ToLower(path.Ext(d.Name()))] {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		scanned++
		fileViols, err := checkFile(filepath.ToSlash(rel), p)
		if err != nil {
			return err
		}
		viols = append(viols, fileViols...)
		return nil
	})
	if walkErr != nil {
		return nil, scanned, walkErr
	}
	return viols, scanned, nil
}

// checkFile applies both rules to a single file. The line-count rule
// applies to every file; the dash rule is limited to source, docs, and
// workflow files.
func checkFile(rel, abs string) ([]violation, error) {
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}

	var viols []violation
	if n := countLines(data); n > maxLines {
		viols = append(viols, violation{
			path: rel,
			msg:  fmt.Sprintf("%d lines (max %d)", n, maxLines),
		})
	}

	if !dashExts[strings.ToLower(path.Ext(rel))] {
		return viols, nil
	}
	for i, line := range strings.Split(string(data), "\n") {
		if strings.ContainsRune(line, emDash) {
			viols = append(viols, violation{path: rel, line: i + 1, msg: "em dash"})
		}
		if strings.ContainsRune(line, enDash) {
			viols = append(viols, violation{path: rel, line: i + 1, msg: "en dash"})
		}
	}
	return viols, nil
}

// countLines counts newline-separated records. A trailing record without a
// final newline still counts as one line; an empty file counts as zero.
func countLines(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	n := bytes.Count(data, []byte{'\n'})
	if data[len(data)-1] != '\n' {
		n++
	}
	return n
}
