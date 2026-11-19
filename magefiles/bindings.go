//go:build mage

package main

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/brand"
)

const (
	// goModFile declares the module whose packages the bindings render.
	goModFile = "go.mod"

	// modelsFile is the generated file that holds a package's models, its enums among
	// them.
	modelsFile = "models.ts"

	// enumEnd closes the const object unionEnum writes for an enum.
	enumEnd = "} as const;"
)

var (
	// errSplitEnum is returned when an enum the bindings render has its Go constants in
	// more than one file (see checkEnumFiles).
	errSplitEnum = errors.New("the binding generator orders the members of an enum whose constants " +
		"are in more than one file differently from run to run; declare each one's constants in one file")

	// errNoModule is returned when go.mod declares no module path.
	errNoModule = errors.New("go.mod declares no module")

	// enumObject matches the first line of the const object unionEnum writes for an enum,
	// capturing the enum's name.
	enumObject = regexp.MustCompile(`^export const ([\w$]+) = \{$`)

	// enumKey matches one member of that object, capturing the name of its Go constant.
	enumKey = regexp.MustCompile(`^\s+([\w$]+): .+,$`)
)

// generateBindings writes the TypeScript bindings for the desktop command into out, in
// the checkout when it is relative, running the wails3 CLI at the version go.mod pins.
func (r runner) generateBindings(out string) error {
	if err := r.ensureDist(); err != nil {
		return err
	}

	cli, err := r.wailsCLI()
	if err != nil {
		return err
	}

	out = r.path(out)

	err = r.runV("go", "run", cli, "generate", "bindings",
		"-ts", "-i", "-f", "-tags "+desktopTag, "-d", out, "./cmd/"+brand.Slug)
	if err != nil {
		return fmt.Errorf("generate bindings: %w", err)
	}

	if err = tidyBindings(out); err != nil {
		return err
	}

	return r.checkEnumFiles(out)
}

// checkEnumFiles fails, naming each one, when an enum the bindings under root render has
// its Go constants declared in more than one file of its package.
//
// The generator orders an enum's members by the source positions of their declarations,
// comparing them across files too, and those positions are not stable across files:
// go/packages parses a package's files concurrently, and each file's positions start
// wherever the shared token.FileSet stood when its parse began. So an enum whose constants
// sit in two files comes out in either order from one run to the next, and the drift
// check fails on a tree that regenerating doesn't change. Within one file the order is the
// source order every time, so holding each enum to one file makes the bindings the same
// on every run. Only this module's packages are checked, the checkout's: they are the
// ones it can change.
func (r runner) checkEnumFiles(root string) error {
	files, err := treeFiles(root)
	if err != nil {
		return err
	}

	enums := boundEnums(files)
	if len(enums) == 0 {
		return nil
	}

	module, err := modulePath(r.path(goModFile))
	if err != nil {
		return err
	}

	var split []string

	for _, pkg := range slices.Sorted(maps.Keys(enums)) {
		dir, ours := moduleDir(module, pkg)
		if !ours {
			continue
		}

		consts, err := r.constFiles(dir)
		if err != nil {
			return err
		}

		split = append(split, splitEnums(pkg, enums[pkg], consts)...)
	}

	if len(split) > 0 {
		return fmt.Errorf("%w:\n  %s", errSplitEnum, strings.Join(split, "\n  "))
	}

	return nil
}

// splitEnums returns a line, in name order, for each enum of the package with import path
// pkg whose constants consts places in more than one file, naming the files.
func splitEnums(pkg string, enums map[string][]string, consts map[string]string) []string {
	var split []string

	for _, name := range slices.Sorted(maps.Keys(enums)) {
		var in []string

		for _, member := range enums[name] {
			if file, ok := consts[member]; ok && !slices.Contains(in, file) {
				in = append(in, file)
			}
		}

		if len(in) > 1 {
			slices.Sort(in)
			split = append(split, pkg+"."+name+": "+strings.Join(in, ", "))
		}
	}

	return split
}

// boundEnums returns the enums the tidy bindings in files render, by the import path of
// their package and then by name, each with the names of its Go constants.
func boundEnums(files map[string][]byte) map[string]map[string][]string {
	enums := map[string]map[string][]string{}

	for rel, body := range files {
		if path.Base(rel) != modelsFile {
			continue
		}

		if found := fileEnums(body); len(found) > 0 {
			enums[path.Dir(rel)] = found
		}
	}

	return enums
}

// fileEnums returns the enums in one tidy models file, by name, each with the names of
// its Go constants.
func fileEnums(body []byte) map[string][]string {
	enums := map[string][]string{}
	name := ""

	for line := range strings.SplitSeq(string(body), "\n") {
		switch {
		case name == "":
			if start := enumObject.FindStringSubmatch(line); start != nil {
				name = start[1]
			}
		case line == enumEnd:
			name = ""
		default:
			if member := enumKey.FindStringSubmatch(line); member != nil {
				enums[name] = append(enums[name], member[1])
			}
		}
	}

	return enums
}

// modulePath returns the module path the go.mod file at file declares.
func modulePath(file string) (string, error) {
	body, err := os.ReadFile(file) //nolint:gosec // this repository's own go.mod
	if err != nil {
		return "", fmt.Errorf("read %s: %w", file, err)
	}

	for line := range strings.SplitSeq(string(body), "\n") {
		if fields := strings.Fields(line); len(fields) >= 2 && fields[0] == "module" {
			return strings.Trim(fields[1], "\"`"), nil
		}
	}

	return "", errNoModule
}

// moduleDir returns the directory, relative to the module's root, of the package with
// import path pkg, and false when pkg is not in module.
func moduleDir(module, pkg string) (string, bool) {
	if pkg == module {
		return ".", true
	}

	rel, ok := strings.CutPrefix(pkg, module+"/")

	return filepath.FromSlash(rel), ok
}

// constFiles returns, by name, the file that declares each package-level constant of the
// package in dir, in the checkout, reading the files the desktop build compiles, as the
// generator does.
func (r runner) constFiles(dir string) (map[string]string, error) {
	ctxt := build.Default
	ctxt.BuildTags = append(slices.Clone(ctxt.BuildTags), desktopTag)

	pkgDir := r.path(dir)

	pkg, err := ctxt.ImportDir(pkgDir, 0)
	if err != nil {
		return nil, fmt.Errorf("read the package in %s: %w", dir, err)
	}

	fset := token.NewFileSet()
	consts := map[string]string{}

	for _, name := range slices.Concat(pkg.GoFiles, pkg.CgoFiles) {
		file, err := parser.ParseFile(fset, filepath.Join(pkgDir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("read the constants in %s: %w", dir, err)
		}

		for _, constant := range packageConsts(file) {
			consts[constant] = name
		}
	}

	return consts, nil
}

// packageConsts returns the names of the package-level constants file declares.
func packageConsts(file *ast.File) []string {
	var names []string

	for _, decl := range file.Decls {
		if gen, ok := decl.(*ast.GenDecl); ok && gen.Tok == token.CONST {
			names = append(names, specNames(gen.Specs)...)
		}
	}

	return names
}

// specNames returns the names the value specs among specs declare.
func specNames(specs []ast.Spec) []string {
	var names []string

	for _, spec := range specs {
		if value, ok := spec.(*ast.ValueSpec); ok {
			for _, id := range value.Names {
				names = append(names, id.Name)
			}
		}
	}

	return names
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
func (r runner) wailsCLI() (string, error) {
	version, err := r.output("go", "list", "-m", "-f", "{{.Version}}", wailsModule)
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
