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

// withoutTypeArgs returns fun with any type arguments taken off: the selector or
// identifier a generic function's instantiation names.
func withoutTypeArgs(fun ast.Expr) ast.Expr {
	switch f := fun.(type) {
	case *ast.IndexExpr:
		return f.X
	case *ast.IndexListExpr:
		return f.X
	default:
		return fun
	}
}

// isCallTo reports whether expr calls the function named name, with no arguments.
func isCallTo(expr ast.Expr, name string) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}

	ident, ok := withoutTypeArgs(call.Fun).(*ast.Ident)

	return ok && ident.Name == name
}

// serviceBindings returns how many services files bind with the runtime's
// NewServiceWithOptions and the bound options, and the position of every reference that
// binds one otherwise: NewService (called or taken as a value), NewServiceWithOptions with
// other options or taken as a value, or an App's RegisterService.
func serviceBindings(fset *token.FileSet, files []*ast.File) (bound int, problems []string) {
	for _, file := range files {
		imports := importNames(file)
		calls := calledSelectors(file)

		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}

			switch ok, problem := checkBinding(sel, calls[sel], imports); {
			case problem != "":
				problems = append(problems, fset.Position(sel.Pos()).String()+": "+problem)
			case ok:
				bound++
			}

			return true
		})
	}

	return bound, problems
}

// calledSelectors maps each selector file calls, type arguments aside, to its call.
func calledSelectors(file *ast.File) map[*ast.SelectorExpr]*ast.CallExpr {
	out := map[*ast.SelectorExpr]*ast.CallExpr{}

	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if sel, ok := withoutTypeArgs(call.Fun).(*ast.SelectorExpr); ok {
				out[sel] = call
			}
		}

		return true
	})

	return out
}

// checkBinding reports whether sel, called as call (nil when it is not called), binds a
// service with the runtime's NewServiceWithOptions and the bound options, or what is wrong
// with a reference that binds one otherwise; a reference that binds nothing is neither.
func checkBinding(sel *ast.SelectorExpr, call *ast.CallExpr, imports map[string]string) (bound bool, problem string) {
	if sel.Sel.Name == "RegisterService" {
		return false, "RegisterService"
	}

	switch name, _ := refIn(sel, imports, wailsApplication); name {
	case "NewService":
		return false, "NewService"
	case "NewServiceWithOptions":
		if call == nil || len(call.Args) != 2 || !isCallTo(call.Args[1], boundOptionsFunc) {
			return false, "NewServiceWithOptions other than with " + boundOptionsFunc + "()"
		}

		return true, ""
	default:
		return false, ""
	}
}

// boundMarshaler reports whether files declare the bound options function, and every
// declaration of it, whatever its build tags, is one return of the runtime's
// ServiceOptions whose MarshalError is desktop.MarshalError.
func boundMarshaler(files []*ast.File, desktopPath string) bool {
	found := false

	for _, file := range files {
		for _, decl := range file.Decls {
			result, ok := boundOptionsResult(decl)
			if !ok {
				continue
			}

			if !marshalsWithDesktop(result, importNames(file), desktopPath) {
				return false
			}

			found = true
		}
	}

	return found
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
	fset, files := dirFiles(t, "cmd/"+brand.Slug)

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

func bind[T any](s *T, a *app.App) {
	_ = app.NewServiceWithOptions(s, boundOptions())
	_ = app.NewServiceWithOptions[T](s, boundOptions())
	_ = app.NewService(s)
	_ = app.NewServiceWithOptions(s, app.ServiceOptions{})
	_ = app.NewServiceWithOptions(s, otherOptions())
	f := app.NewService[T]
	g := app.NewServiceWithOptions[T]
	a.RegisterService(f(s))
}
`, 0)
	require.NoError(t, err)

	files := []*ast.File{file}

	bound, problems := serviceBindings(fset, files)
	assert.Equal(t, 2, bound, "with or without type arguments")
	assert.Equal(t, []string{
		"main.go:15:6: NewService",
		"main.go:16:6: NewServiceWithOptions other than with boundOptions()",
		"main.go:17:6: NewServiceWithOptions other than with boundOptions()",
		"main.go:18:7: NewService",
		"main.go:19:7: NewServiceWithOptions other than with boundOptions()",
		"main.go:20:2: RegisterService",
	}, problems, "a call, a function value, and a registration alike")
	assert.True(t, boundMarshaler(files, desktopPath))
	assert.False(t, boundMarshaler(files, "example.com/m/internal/other"), "another package's MarshalError")

	other, err := parser.ParseFile(fset, "main_other.go", `package main

import "github.com/wailsapp/wails/v3/pkg/application"

func boundOptions() application.ServiceOptions {
	return application.ServiceOptions{}
}
`, 0)
	require.NoError(t, err)
	assert.False(t, boundMarshaler([]*ast.File{file, other}, desktopPath),
		"every declaration counts, as another build's file can declare its own")
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
