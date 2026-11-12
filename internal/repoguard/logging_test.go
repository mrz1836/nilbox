package repoguard_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The app logs through one logger, which the desktop bootstrap builds with
// internal/logging and injects into everything that logs. A call to a process-wide logger
// would bypass it: the record would miss the log file, or land where nobody reads it.

// globalLoggerFuncs are the package-level functions that log through, or replace, a
// process-wide logger, by import path: slog's default logger and the standard log
// package's.
func globalLoggerFuncs() map[string][]string {
	return map[string][]string{
		"log/slog": {
			"Debug", "DebugContext", "Default", "Error", "ErrorContext", "Info", "InfoContext", "Log", "LogAttrs",
			"SetDefault", "SetLogLoggerLevel", "Warn", "WarnContext",
		},
		"log": {
			"Default", "Fatal", "Fatalf", "Fatalln", "Output", "Panic", "Panicf", "Panicln", "Print", "Printf",
			"Println", "SetFlags", "SetOutput", "SetPrefix", "Writer",
		},
	}
}

// loggingScope reports whether the Go file rel is production code the guard covers:
// anything under internal/ or cmd/ that is not a test.
func loggingScope(rel string) bool {
	if strings.HasSuffix(rel, "_test.go") {
		return false
	}

	return strings.HasPrefix(rel, "internal/") || strings.HasPrefix(rel, "cmd/")
}

// globalLoggerUses returns "file:line: what" for every use of a process-wide logger in
// src, the Go file rel, as globalLoggerUsesIn tells them.
func globalLoggerUses(rel, src string) ([]string, error) {
	fset := token.NewFileSet()

	// Object resolution stays on: it tells a package name from a local variable that
	// shadows it, such as a *slog.Logger parameter named log.
	file, err := parser.ParseFile(fset, rel, src, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", rel, err)
	}

	return globalLoggerUsesIn(fset, rel, file), nil
}

// globalLoggerUsesIn returns "file:line: what" for every use of a process-wide logger in
// file, the Go file rel parsed with object resolution on: a reference to one of
// globalLoggerFuncs through its package, or a dot import of either package, which would
// hide such calls.
func globalLoggerUsesIn(fset *token.FileSet, rel string, file *ast.File) []string {
	packages, found := loggerImports(fset, rel, file)
	funcs := globalLoggerFuncs()

	ast.Inspect(file, func(n ast.Node) bool {
		if pkg, name, ok := packageSelector(n); ok && slices.Contains(funcs[packages[pkg]], name) {
			found = append(found, fmt.Sprintf("%s:%d: %s.%s", rel, fset.Position(n.Pos()).Line, pkg, name))
		}

		return true
	})

	return found
}

// loggerImports returns the name each logger package in globalLoggerFuncs is imported
// under in file, with "file:line: what" for each dot import of one.
func loggerImports(fset *token.FileSet, rel string, file *ast.File) (map[string]string, []string) {
	funcs := globalLoggerFuncs()
	packages := map[string]string{}

	var dots []string

	for _, imp := range file.Imports {
		importPath, err := strconv.Unquote(imp.Path.Value)
		if err != nil || funcs[importPath] == nil {
			continue
		}

		name := path.Base(importPath)
		if imp.Name != nil {
			name = imp.Name.Name
		}

		if name == "." {
			dots = append(dots, fmt.Sprintf("%s:%d: dot import of %s", rel, fset.Position(imp.Pos()).Line, importPath))
		}

		packages[name] = importPath
	}

	return packages, dots
}

// packageSelector returns the package name and the selected name of n when n selects
// from a package: pkg.Name, where pkg resolves to no declaration in the file, unlike a
// local variable that shadows the package.
func packageSelector(n ast.Node) (pkg, name string, ok bool) {
	sel, isSel := n.(*ast.SelectorExpr)
	if !isSel {
		return "", "", false
	}

	ident, isIdent := sel.X.(*ast.Ident)
	if !isIdent || ident.Obj != nil {
		return "", "", false
	}

	return ident.Name, sel.Sel.Name, true
}

func TestNoGlobalLogger(t *testing.T) {
	t.Parallel()

	var found []string

	// Every Go file is parsed whatever its build tags, so a tag cannot hide a call.
	for _, rel := range goFiles(t) {
		if !loggingScope(rel) {
			continue
		}

		if fset, file, ok := goSyntax(t, rel); ok {
			found = append(found, globalLoggerUsesIn(fset, rel, file)...)
		}
	}

	assert.Empty(t, found,
		"log through the *slog.Logger the service was given (a Config field or option; nil discards), "+
			"never through slog's default logger or the log package")
}

func TestGlobalLoggerUses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "default logger calls",
			src: `package p
import "log/slog"
func f() { slog.Info("x"); slog.WarnContext(nil, "y"); _ = slog.Default() }`,
			want: []string{"p.go:3: slog.Info", "p.go:3: slog.WarnContext", "p.go:3: slog.Default"},
		},
		{
			name: "replacing the default",
			src: `package p
import "log/slog"
func f(l *slog.Logger) { slog.SetDefault(l) }`,
			want: []string{"p.go:3: slog.SetDefault"},
		},
		{
			name: "a function value",
			src: `package p
import "log/slog"
var logf = slog.Error`,
			want: []string{"p.go:3: slog.Error"},
		},
		{
			name: "renamed import",
			src: `package p
import logs "log/slog"
func f() { logs.Debug("x") }`,
			want: []string{"p.go:3: logs.Debug"},
		},
		{
			name: "the standard log package",
			src: `package p
import "log"
func f() { log.Printf("x"); log.Fatal("y") }`,
			want: []string{"p.go:3: log.Printf", "p.go:3: log.Fatal"},
		},
		{
			name: "dot import",
			src: `package p
import . "log/slog"
func f() { Info("x") }`,
			want: []string{"p.go:2: dot import of log/slog"},
		},
		{
			name: "an injected logger",
			src: `package p
import "log/slog"
func f(log *slog.Logger) { log.Info("x"); log.Warn("y"); _ = slog.New(slog.DiscardHandler) }`,
		},
		{
			name: "a variable that shadows the log package",
			src: `package p
import ("log"; "log/slog")
var _ = log.Prefix
func f(log *slog.Logger) { log.Info("x") }`,
		},
		{
			name: "another package with the same name",
			src: `package p
import slog "example.com/other/slog"
func f() { slog.Info("x") }`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := globalLoggerUses("p.go", tt.src)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	t.Run("scope", func(t *testing.T) {
		t.Parallel()

		assert.True(t, loggingScope("internal/inbox/service.go"))
		assert.True(t, loggingScope("cmd/nilbox/main_desktop.go"))
		assert.False(t, loggingScope("internal/inbox/service_test.go"))
		assert.False(t, loggingScope("magefiles/app.go"))
	})
}
