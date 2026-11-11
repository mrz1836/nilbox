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

// The app changes GitHub only when the user asks, and every change goes through the
// client's WriteGate, which paces the edits and keeps the user's budget clear of them. So,
// inside internal/github:
//
//   - a request with any method but GET is sent through (*Client).do only by edit, the
//     gated REST edit, and by sendGraphQL, which posts GraphQL;
//   - sendGraphQL is called only by postGraphQL, which refuses a mutation, and by mutate,
//     the gated GraphQL edit.

// writeSenders are the functions allowed to send a request other than a GET through do,
// by "Receiver.Name".
func writeSenders() []string {
	return []string{"*Client.edit", "sendGraphQL"}
}

// graphQLSenders are the functions allowed to call sendGraphQL.
func graphQLSenders() []string {
	return []string{"postGraphQL", "mutate"}
}

// strayWrites returns "file:line: what" for each way src, the Go file rel of
// internal/github, could change GitHub around the WriteGate.
func strayWrites(rel, src string) ([]string, error) {
	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, rel, src, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", rel, err)
	}

	var found []string

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}

		name := funcName(fn)

		ast.Inspect(fn, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}

			if what := strayWrite(call, name); what != "" {
				found = append(found, fmt.Sprintf("%s:%d: %s", rel, fset.Position(call.Pos()).Line, what))
			}

			return true
		})
	}

	return found, nil
}

// strayWrite says how call, made in the function named caller, could change GitHub around
// the WriteGate, or "" when it can't.
func strayWrite(call *ast.CallExpr, caller string) string {
	switch callee := calleeName(call); {
	case callee == "do" && len(call.Args) >= 3 && !isMethodGet(call.Args[2]) && !slices.Contains(writeSenders(), caller):
		return "sends a request other than a GET outside edit and sendGraphQL"
	case callee == "sendGraphQL" && !slices.Contains(graphQLSenders(), caller):
		return "calls sendGraphQL outside postGraphQL and mutate"
	default:
		return ""
	}
}

// calleeName is the name a call calls: the method's name for c.do(...), the function's
// for f(...) or f[T](...), and "" for anything else.
func calleeName(call *ast.CallExpr) string {
	switch f := withoutTypeArgs(call.Fun).(type) {
	case *ast.SelectorExpr:
		return f.Sel.Name
	case *ast.Ident:
		return f.Name
	default:
		return ""
	}
}

// isMethodGet reports whether arg is http.MethodGet or the literal "GET".
func isMethodGet(arg ast.Expr) bool {
	if s, ok := stringLiteral(arg); ok {
		return s == "GET"
	}

	sel, ok := arg.(*ast.SelectorExpr)

	return ok && sel.Sel.Name == "MethodGet"
}

func TestEveryGitHubWriteGoesThroughTheGate(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)

	var found []string

	for _, rel := range goFiles(t, root) {
		if !strings.HasPrefix(rel, githubPackage) || !requestScope(rel) {
			continue
		}

		src, ok := readText(t, root, rel)
		if !ok {
			continue
		}

		stray, err := strayWrites(rel, src)
		require.NoError(t, err)

		found = append(found, stray...)
	}

	assert.Empty(t, found,
		"send every change to GitHub through edit (REST) or mutate (GraphQL), which the WriteGate paces and counts")
}

func TestStrayWrites(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "reads and gated writes",
			src: `package github
type Client struct{}
func (c *Client) read() { c.do(nil, tok, http.MethodGet, "/x", nil); c.do(nil, tok, "GET", "/y", nil) }
func (c *Client) edit() { c.do(nil, tok, method, path, body) }
func sendGraphQL[T any]() { c.do(nil, tok, http.MethodPost, graphQLPath, body) }
func postGraphQL[T any]() { sendGraphQL[T]() }
func mutate[T any]() { sendGraphQL[T]() }`,
		},
		{
			name: "an ungated write",
			src: `package github
type Client struct{}
func (c *Client) merge() { c.do(nil, tok, http.MethodPut, "/merge", nil) }`,
			want: []string{"internal/github/x.go:3: sends a request other than a GET outside edit and sendGraphQL"},
		},
		{
			name: "a mutation around mutate",
			src: `package github
func approve() { sendGraphQL[data](nil, nil, tok, "approve", req, anyError, cost) }`,
			want: []string{"internal/github/x.go:2: calls sendGraphQL outside postGraphQL and mutate"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := strayWrites("internal/github/x.go", tt.src)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
