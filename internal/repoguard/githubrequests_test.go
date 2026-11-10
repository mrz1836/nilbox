package repoguard_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every GitHub request the app makes is counted against the user's API budget: the GitHub
// client hands each response's rate-limit reading, and what the request spent, to the
// budget tracker in one place, (*Client).do, which every API call goes through. A request
// sent any other way would spend the user's budget without the budget knowing. So:
//
//   - only internal/github speaks HTTP;
//   - inside it, a request is built only in (*Client).do, in (*Client).FetchAvatar,
//     which downloads the profile picture from GitHub's avatar host, and in
//     (*Client).DownloadAsset, which downloads a public release file from GitHub's
//     download hosts; neither is the API, sends a token, or costs budget;
//   - nothing runs the GitHub CLI's api command, which would spend the same budget behind
//     the app's back. (gh auth token and gh repo clone stay allowed: the first spends
//     nothing, and the clone is git's.)

// githubPackage is the one package allowed to speak HTTP.
const githubPackage = "internal/github/"

// requestBuilders are the functions in internal/github allowed to build a request, by
// "Receiver.Name".
func requestBuilders() []string {
	return []string{"*Client.do", "*Client.FetchAvatar", "*Client.DownloadAsset"}
}

// httpSenders are the net/http functions that build or send a request.
func httpSenders() []string {
	return []string{"NewRequest", "NewRequestWithContext", "Get", "Head", "Post", "PostForm", "DefaultClient"}
}

// requestScope reports whether the Go file rel is production code the guard covers.
func requestScope(rel string) bool {
	return loggingScope(rel)
}

// funcName names a function declaration as "Receiver.Name", "*Receiver.Name", or "Name".
func funcName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}

	recv := fn.Recv.List[0].Type
	star := ""

	if s, ok := recv.(*ast.StarExpr); ok {
		recv, star = s.X, "*"
	}

	if g, ok := recv.(*ast.IndexExpr); ok {
		recv = g.X
	}

	if id, ok := recv.(*ast.Ident); ok {
		return star + id.Name + "." + fn.Name.Name
	}

	return fn.Name.Name
}

// httpName is the name net/http is imported under in file, "" when it is not.
func httpName(file *ast.File) string {
	for _, imp := range file.Imports {
		if p, err := strconv.Unquote(imp.Path.Value); err == nil && p == "net/http" {
			if imp.Name != nil {
				return imp.Name.Name
			}

			return "http"
		}
	}

	return ""
}

// strayRequests returns "file:line: what" for each way src, the Go file rel, could send a
// GitHub request around the budget: importing net/http outside internal/github, building
// or sending a request outside requestBuilders, or running gh's api command.
func strayRequests(rel, src string) ([]string, error) {
	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, rel, src, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", rel, err)
	}

	var found []string

	where := func(n ast.Node, what string) {
		found = append(found, fmt.Sprintf("%s:%d: %s", rel, fset.Position(n.Pos()).Line, what))
	}

	httpPkg := httpName(file)
	if httpPkg != "" && !strings.HasPrefix(rel, githubPackage) {
		where(file.Name, "imports net/http outside internal/github")
	}

	for _, decl := range file.Decls {
		allowed := strings.HasPrefix(rel, githubPackage) && buildsRequests(decl)

		ast.Inspect(decl, func(n ast.Node) bool {
			if what := strayNode(n, httpPkg, allowed); what != "" {
				where(n, what)
			}

			return true
		})
	}

	return found, nil
}

// buildsRequests reports whether decl is one of requestBuilders.
func buildsRequests(decl ast.Decl) bool {
	fn, ok := decl.(*ast.FuncDecl)

	return ok && slices.Contains(requestBuilders(), funcName(fn))
}

// strayNode says how n could send a request around the budget, or "" when it can't: a
// net/http sender used where requests are not allowed, or gh's api command.
func strayNode(n ast.Node, httpPkg string, allowed bool) string {
	if pkg, name, ok := packageSelector(n); ok && httpPkg != "" && pkg == httpPkg &&
		slices.Contains(httpSenders(), name) && !allowed {
		return "http." + name + " outside the GitHub client's request functions"
	}

	if call, ok := n.(*ast.CallExpr); ok && runsGHAPI(call) {
		return "runs the GitHub CLI's api command"
	}

	return ""
}

// runsGHAPI reports whether call passes the literal argument "api", as running gh api
// does.
func runsGHAPI(call *ast.CallExpr) bool {
	for _, arg := range call.Args {
		if s, ok := stringLiteral(arg); ok && s == "api" {
			return true
		}
	}

	return false
}

func TestEveryGitHubRequestCountsAgainstTheBudget(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)

	var found []string

	for _, rel := range goFiles(t, root) {
		if !requestScope(rel) {
			continue
		}

		src, ok := readText(t, root, rel)
		if !ok {
			continue
		}

		stray, err := strayRequests(rel, src)
		require.NoError(t, err)

		found = append(found, stray...)
	}

	assert.Empty(t, found,
		"send every GitHub request through the GitHub client's (*Client).do, which tells the budget tracker "+
			"what each response says is left and what the request spent")
}

func TestStrayRequests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		rel  string
		src  string
		want []string
	}{
		{
			name: "the client's request function",
			rel:  "internal/github/client.go",
			src: `package github
import "net/http"
type Client struct{}
func (c *Client) do() { _, _ = http.NewRequestWithContext(nil, "GET", "", nil) }`,
		},
		{
			name: "another function in the client",
			rel:  "internal/github/files.go",
			src: `package github
import "net/http"
func fetch(url string) { _, _ = http.Get(url) }`,
			want: []string{"internal/github/files.go:3: http.Get outside the GitHub client's request functions"},
		},
		{
			name: "the default client, renamed",
			rel:  "internal/github/x.go",
			src: `package github
import web "net/http"
func f() { _ = web.DefaultClient }`,
			want: []string{"internal/github/x.go:3: http.DefaultClient outside the GitHub client's request functions"},
		},
		{
			name: "HTTP outside the client",
			rel:  "internal/inbox/x.go",
			src: `package inbox
import "net/http"
var _ = http.MethodGet`,
			want: []string{"internal/inbox/x.go:1: imports net/http outside internal/github"},
		},
		{
			name: "gh api",
			rel:  "internal/launcher/x.go",
			src: `package launcher
func f(r interface{ Run(...string) }) { r.Run("gh", "api", "user") }`,
			want: []string{"internal/launcher/x.go:2: runs the GitHub CLI's api command"},
		},
		{
			name: "gh's other commands",
			rel:  "internal/launcher/x.go",
			src: `package launcher
func f(r interface{ Run(...string) }) { r.Run("gh", "auth", "token"); r.Run("gh", "repo", "clone") }`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := strayRequests(tt.rel, tt.src)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
