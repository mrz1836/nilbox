package repoguard_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
)

const (
	// wailsApplication is the desktop runtime's package that binds services.
	wailsApplication = "github.com/wailsapp/wails/v3/pkg/application"

	// boundOptionsFunc is the desktop bootstrap's function that returns the options every
	// bound service is bound with.
	boundOptionsFunc = "boundOptions"
)

// calledFunc returns the function call calls with any type arguments taken off: the
// selector or identifier it names.
func calledFunc(call *ast.CallExpr) ast.Expr {
	switch fun := call.Fun.(type) {
	case *ast.IndexExpr:
		return fun.X
	case *ast.IndexListExpr:
		return fun.X
	default:
		return call.Fun
	}
}

// isCallTo reports whether expr calls the function named name, with no arguments.
func isCallTo(expr ast.Expr, name string) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}

	ident, ok := calledFunc(call).(*ast.Ident)

	return ok && ident.Name == name
}

// serviceBindings returns how many services files bind with the runtime's
// NewServiceWithOptions and the bound options, and the position of every call that binds
// one otherwise: through NewService, or with other options.
func serviceBindings(fset *token.FileSet, files []*ast.File) (bound int, problems []string) {
	for _, file := range files {
		imports := importNames(file)

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}

			switch ok, problem := checkBinding(call, imports); {
			case problem != "":
				problems = append(problems, fset.Position(call.Pos()).String()+": "+problem)
			case ok:
				bound++
			}

			return true
		})
	}

	return bound, problems
}

// checkBinding reports whether call binds a service with the runtime's
// NewServiceWithOptions and the bound options, or what is wrong with a call that binds one
// otherwise; a call that binds nothing is neither.
func checkBinding(call *ast.CallExpr, imports map[string]string) (bound bool, problem string) {
	switch name, _ := refIn(calledFunc(call), imports, wailsApplication); name {
	case "NewService":
		return false, "NewService"
	case "NewServiceWithOptions":
		if len(call.Args) != 2 || !isCallTo(call.Args[1], boundOptionsFunc) {
			return false, "options other than " + boundOptionsFunc + "()"
		}

		return true, ""
	default:
		return false, ""
	}
}

// boundMarshaler reports whether files declare the bound options function as one return
// of the runtime's ServiceOptions whose MarshalError is desktop.MarshalError.
func boundMarshaler(files []*ast.File, desktopPath string) bool {
	for _, file := range files {
		for _, decl := range file.Decls {
			if result, ok := boundOptionsResult(decl); ok {
				return marshalsWithDesktop(result, importNames(file), desktopPath)
			}
		}
	}

	return false
}

// boundOptionsResult returns what decl returns when it is the bound options function and
// its body is that one return, with nil for a body that is anything else; false means decl
// is another declaration.
func boundOptionsResult(decl ast.Decl) (ast.Expr, bool) {
	fn, ok := decl.(*ast.FuncDecl)
	if !ok || fn.Recv != nil || fn.Name.Name != boundOptionsFunc {
		return nil, false
	}

	if fn.Body == nil || len(fn.Body.List) != 1 {
		return nil, true
	}

	ret, ok := fn.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return nil, true
	}

	return ret.Results[0], true
}

// marshalsWithDesktop reports whether expr is the runtime's ServiceOptions with
// MarshalError set to desktop.MarshalError.
func marshalsWithDesktop(expr ast.Expr, imports map[string]string, desktopPath string) bool {
	lit, ok := expr.(*ast.CompositeLit)
	if !ok {
		return false
	}

	if name, ok := refIn(lit.Type, imports, wailsApplication); !ok || name != "ServiceOptions" {
		return false
	}

	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}

		if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "MarshalError" {
			name, ok := refIn(kv.Value, imports, desktopPath)

			return ok && name == "MarshalError"
		}
	}

	return false
}

// TestBoundServicesWordTheirErrors checks that the desktop bootstrap binds every service
// with boundOptions, whose MarshalError is desktop.MarshalError. The runtime encodes a
// bound method's error with its own service's marshaler alone, never the application's
// MarshalError option, so a service bound any other way sends the window its errors
// without what the user is told about them, or the detail it needs to answer one (a
// repository command's trust question).
func TestBoundServicesWordTheirErrors(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)
	fset, files := dirFiles(t, root, "cmd/"+brand.Slug)

	bound, problems := serviceBindings(fset, files)
	assert.Empty(t, problems, "bind every service with application.NewServiceWithOptions(svc, %s())", boundOptionsFunc)
	assert.Positive(t, bound, "the desktop bootstrap binds no service; update this guard")
	assert.True(t, boundMarshaler(files, modulePath(t, root)+"/internal/desktop"),
		"%s must return application.ServiceOptions{MarshalError: desktop.MarshalError}", boundOptionsFunc)
}

func TestServiceBindings(t *testing.T) {
	t.Parallel()

	const desktopPath = "example.com/m/internal/desktop"

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", `package main

import (
	"example.com/m/internal/desktop"
	app "github.com/wailsapp/wails/v3/pkg/application"
)

func boundOptions() app.ServiceOptions {
	return app.ServiceOptions{Name: "x", MarshalError: desktop.MarshalError}
}

func bind[T any](s *T) {
	_ = app.NewServiceWithOptions(s, boundOptions())
	_ = app.NewServiceWithOptions[T](s, boundOptions())
	_ = app.NewService(s)
	_ = app.NewServiceWithOptions(s, app.ServiceOptions{})
	_ = app.NewServiceWithOptions(s, otherOptions())
}
`, 0)
	require.NoError(t, err)

	files := []*ast.File{file}

	bound, problems := serviceBindings(fset, files)
	assert.Equal(t, 2, bound, "with or without type arguments")
	assert.Equal(t, []string{
		"main.go:15:6: NewService",
		"main.go:16:6: options other than boundOptions()",
		"main.go:17:6: options other than boundOptions()",
	}, problems)
	assert.True(t, boundMarshaler(files, desktopPath))
	assert.False(t, boundMarshaler(files, "example.com/m/internal/other"), "another package's MarshalError")
}

func TestBoundMarshalerNeedsDesktopMarshalError(t *testing.T) {
	t.Parallel()

	const desktopPath = "example.com/m/internal/desktop"

	for name, body := range map[string]string{
		"no marshaler":       `return application.ServiceOptions{}`,
		"another marshaler":  `return application.ServiceOptions{MarshalError: other}`,
		"another type":       `return desktop.Options{MarshalError: desktop.MarshalError}`,
		"more than a return": `o := application.ServiceOptions{MarshalError: desktop.MarshalError}; return o`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			file, err := parser.ParseFile(token.NewFileSet(), "main.go", `package main

import (
	"example.com/m/internal/desktop"
	"github.com/wailsapp/wails/v3/pkg/application"
)

func boundOptions() application.ServiceOptions {
	`+body+`
}
`, 0)
			require.NoError(t, err)
			assert.False(t, boundMarshaler([]*ast.File{file}, desktopPath))
		})
	}
}
