package repoguard_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The app reads the user's GitHub notifications and clears them one thread at a time, so
// the GitHub client calls three notification endpoints and no other: GET /notifications,
// which lists the web inbox, and DELETE and PATCH on /notifications/threads/{id}, which
// mark one thread done or read. It can never mark every notification read (PUT
// /notifications), mark a repository's read (PUT /repos/{owner}/{name}/notifications), or
// change a subscription (/notifications/threads/{id}/subscription). So, inside
// internal/github:
//
//   - a string that names notifications is one of the two paths, each held by its
//     constant (notificationPaths), unless it is an error's message or a struct tag;
//   - the listing's constant only starts the path of a GET sent through do, followed by
//     "?" and its query, or is compared with a path;
//   - the threads' constant is used only in threadPath, as the whole of threadsPath +
//     strconv.FormatInt(id, 10);
//   - threadPath is called only as the whole path of chore's request, and every call of
//     chore sends DELETE or PATCH, which chore checks again as it runs.

const (
	// listingPath and threadsPrefix name the constants holding the endpoints' paths.
	listingPath   = "notificationsPath"
	threadsPrefix = "threadsPath"

	// threadPathFunc builds a thread's path, choreFunc sends a write on one, and
	// choreSender is the one function that sends it.
	threadPathFunc = "threadPath"
	choreFunc      = "chore"
	choreSender    = "*Client.chore"
)

// notificationPaths are the constants holding the notification endpoints' paths, by name.
func notificationPaths() map[string]string {
	return map[string]string{listingPath: "/notifications", threadsPrefix: "/notifications/threads/"}
}

// choreMethods are the methods a chore may send, as their net/http constants and as text.
func choreMethods() []string {
	return []string{"MethodDelete", "MethodPatch", "DELETE", "PATCH"}
}

// messageFuncs are the functions, by "package.Name", whose arguments are an error's
// message, never a request's path.
func messageFuncs() []string {
	return []string{"fmt.Errorf", "errors.New", "uierr.New"}
}

// strayNotificationCalls returns "file:line: what" for each way src, the Go file rel of
// internal/github, could call a notification endpoint the app never calls.
func strayNotificationCalls(rel, src string) ([]string, error) {
	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, rel, src, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", rel, err)
	}

	return strayNotificationCallsIn(fset, rel, file), nil
}

// strayNotificationCallsIn is strayNotificationCalls for file, the parsed Go file rel.
func strayNotificationCallsIn(fset *token.FileSet, rel string, file *ast.File) []string {
	parents := parentsOf(file)
	allowed := allowedNotificationUses(file)

	var found []string

	ast.Inspect(file, func(n ast.Node) bool {
		var what string

		switch n := n.(type) {
		case *ast.BasicLit:
			what = strayNotificationLiteral(n, parents)
		case *ast.Ident:
			what = strayNotificationName(n, parents, allowed)
		case *ast.CallExpr:
			what = strayChoreCall(n)
		}

		if what != "" {
			found = append(found, fmt.Sprintf("%s:%d: %s", rel, fset.Position(n.Pos()).Line, what))
		}

		return true
	})

	return found
}

// parentsOf maps every node under root to the node it is in.
func parentsOf(root ast.Node) map[ast.Node]ast.Node {
	parents := map[ast.Node]ast.Node{}

	var stack []ast.Node

	ast.Inspect(root, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]

			return true
		}

		if len(stack) > 0 {
			parents[n] = stack[len(stack)-1]
		}

		stack = append(stack, n)

		return true
	})

	return parents
}

// allowedNotificationUses returns the identifiers in file that use the endpoints' names
// in their one place each: the listing's constant starting a GET's path, the threads'
// constant in threadPath, and threadPath as chore's path.
func allowedNotificationUses(file *ast.File) map[*ast.Ident]bool {
	allowed := map[*ast.Ident]bool{}

	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
			allowUsesIn(fn, allowed)
		}
	}

	return allowed
}

// allowUsesIn adds to allowed the identifiers in fn that use the endpoints' names in their
// one place.
func allowUsesIn(fn *ast.FuncDecl, allowed map[*ast.Ident]bool) {
	name := funcName(fn)

	if id := threadPathBody(fn); id != nil && name == threadPathFunc {
		allowed[id] = true
	}

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if call, isCall := n.(*ast.CallExpr); isCall && calleeName(call) == "do" && len(call.Args) >= 4 {
			allowPathOf(call, name, allowed)
		}

		return true
	})
}

// allowPathOf adds to allowed the endpoint's name call, a call of do in the function
// named sender, starts its path with in its one place: the listing's constant on a GET,
// or threadPath in chore.
func allowPathOf(call *ast.CallExpr, sender string, allowed map[*ast.Ident]bool) {
	if id := listingStart(call.Args[3]); id != nil && isMethodGet(call.Args[2]) {
		allowed[id] = true
	}

	if id := threadPathCall(call.Args[3]); id != nil && sender == choreSender {
		allowed[id] = true
	}
}

// threadPathBody returns the threads' constant in fn when fn's body is exactly return
// threadsPath + strconv.FormatInt(id, 10).
func threadPathBody(fn *ast.FuncDecl) *ast.Ident {
	sum, ok := onlyReturned(fn).(*ast.BinaryExpr)
	if !ok || sum.Op != token.ADD || !isDecimalFormatInt(sum.Y) {
		return nil
	}

	id, ok := sum.X.(*ast.Ident)
	if !ok || id.Name != threadsPrefix {
		return nil
	}

	return id
}

// onlyReturned is what fn returns when its body is one return of one value; nil otherwise.
func onlyReturned(fn *ast.FuncDecl) ast.Expr {
	if fn.Body == nil || len(fn.Body.List) != 1 {
		return nil
	}

	ret, ok := fn.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return nil
	}

	return ret.Results[0]
}

// isDecimalFormatInt reports whether expr is strconv.FormatInt(x, 10).
func isDecimalFormatInt(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) != 2 {
		return false
	}

	pkg, fun, isPkg := packageSelector(call.Fun)
	base, isLit := call.Args[1].(*ast.BasicLit)

	return isPkg && pkg == "strconv" && fun == "FormatInt" && isLit && base.Value == "10"
}

// listingStart returns the listing's constant when path is notificationsPath + "?" + a
// query.
func listingStart(path ast.Expr) *ast.Ident {
	outer, ok := path.(*ast.BinaryExpr)
	if !ok || outer.Op != token.ADD {
		return nil
	}

	inner, ok := outer.X.(*ast.BinaryExpr)
	if !ok || inner.Op != token.ADD {
		return nil
	}

	if s, isLit := stringLiteral(inner.Y); !isLit || s != "?" {
		return nil
	}

	id, ok := inner.X.(*ast.Ident)
	if !ok || id.Name != listingPath {
		return nil
	}

	return id
}

// threadPathCall returns threadPath's name when path is a call of it and nothing more.
func threadPathCall(path ast.Expr) *ast.Ident {
	call, ok := path.(*ast.CallExpr)
	if !ok {
		return nil
	}

	id, ok := call.Fun.(*ast.Ident)
	if !ok || id.Name != threadPathFunc {
		return nil
	}

	return id
}

// strayNotificationLiteral says how lit, a literal in a file of internal/github, names a
// notification endpoint other than as one of the paths' constants, or "" when it doesn't.
func strayNotificationLiteral(lit *ast.BasicLit, parents map[ast.Node]ast.Node) string {
	s, ok := stringLiteral(lit)
	if !ok || !strings.Contains(strings.ToLower(s), "notification") || literalAllowed(lit, s, parents) {
		return ""
	}

	return fmt.Sprintf("names a notification endpoint (%q) outside the paths' constants", s)
}

// literalAllowed reports whether lit, whose value is s, is a struct tag, an error's
// message, or the value of the paths' constant that holds s.
func literalAllowed(lit *ast.BasicLit, s string, parents map[ast.Node]ast.Node) bool {
	switch parent := parents[lit].(type) {
	case *ast.Field:
		return parent.Tag == lit
	case *ast.CallExpr:
		pkg, name, isPkg := packageSelector(parent.Fun)

		return isPkg && slices.Contains(messageFuncs(), pkg+"."+name)
	case *ast.ValueSpec:
		return constSpec(parent, parents) && len(parent.Names) == 1 && len(parent.Values) == 1 &&
			notificationPaths()[parent.Names[0].Name] == s
	default:
		return false
	}
}

// constSpec reports whether spec declares constants.
func constSpec(spec *ast.ValueSpec, parents map[ast.Node]ast.Node) bool {
	decl, ok := parents[spec].(*ast.GenDecl)

	return ok && decl.Tok == token.CONST
}

// strayNotificationName says how id, an identifier in a file of internal/github, uses
// one of the endpoints' names outside its one place, or "" when it doesn't.
func strayNotificationName(id *ast.Ident, parents map[ast.Node]ast.Node, allowed map[*ast.Ident]bool) string {
	if allowed[id] || declares(id, parents) {
		return ""
	}

	switch id.Name {
	case listingPath:
		if compared(id, parents) {
			return ""
		}

		return "uses " + listingPath + " other than to start the path of a GET"
	case threadsPrefix:
		return "uses " + threadsPrefix + " outside " + threadPathFunc
	case threadPathFunc:
		return "uses " + threadPathFunc + " other than as the whole path of " + choreSender + "'s request"
	case choreFunc:
		if calledAs(id, parents) {
			return ""
		}

		return "uses " + choreFunc + " other than by calling it"
	default:
		return ""
	}
}

// declares reports whether id is the name a constant, variable, or function declaration
// declares.
func declares(id *ast.Ident, parents map[ast.Node]ast.Node) bool {
	switch parent := parents[id].(type) {
	case *ast.ValueSpec:
		return slices.Contains(parent.Names, id)
	case *ast.FuncDecl:
		return parent.Name == id
	default:
		return false
	}
}

// compared reports whether id is one side of an == or != comparison, which builds no
// request's path.
func compared(id *ast.Ident, parents map[ast.Node]ast.Node) bool {
	cmp, ok := parents[id].(*ast.BinaryExpr)

	return ok && (cmp.Op == token.EQL || cmp.Op == token.NEQ)
}

// calledAs reports whether id is what a call calls, as f(...) or c.f(...).
func calledAs(id *ast.Ident, parents map[ast.Node]ast.Node) bool {
	var fun ast.Node = id

	if sel, ok := parents[id].(*ast.SelectorExpr); ok && sel.Sel == id {
		fun = sel
	}

	call, ok := parents[fun].(*ast.CallExpr)

	return ok && call.Fun == fun
}

// strayChoreCall says how call, a call of chore, sends a method a chore may not, or ""
// when it calls something else or sends DELETE or PATCH.
func strayChoreCall(call *ast.CallExpr) string {
	if calleeName(call) != choreFunc {
		return ""
	}

	if len(call.Args) >= 3 && isChoreMethod(call.Args[2]) {
		return ""
	}

	return "sends a chore other than DELETE or PATCH, or names no method"
}

// isChoreMethod reports whether arg is http.MethodDelete or http.MethodPatch, or their
// text.
func isChoreMethod(arg ast.Expr) bool {
	if s, ok := stringLiteral(arg); ok {
		return slices.Contains(choreMethods(), s)
	}

	pkg, name, ok := packageSelector(arg)

	return ok && pkg == "http" && slices.Contains(choreMethods(), name)
}

func TestTheClientCallsOnlyItsNotificationEndpoints(t *testing.T) {
	t.Parallel()

	var found []string

	for _, rel := range goFiles(t) {
		if !strings.HasPrefix(rel, githubPackage) || !requestScope(rel) {
			continue
		}

		if fset, file, ok := goSyntax(t, rel); ok {
			found = append(found, strayNotificationCallsIn(fset, rel, file)...)
		}
	}

	assert.Empty(t, found, "the client lists notifications with GET /notifications, and marks one thread done or read "+
		"with DELETE or PATCH on /notifications/threads/{id} through chore; it calls no other notification endpoint")
}

func TestStrayNotificationCalls(t *testing.T) {
	t.Parallel()

	// client is the client's own code, which the guard allows.
	const client = `package github
import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
)
const (
	notificationsPath = "/notifications"
	threadsPath       = "/notifications/threads/"
)
var ErrThreadNotFound = errors.New("GitHub found no such notification thread")
type restThread struct {
	URL string ` + "`json:\"notifications_url\"`" + `
}
func (c *Client) list(page int) error {
	_, _, err := c.do(ctx, tok, http.MethodGet, notificationsPath+"?"+query(page), nil, opts...)
	return fmt.Errorf("list the notifications: %w", err)
}
func next(path string) bool { return path != notificationsPath }
func threadPath(id int64) string { return threadsPath + strconv.FormatInt(id, 10) }
func (c *Client) chore(method string, id int64) error {
	_, _, err := c.do(ctx, tok, method, threadPath(id), nil)
	return err
}
func (c *Client) MarkThreadDone(id int64) error { return c.chore(ctx, tok, http.MethodDelete, id) }
func (c *Client) MarkThreadRead(id int64) error { return c.chore(ctx, tok, http.MethodPatch, id) }
`

	tests := []struct {
		name string
		src  string
		want []string
	}{
		{name: "the client's own", src: client},
		{
			name: "mark every notification read",
			src: `package github
func (c *Client) markAll() { c.do(ctx, tok, http.MethodPut, notificationsPath, nil) }`,
			want: []string{"internal/github/x.go:2: uses notificationsPath other than to start the path of a GET"},
		},
		{
			name: "list with another method",
			src: `package github
func (c *Client) markAll() { c.do(ctx, tok, http.MethodPut, notificationsPath+"?"+q, nil) }`,
			want: []string{"internal/github/x.go:2: uses notificationsPath other than to start the path of a GET"},
		},
		{
			name: "mark a repository's notifications read",
			src: `package github
func (c *Client) markRepo(o, r string) { c.do(ctx, tok, http.MethodPut, "/repos/"+o+"/"+r+"/notifications", nil) }`,
			want: []string{`internal/github/x.go:2: names a notification endpoint ("/notifications") outside the paths' constants`},
		},
		{
			name: "a repository's notifications from the constant",
			src: `package github
func (c *Client) markRepo(o, r string) { c.do(ctx, tok, http.MethodGet, "/repos/"+o+"/"+r+notificationsPath+"?"+q, nil) }`,
			want: []string{"internal/github/x.go:2: uses notificationsPath other than to start the path of a GET"},
		},
		{
			name: "change a subscription",
			src: `package github
func (c *Client) chore(method string, id int64) { c.do(ctx, tok, method, threadPath(id)+"/subscription", nil) }`,
			want: []string{"internal/github/x.go:2: uses threadPath other than as the whole path of *Client.chore's request"},
		},
		{
			name: "a thread's path built elsewhere",
			src: `package github
func subscription(id string) string { return threadsPath + id + "/subscription" }`,
			want: []string{"internal/github/x.go:2: uses threadsPath outside threadPath"},
		},
		{
			name: "threadPath outside chore",
			src: `package github
func (c *Client) read(id int64) { c.do(ctx, tok, http.MethodGet, threadPath(id), nil) }`,
			want: []string{"internal/github/x.go:2: uses threadPath other than as the whole path of *Client.chore's request"},
		},
		{
			name: "a chore that subscribes",
			src: `package github
func (c *Client) subscribe(id int64) error { return c.chore(ctx, tok, http.MethodPut, id) }`,
			want: []string{"internal/github/x.go:2: sends a chore other than DELETE or PATCH, or names no method"},
		},
		{
			name: "a chore whose method is passed in",
			src: `package github
func (c *Client) mark(method string, id int64) error { return c.chore(ctx, tok, method, id) }`,
			want: []string{"internal/github/x.go:2: sends a chore other than DELETE or PATCH, or names no method"},
		},
		{
			name: "chore handed on",
			src: `package github
func (c *Client) sender() func() { return c.chore }`,
			want: []string{"internal/github/x.go:2: uses chore other than by calling it"},
		},
		{
			name: "a path written out",
			src: `package github
func path(id int64) string { return fmt.Sprintf("/notifications/threads/%d/subscription", id) }`,
			want: []string{
				`internal/github/x.go:2: names a notification endpoint ("/notifications/threads/%d/subscription") ` +
					"outside the paths' constants",
			},
		},
		{
			name: "a constant with another path",
			src: `package github
const threadsPath = "/notifications/threads/subscription/"`,
			want: []string{
				`internal/github/x.go:2: names a notification endpoint ("/notifications/threads/subscription/") ` +
					"outside the paths' constants",
			},
		},
		{
			name: "a GraphQL mutation",
			src: `package github
const markDone = "mutation { markNotificationAsDone(input: {id: $id}) { success } }"`,
			want: []string{
				`internal/github/x.go:2: names a notification endpoint ("mutation { markNotificationAsDone(input: ` +
					`{id: $id}) { success } }") outside the paths' constants`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := strayNotificationCalls("internal/github/x.go", tt.src)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
