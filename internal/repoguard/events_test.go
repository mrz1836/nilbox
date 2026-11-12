package repoguard_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/event"
)

// eventDir holds the event types and their names.
const eventDir = "internal/event"

// windowMarker is the method only window events have.
const windowMarker = "window"

// registration is one window event: its type and the name it is registered under.
type registration struct {
	typeName string
	name     string
}

// dirFiles returns the syntax of the Go files of dir, whatever their build tags, leaving
// out tests, and the file set that positions it.
func dirFiles(t *testing.T, dir string) (*token.FileSet, []*ast.File) {
	t.Helper()

	var files []*ast.File

	for _, rel := range goFiles(t) {
		if filepath.ToSlash(filepath.Dir(rel)) != dir || strings.HasSuffix(rel, "_test.go") {
			continue
		}

		_, file, ok := goSyntax(t, rel)
		require.True(t, ok, rel)

		files = append(files, file)
	}

	require.NotEmpty(t, files, "%s has no Go files; update this guard", dir)

	return repo(t).fset, files
}

// stringConstants returns the string constants files declare, by name.
func stringConstants(files []*ast.File) map[string]string {
	out := map[string]string{}

	for _, file := range files {
		for _, decl := range file.Decls {
			addStringConstants(decl, out)
		}
	}

	return out
}

// addStringConstants adds the string constants decl declares to out.
func addStringConstants(decl ast.Decl, out map[string]string) {
	gen, ok := decl.(*ast.GenDecl)
	if !ok || gen.Tok != token.CONST {
		return
	}

	for _, spec := range gen.Specs {
		if value, ok := spec.(*ast.ValueSpec); ok && len(value.Names) == len(value.Values) {
			addSpecStrings(value, out)
		}
	}
}

// addSpecStrings adds the names value declares with a string literal to out.
func addSpecStrings(value *ast.ValueSpec, out map[string]string) {
	for i, name := range value.Names {
		if s, ok := stringLiteral(value.Values[i]); ok {
			out[name.Name] = s
		}
	}
}

// stringLiteral returns the value of expr when it is a string literal.
func stringLiteral(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}

	s, err := strconv.Unquote(lit.Value)

	return s, err == nil
}

// receiverType returns the name of the type fn is a method of, or "" for a function.
func receiverType(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) != 1 {
		return ""
	}

	typ := fn.Recv.List[0].Type
	if star, ok := typ.(*ast.StarExpr); ok {
		typ = star.X
	}

	if ident, ok := typ.(*ast.Ident); ok {
		return ident.Name
	}

	return ""
}

// returnedIdent returns the identifier fn's body returns when the body is that one return
// and nothing else.
func returnedIdent(fn *ast.FuncDecl) (string, bool) {
	if fn.Body == nil || len(fn.Body.List) != 1 {
		return "", false
	}

	ret, ok := fn.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return "", false
	}

	ident, ok := ret.Results[0].(*ast.Ident)
	if !ok {
		return "", false
	}

	return ident.Name, true
}

// windowEvents returns every type files declare with the window marker method, under the
// name its Name method returns, read through constants. A window event whose Name does
// not return one of the constants is returned as a problem.
func windowEvents(files []*ast.File, constants map[string]string) ([]registration, []string) {
	marked, nameConst := eventMethods(files)

	found := make([]registration, 0, len(marked))

	var problems []string

	for typeName := range marked {
		name, ok := constants[nameConst[typeName]]
		if !ok {
			problems = append(problems, typeName+": Name must return one of the event name constants")
		}

		found = append(found, registration{typeName: typeName, name: name})
	}

	return found, problems
}

// eventMethods returns the types files declare with the window marker method, and the
// identifier each type's Name method returns, by type.
func eventMethods(files []*ast.File) (marked map[string]bool, nameConst map[string]string) {
	marked, nameConst = map[string]bool{}, map[string]string{}

	for _, file := range files {
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok {
				addEventMethod(fn, marked, nameConst)
			}
		}
	}

	return marked, nameConst
}

// addEventMethod records fn in marked when it is a type's window marker method, and in
// nameConst when it is a type's Name method that returns an identifier.
func addEventMethod(fn *ast.FuncDecl, marked map[string]bool, nameConst map[string]string) {
	recv := receiverType(fn)
	if recv == "" {
		return
	}

	switch fn.Name.Name {
	case windowMarker:
		marked[recv] = true
	case "Name":
		if ident, ok := returnedIdent(fn); ok {
			nameConst[recv] = ident
		}
	}
}

// importNames maps the name each import of file is known by in it to the import's path.
// Blank and dot imports name nothing.
func importNames(file *ast.File) map[string]string {
	out := make(map[string]string, len(file.Imports))

	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}

		name := path.Base(importPath)
		if spec.Name != nil {
			name = spec.Name.Name
		}

		if name != "_" && name != "." {
			out[name] = importPath
		}
	}

	return out
}

// refIn returns the name expr selects from the package at pkgPath, as imports name the
// file's imports, or false when expr selects nothing from that package.
func refIn(expr ast.Expr, imports map[string]string, pkgPath string) (string, bool) {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}

	pkg, ok := sel.X.(*ast.Ident)
	if !ok || imports[pkg.Name] != pkgPath {
		return "", false
	}

	return sel.Sel.Name, true
}

// isRegisterEvent reports whether fun is RegisterEvent instantiated with a type.
func isRegisterEvent(fun ast.Expr) bool {
	index, ok := fun.(*ast.IndexExpr)
	if !ok {
		return false
	}

	sel, ok := index.X.(*ast.SelectorExpr)

	return ok && sel.Sel.Name == "RegisterEvent"
}

// readRegistration reads a RegisterEvent call of the form
// RegisterEvent[event.Type](event.Name), where both event identifiers are the import of
// the event package at eventPath, and the name is one of its constants.
func readRegistration(
	call *ast.CallExpr, imports map[string]string, eventPath string, constants map[string]string,
) (registration, bool) {
	index, _ := call.Fun.(*ast.IndexExpr)

	typeName, ok := refIn(index.Index, imports, eventPath)
	if !ok || len(call.Args) != 1 {
		return registration{}, false
	}

	constName, ok := refIn(call.Args[0], imports, eventPath)
	if !ok {
		return registration{typeName: typeName}, false
	}

	name, ok := constants[constName]

	return registration{typeName: typeName, name: name}, ok
}

// registrations returns the events files register with the desktop runtime. A call it
// cannot read as readRegistration describes is returned as a problem.
func registrations(
	fset *token.FileSet, files []*ast.File, eventPath string, constants map[string]string,
) ([]registration, []string) {
	var (
		found    []registration
		problems []string
	)

	for _, file := range files {
		imports := importNames(file)

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !isRegisterEvent(call.Fun) {
				return true
			}

			r, ok := readRegistration(call, imports, eventPath, constants)
			if !ok {
				problems = append(problems, fset.Position(call.Pos()).String())
			}

			found = append(found, r)

			return true
		})
	}

	return found, problems
}

// TestEveryWindowEventIsRegistered checks that the desktop bootstrap registers each window
// event, and nothing else, with its own type under its own name. The runtime drops an
// event whose data is not the type its name was registered with, and the generated
// bindings type only the registered events. The window events are read from the event
// package's source: every type with the window marker method.
func TestEveryWindowEventIsRegistered(t *testing.T) {
	t.Parallel()

	eventPath := modulePath(t, moduleRoot(t)) + "/" + eventDir

	_, eventFiles := dirFiles(t, eventDir)
	constants := stringConstants(eventFiles)

	want, problems := windowEvents(eventFiles, constants)
	require.Empty(t, problems)
	require.NotEmpty(t, want, "%s declares no window events; update this guard", eventDir)

	fset, cmdFiles := dirFiles(t, "cmd/"+brand.Slug)

	got, problems := registrations(fset, cmdFiles, eventPath, constants)
	assert.Empty(t, problems, "register each event as RegisterEvent[event.Type](event.Name), "+
		"both from the event package, so this check can read it")
	assert.ElementsMatch(t, want, got, "register every window event, and only those, in the desktop bootstrap")
}

// TestWindowEventsListsEveryWindowEvent checks that event.WindowEvents returns one of
// each type with the window marker method, which the tests that use it rely on.
func TestWindowEventsListsEveryWindowEvent(t *testing.T) {
	t.Parallel()

	_, eventFiles := dirFiles(t, eventDir)

	want, problems := windowEvents(eventFiles, stringConstants(eventFiles))
	require.Empty(t, problems)

	got := make([]registration, 0, len(event.WindowEvents()))
	for _, e := range event.WindowEvents() {
		got = append(got, registration{typeName: reflect.TypeOf(e).Name(), name: e.Name()})
	}

	assert.ElementsMatch(t, want, got, "list every window event in event.WindowEvents")
}

func TestWindowEvents(t *testing.T) {
	t.Parallel()

	file, err := parser.ParseFile(token.NewFileSet(), "event.go", `package event

const (
	NameThing = "thing:changed"
	NameOther = "other:changed"
)

type Thing struct{}
func (Thing) Name() string { return NameThing }
func (Thing) window()      {}

type Other struct{}
func (*Other) Name() string { return NameOther }
func (*Other) window()      {}

type Quiet struct{}
func (Quiet) Name() string { return NameOther }

type Odd struct{}
func (Odd) Name() string { return "odd" }
func (Odd) window()      {}

func window() {}
`, 0)
	require.NoError(t, err)

	files := []*ast.File{file}

	got, problems := windowEvents(files, stringConstants(files))
	assert.ElementsMatch(t, []registration{
		{typeName: "Thing", name: "thing:changed"}, {typeName: "Other", name: "other:changed"}, {typeName: "Odd"},
	}, got, "every marked type, pointer receivers too, and nothing unmarked")
	assert.Equal(t, []string{"Odd: Name must return one of the event name constants"}, problems)
}

func TestReadRegistration(t *testing.T) {
	t.Parallel()

	const eventPath = "example.com/m/internal/event"

	file, err := parser.ParseFile(token.NewFileSet(), "main.go", `package main

import (
	"example.com/m/internal/event"
	"example.com/m/internal/inbox"
	ev "example.com/m/internal/event"
	settings "example.com/m/internal/settings"
	_ "example.com/m/internal/blank"
)
`, parser.ImportsOnly)
	require.NoError(t, err)

	imports := importNames(file)
	assert.Equal(t, map[string]string{
		"event": eventPath, "inbox": "example.com/m/internal/inbox", "ev": eventPath,
		"settings": "example.com/m/internal/settings",
	}, imports)

	constants := map[string]string{"NameThing": "thing:changed"}

	for src, want := range map[string]struct {
		r  registration
		ok bool
	}{
		"application.RegisterEvent[event.Thing](event.NameThing)":    {registration{"Thing", "thing:changed"}, true},
		"application.RegisterEvent[ev.Thing](ev.NameThing)":          {registration{"Thing", "thing:changed"}, true},
		"application.RegisterEvent[inbox.Thing](event.NameThing)":    {registration{}, false},
		"application.RegisterEvent[event.Thing](settings.NameThing)": {registration{typeName: "Thing"}, false},
		`application.RegisterEvent[event.Thing]("thing:changed")`:    {registration{typeName: "Thing"}, false},
		"application.RegisterEvent[event.Thing](event.Unknown)":      {registration{typeName: "Thing"}, false},
		"application.RegisterEvent[Thing](event.NameThing)":          {registration{}, false},
		"application.RegisterEvent[event.Thing](a, b)":               {registration{}, false},
	} {
		expr, err := parser.ParseExpr(src)
		require.NoError(t, err, src)

		call, ok := expr.(*ast.CallExpr)
		require.True(t, ok, src)
		require.True(t, isRegisterEvent(call.Fun), src)

		r, ok := readRegistration(call, imports, eventPath, constants)
		assert.Equal(t, want.r, r, src)
		assert.Equal(t, want.ok, ok, src)
	}

	for _, src := range []string{"application.NewService(svc)", "RegisterEvent(name)"} {
		expr, err := parser.ParseExpr(src)
		require.NoError(t, err)

		call, ok := expr.(*ast.CallExpr)
		require.True(t, ok)
		assert.False(t, isRegisterEvent(call.Fun), src)
	}
}
