//go:build mage

package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/magefile/mage/sh"

	"github.com/mrz1836/nilbox/internal/brand"
)

// generateBindings writes the TypeScript bindings for the desktop command into out,
// running the wails3 CLI at the version go.mod pins.
func generateBindings(out string) error {
	if err := ensureDist(); err != nil {
		return err
	}

	cli, err := wailsCLI()
	if err != nil {
		return err
	}

	err = sh.RunV("go", "run", cli, "generate", "bindings",
		"-ts", "-i", "-f", "-tags "+desktopTag, "-d", out, "./cmd/"+brand.Slug)
	if err != nil {
		return fmt.Errorf("generate bindings: %w", err)
	}

	return tidyBindings(out)
}

// tidyBindings rewrites each generated file under root the way the repository keeps it
// (see trimTrailingSpace and unionEnums), so the committed bindings, a fresh generation,
// and the drift check stay identical. A file that is already tidy is left untouched.
func tidyBindings(root string) error {
	files, err := treeFiles(root)
	if err != nil {
		return err
	}

	for rel, body := range files {
		tidy := unionEnums(trimTrailingSpace(body))
		if bytes.Equal(tidy, body) {
			continue
		}

		if err = os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), tidy, 0o644); err != nil { //nolint:gosec // generated sources, world-readable
			return fmt.Errorf("tidy %s: %w", rel, err)
		}
	}

	return nil
}

// trimTrailingSpace strips trailing spaces and tabs from every line of body. The
// generator leaves them on blank doc-comment lines, which the repository's whitespace
// check rejects.
func trimTrailingSpace(body []byte) []byte {
	lines := bytes.Split(body, []byte("\n"))
	for i, line := range lines {
		lines[i] = bytes.TrimRight(line, " \t")
	}

	return bytes.Join(lines, []byte("\n"))
}

// enumZero is the member the generator adds to every enum for the Go zero value.
const enumZero = "$zero"

var (
	// enumStart matches the first line of a generated enum, capturing its name.
	enumStart = regexp.MustCompile(`^export enum ([\w$]+) \{$`)

	// enumMember matches one member of a generated enum, capturing its indent, name,
	// and value.
	enumMember = regexp.MustCompile(`^(\s+)([\w$]+) = (.+),$`)
)

// unionEnums rewrites each enum in a generated file as a type that is the union of its
// values, and a const object of the same name that maps each Go constant's name to its
// value. The generator renders a Go string type with constants as a TypeScript enum,
// which the frontend's erasableSyntaxOnly rejects and which a plain 'pr' is not
// assignable to; the union keeps the values checked while literals still type-check.
// The enum's $zero member is left out: it is not one of the constants, and a type whose
// zero value means something declares a constant for it.
func unionEnums(body []byte) []byte {
	lines := strings.Split(string(body), "\n")
	out := make([]string, 0, len(lines))

	for i := 0; i < len(lines); i++ {
		start := enumStart.FindStringSubmatch(lines[i])
		if start == nil {
			out = append(out, lines[i])

			continue
		}

		end := slices.Index(lines[i+1:], "};")
		if end < 0 {
			// Not an enum as the generator writes one: leave the rest as it is.
			out = append(out, lines[i:]...)

			break
		}

		out = append(out, unionEnum(start[1], lines[i+1:i+1+end])...)
		i += end + 1
	}

	return []byte(strings.Join(out, "\n"))
}

// unionEnum returns the union type and the const object for the enum named name whose
// body (the lines between its braces) is given. Each member keeps the comments above
// it; the $zero member goes with its comment and the blank lines after it.
func unionEnum(name string, body []string) []string {
	var (
		members, pending []string
		values           []string
		afterZero        bool
	)

	for _, line := range body {
		member := enumMember.FindStringSubmatch(line)

		switch {
		case member == nil && afterZero && strings.TrimSpace(line) == "":
			continue
		case member == nil:
			pending = append(pending, line)
		case member[2] == enumZero:
			pending, afterZero = nil, true
		default:
			members = append(members, pending...)
			members = append(members, member[1]+member[2]+": "+member[3]+",")
			pending, afterZero = nil, false

			if !slices.Contains(values, member[3]) {
				values = append(values, member[3])
			}
		}
	}

	members = append(members, pending...)

	out := make([]string, 0, len(members)+8)
	out = append(out,
		"export type "+name+" = "+strings.Join(values, " | ")+";",
		"",
		"/**",
		" * The values of "+name+", by the names of their Go constants.",
		" */",
		"export const "+name+" = {",
	)
	out = append(out, members...)

	return append(out, "} as const;")
}

// wailsCLI returns the go-run package path of the wails3 CLI at the pinned version.
func wailsCLI() (string, error) {
	version, err := sh.Output("go", "list", "-m", "-f", "{{.Version}}", wailsModule)
	if err != nil {
		return "", fmt.Errorf("read the pinned %s version: %w", wailsModule, err)
	}

	return wailsModule + "/cmd/wails3@" + strings.TrimSpace(version), nil
}

// treeDiff compares two directory trees by relative path and content, and returns one
// line per added, removed, or changed file.
func treeDiff(want, got string) ([]string, error) {
	wantFiles, err := treeFiles(want)
	if err != nil {
		return nil, err
	}

	gotFiles, err := treeFiles(got)
	if err != nil {
		return nil, err
	}

	var drift []string

	for rel, body := range gotFiles {
		old, ok := wantFiles[rel]

		switch {
		case !ok:
			drift = append(drift, "missing: "+rel)
		case !bytes.Equal(old, body):
			drift = append(drift, "changed: "+rel)
		}
	}

	for rel := range wantFiles {
		if _, ok := gotFiles[rel]; !ok {
			drift = append(drift, "stale: "+rel)
		}
	}

	slices.Sort(drift)

	return drift, nil
}

// treeFiles reads every regular file under root, keyed by slash-separated relative path.
// A missing root is an empty tree.
func treeFiles(root string) (map[string][]byte, error) {
	files := map[string][]byte{}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.Type().IsRegular() {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		body, err := os.ReadFile(path) //nolint:gosec // walking this repository's own generated files
		if err != nil {
			return err
		}

		files[filepath.ToSlash(rel)] = body

		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return files, nil
	}

	if err != nil {
		return nil, fmt.Errorf("read %s: %w", root, err)
	}

	return files, nil
}
