package scripts

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/integrations"
	"github.com/mrz1836/nilbox/internal/textutil"
)

const (
	// helpTimeout bounds magex's help:commands.
	helpTimeout = 15 * time.Second

	// maxMagefiles and maxMagefileBytes bound what the app hashes under magefiles/.
	maxMagefiles     = 4096
	maxMagefileBytes = 64 << 20
)

var (
	// errNoTargets is returned for a magex whose help:commands listed nothing.
	errNoTargets = errors.New("magex listed no targets")

	// errTooMany is returned for magefiles too many or too large to sum.
	errTooMany = errors.New("its magefiles are too large to check")
)

// commonTargets are the magex targets the page offers as chips, in its order.
func commonTargets() []string {
	return []string{"format:fix", "lint", "lint:fix", "test", "test:race", "build", "deps:tidy", "deps:update"}
}

// wrapperNamespaces are the namespaces magex checks a function's name against, to leave
// out a function that wraps one of its own targets, as BuildDefault wraps build:default.
func wrapperNamespaces() []string {
	return []string{
		"build", "test", "lint", "format", "deps", "git", "release", "docs", "tools", "generate", "mod", "help",
		"version", "install", "configure", "init", "bench", "vet", "aws",
	}
}

// ToolFacts is what Repo scripts knows of magex: the copy in use, and whether it can run.
type ToolFacts struct {
	// Path is the copy in use, absolute, or "" when there is none; Version is its version
	// as printed, without a leading v.
	Path    string
	Version string

	// State is what the settings say of it, and Off reports the user turned mage-x off.
	State integrations.State
	Off   bool
}

// ToolFactsOf reads magex's facts from what the settings show of it.
func ToolFactsOf(tool integrations.Tool) ToolFacts {
	return ToolFacts{
		Path: tool.Copy.Path, Version: tool.Copy.Version.Raw, State: tool.State,
		Off: tool.Switch == integrationprefs.SwitchOff,
	}
}

// Usable reports a copy the page can run: on, found, and new enough.
func (t ToolFacts) Usable() bool {
	return !t.Off && t.Path != "" && t.State.Usable()
}

// key names the copy, for the built-ins it listed.
func (t ToolFacts) key() string {
	return t.Path + "@" + t.Version
}

// why says why magex can't run.
func (t ToolFacts) why() string {
	switch {
	case t.Off:
		return "mage-x is turned off in Settings → Integrations."
	case t.Path == "" || t.State == integrations.StateMissing:
		return "magex isn't installed. Install it in Settings → Integrations."
	case t.State == integrations.StateTooOld:
		return "This magex is too old for " + brand.Name + ". Update it in Settings → Integrations."
	default:
		return "magex won't run. Check it in Settings → Integrations."
	}
}

// builtin is one of magex's own targets, as help:commands json=true lists it.
type builtin struct {
	Name        string   `json:"name"`
	Namespace   string   `json:"namespace"`
	Description string   `json:"description"`
	Aliases     []string `json:"aliases"`
}

// builtins are magex's own targets: in its order, and by every name and alias, and its
// namespaces, lowercased, which a repository's target of the same name never reaches.
type builtins struct {
	list       []builtin
	names      map[string]bool
	namespaces map[string]bool
}

// parseBuiltins reads help:commands json=true's answer.
func parseBuiltins(out []byte) (builtins, error) {
	if i := bytes.IndexByte(out, '{'); i > 0 {
		out = out[i:]
	}

	var doc struct {
		Commands []builtin `json:"commands"`
	}

	if err := json.Unmarshal(out, &doc); err != nil {
		return builtins{}, fmt.Errorf("read magex's targets: %w", err)
	}

	b := builtins{names: map[string]bool{}, namespaces: map[string]bool{}}
	for _, c := range doc.Commands {
		b.add(c)
	}

	if len(b.list) == 0 {
		return builtins{}, errNoTargets
	}

	return b, nil
}

// add adds the built-in c, its name lowercased, unless a built-in of its name is there.
func (b *builtins) add(c builtin) {
	name := strings.ToLower(strings.TrimSpace(c.Name))
	if name == "" || b.names[name] {
		return
	}

	c.Name = name
	b.list = append(b.list, c)
	b.names[name] = true

	for _, a := range c.Aliases {
		b.names[strings.ToLower(a)] = true
	}

	if c.Namespace != "" {
		b.namespaces[strings.ToLower(c.Namespace)] = true
	}
}

// shadows reports a repository target magex never runs: a built-in or a namespace of the
// same name runs instead, or it wraps a built-in, as BuildDefault wraps build:default.
func (b *builtins) shadows(name string) bool {
	return b.names[name] || b.namespaces[name] || slices.ContainsFunc(wrapperNamespaces(), func(ns string) bool {
		method, ok := strings.CutPrefix(name, ns)

		return ok && method != "" && (b.names[ns+":"+method] || (method == "default" && b.names[ns]))
	})
}

// loadBuiltins runs magex's help:commands json=true in a scratch folder of the app's own,
// never in a clone, whose .github/env files magex would load over its environment.
func (s *Service) loadBuiltins(ctx context.Context, tool ToolFacts) (builtins, error) {
	dir, err := os.MkdirTemp(s.scratch, brand.Slug+"-magex-")
	if err != nil {
		return builtins{}, fmt.Errorf("make a folder to list magex's targets in: %w", err)
	}

	defer func() { _ = os.RemoveAll(dir) }()

	ctx, cancel := context.WithTimeout(ctx, helpTimeout)
	defer cancel()

	base, _ := s.env()
	env := integrations.MageX().RunEnv(base, integrationprefs.RunsInApp)

	out, _, err := s.runner(env, dir).Run(ctx, tool.Path, "help:commands", "json=true")
	if err != nil {
		return builtins{}, fmt.Errorf("list magex's targets: %w", err)
	}

	return parseBuiltins(out)
}

// builtinsOf returns the built-ins of the copy tool, listed once per copy.
func (s *Service) builtinsOf(ctx context.Context, tool ToolFacts) (builtins, error) {
	s.mu.Lock()
	got, ok := s.listed[tool.key()]
	s.mu.Unlock()

	if ok {
		return got, nil
	}

	return s.listing.Do(ctx, tool.key(), func(ctx context.Context) (builtins, error) {
		got, err := s.loadBuiltins(ctx, tool)
		if err != nil {
			return builtins{}, err
		}

		s.mu.Lock()
		s.listed[tool.key()] = got
		s.mu.Unlock()

		return got, nil
	})
}

// customTarget is one of the repository's own magex targets.
type customTarget struct {
	// name is what magex runs it as: function, or namespace:method, lowercased.
	name        string
	description string

	// params reports a target that takes arguments, which the page can't fill in.
	params bool
}

// fileSum is one file under magefiles/ as a custom target's trust holds it: its path, its
// kind (file, link, or other), where a link points, and its content's SHA-256.
type fileSum struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Link   string `json:"link"`
	SHA256 string `json:"sha256"`
}

// magefiles are a clone's magefiles as read: whether it has any, its targets, every file's
// sum, and why they couldn't be read or summed.
type magefiles struct {
	found   bool
	targets []customTarget
	files   []fileSum

	readErr error
	sumErr  error
}

// magexForm is a custom target's effective form, whose hash its trust is: the target, and
// every file under magefiles/.
type magexForm struct {
	Source Source    `json:"source"`
	Target string    `json:"target"`
	Files  []fileSum `json:"files"`
}

// readMagefiles reads the clone at dir's magefiles as magex discovers them: every .go file
// directly in magefiles/, tests aside, or else magefile.go, with each file under them
// summed, read from its bytes.
func readMagefiles(dir string) magefiles {
	root, single := magefilesRoot(dir)
	if root == "" {
		return magefiles{}
	}

	base, paths, goFiles := filepath.Dir(root), []string{root}, []string{root}

	if !single {
		var err error
		if paths, err = walkFiles(root); err != nil {
			return magefiles{found: true, readErr: err}
		}

		base, goFiles = root, targetFiles(root, paths)
	}

	mf := magefiles{found: true}
	mf.targets, mf.readErr = parseTargets(goFiles)
	mf.files, mf.sumErr = sumFiles(base, paths)

	return mf
}

// targetFiles returns the files of paths magex reads targets from: the .go files directly
// in root, tests aside.
func targetFiles(root string, paths []string) []string {
	var out []string

	for _, p := range paths {
		if filepath.Dir(p) == root && strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") {
			out = append(out, p)
		}
	}

	return out
}

// magefilesMessage words why a clone's magefiles couldn't be read or summed.
func magefilesMessage(err error) string {
	if errors.Is(err, errTooMany) {
		return textutil.EndSentence(textutil.Capitalize(err.Error()))
	}

	return textutil.WithReason("Couldn't read the repository's magefiles", err.Error())
}

// magefilesRoot returns the clone at dir's magefiles/ folder, resolved through links, or
// else its magefile.go and single; "" when it has neither.
func magefilesRoot(dir string) (root string, single bool) {
	folder := filepath.Join(dir, magefilesDir)
	if info, err := os.Stat(folder); err == nil && info.IsDir() {
		if resolved, err := filepath.EvalSymlinks(folder); err == nil {
			return resolved, false
		}

		return folder, false
	}

	file := filepath.Join(dir, magefileGo)
	if info, err := os.Stat(file); err == nil && !info.IsDir() {
		return file, true
	}

	return "", false
}

// walkFiles lists every entry under root that isn't a folder, in path order.
func walkFiles(root string) ([]string, error) {
	var out []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir():
			return nil
		case len(out) >= maxMagefiles:
			return fmt.Errorf("%w: more than %d files", errTooMany, maxMagefiles)
		}

		out = append(out, path)

		return nil
	})

	return out, err
}

// sumFiles sums each file of paths, named relative to root.
func sumFiles(root string, paths []string) ([]fileSum, error) {
	out := make([]fileSum, 0, len(paths))
	budget := int64(maxMagefileBytes)

	for _, p := range paths {
		sum, err := sumPath(root, p, &budget)
		if err != nil {
			return nil, err
		}

		out = append(out, sum)
	}

	return out, nil
}

// sumPath sums the file at p, named relative to root: a regular file by its bytes, a link
// by where it points and, when that is a regular file, its bytes, and anything else by its
// kind alone, never opened.
func sumPath(root, p string, budget *int64) (fileSum, error) {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return fileSum{}, err
	}

	sum := fileSum{Path: filepath.ToSlash(rel), Kind: "file"}

	info, err := os.Lstat(p)
	if err == nil && info.Mode()&fs.ModeSymlink != 0 {
		sum.Kind = "link"
		sum.Link, info, err = linkTarget(p)
	}

	switch {
	case err != nil:
		return fileSum{}, err
	case info == nil:
		return sum, nil // a link to nothing is summed by where it points
	case !info.Mode().IsRegular():
		if sum.Kind == "file" {
			sum.Kind = "other"
		}

		return sum, nil
	}

	sum.SHA256, err = sumFile(p, budget)

	return sum, err
}

// linkTarget reads where the link at p points, and what is there: nil for nothing.
func linkTarget(p string) (string, fs.FileInfo, error) {
	link, err := os.Readlink(p)
	if err != nil {
		return "", nil, err
	}

	info, err := os.Stat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return link, nil, nil
	}

	return link, info, err
}

// sumFile returns the SHA-256 of the file at path, taking what it read from budget, and
// fails once budget runs out.
func sumFile(path string, budget *int64) (string, error) {
	f, err := os.Open(path) //nolint:gosec // a file under the clone's magefiles
	if err != nil {
		return "", err
	}

	defer func() { _ = f.Close() }()

	h := sha256.New()

	n, err := io.Copy(h, io.LimitReader(f, *budget+1))
	if err != nil {
		return "", err
	}

	if *budget -= n; *budget < 0 {
		return "", fmt.Errorf("%w: more than %d MB", errTooMany, maxMagefileBytes>>20)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// parseTargets finds the targets mage would run in files, read together, since a namespace
// may be declared in one file and its methods in another: the exported functions with a
// target's signature, and such methods of the types declared as mg.Namespace, named as
// magex runs them.
func parseTargets(files []string) ([]customTarget, error) {
	parsed, err := parseFiles(files)
	if err != nil {
		return nil, err
	}

	var decls []ast.Decl
	for _, f := range parsed {
		decls = append(decls, f.Decls...)
	}

	namespaces := map[string]bool{}

	for _, decl := range decls {
		for _, ns := range namespacesOf(decl) {
			namespaces[ns] = true
		}
	}

	var out []customTarget

	for _, decl := range decls {
		if t, ok := targetOf(decl, namespaces); ok {
			out = append(out, t)
		}
	}

	slices.SortStableFunc(out, func(a, b customTarget) int { return strings.Compare(a.name, b.name) })

	return slices.CompactFunc(out, func(a, b customTarget) bool { return a.name == b.name }), nil
}

// parseFiles parses each of files; one that doesn't parse is skipped, as magex skips it.
func parseFiles(files []string) ([]*ast.File, error) {
	fset := token.NewFileSet()
	budget := maxMagefileBytes

	var parsed []*ast.File

	for _, path := range files {
		data, err := readLimited(path, filepath.Base(path))
		if err != nil {
			return nil, err
		}

		if budget -= len(data); budget < 0 {
			return nil, fmt.Errorf("%w: more than %d MB of Go", errTooMany, maxMagefileBytes>>20)
		}

		if f, err := parser.ParseFile(fset, path, data, parser.ParseComments); err == nil {
			parsed = append(parsed, f)
		}
	}

	return parsed, nil
}

// namespacesOf returns the types decl declares as mg.Namespace, which mage recognizes only
// spelled with the "mg" import name.
func namespacesOf(decl ast.Decl) []string {
	gen, ok := decl.(*ast.GenDecl)
	if !ok || gen.Tok != token.TYPE {
		return nil
	}

	var out []string

	for _, spec := range gen.Specs {
		if ts, ok := spec.(*ast.TypeSpec); ok && isSelector(ts.Type, "mg", "Namespace") {
			out = append(out, ts.Name.Name)
		}
	}

	return out
}

// targetOf returns the target decl declares, if it is one.
func targetOf(decl ast.Decl, namespaces map[string]bool) (customTarget, bool) {
	fn, ok := decl.(*ast.FuncDecl)
	if !ok || !fn.Name.IsExported() {
		return customTarget{}, false
	}

	params, ok := targetParams(fn.Type)
	if !ok {
		return customTarget{}, false
	}

	t := customTarget{name: strings.ToLower(fn.Name.Name), description: description(fn.Doc), params: params}

	if fn.Recv != nil {
		ns := receiverType(fn.Recv)
		if !namespaces[ns] {
			return customTarget{}, false // mage runs only the methods of mg.Namespace types
		}

		t.name = strings.ToLower(ns) + ":" + t.name
	}

	return t, true
}

// targetParams reports whether a function's signature is one mage runs as a target: an
// optional context.Context first, then only string, int, float64, bool, or time.Duration
// arguments, plain or pointers, returning nothing or an error; and whether it takes any
// such argument.
func targetParams(ft *ast.FuncType) (params, ok bool) {
	list := ft.Params.List
	if len(list) > 0 && isSelector(list[0].Type, "context", "Context") {
		if len(list[0].Names) > 1 {
			return false, false
		}

		list = list[1:]
	}

	for _, p := range list {
		if !isArgType(p.Type) {
			return false, false
		}
	}

	switch ft.Results.NumFields() {
	case 0:
		return len(list) > 0, true
	case 1:
		ident, isIdent := ft.Results.List[0].Type.(*ast.Ident)

		return len(list) > 0, isIdent && ident.Name == "error"
	default:
		return false, false
	}
}

// isArgType reports a type mage takes a target argument as.
func isArgType(expr ast.Expr) bool {
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}

	if ident, ok := expr.(*ast.Ident); ok {
		return slices.Contains([]string{"string", "int", "float64", "bool"}, ident.Name)
	}

	return isSelector(expr, "time", "Duration")
}

// isSelector reports whether expr is the qualified identifier pkg.name.
func isSelector(expr ast.Expr, pkg, name string) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	ident, ok := sel.X.(*ast.Ident)

	return ok && ident.Name == pkg && sel.Sel.Name == name
}

// receiverType returns the name of a method's receiver type.
func receiverType(recv *ast.FieldList) string {
	if len(recv.List) == 0 {
		return ""
	}

	switch t := recv.List[0].Type.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		if ident, ok := t.X.(*ast.Ident); ok {
			return ident.Name
		}
	}

	return ""
}

// description joins a target's doc comment's lines, as magex shows it.
func description(doc *ast.CommentGroup) string {
	if doc == nil {
		return ""
	}

	var lines []string

	for _, c := range doc.List {
		text := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(c.Text, "//"), "/*"), "*/")
		if text = strings.TrimSpace(text); text != "" {
			lines = append(lines, text)
		}
	}

	return strings.Join(lines, " ")
}

// magexScripts are magex's targets in the clone, as the page lists them: the repository's
// own, which a built-in of the same name doesn't shadow, then the built-ins, in magex's
// order.
func magexScripts(b builtins, mf magefiles) []script {
	out := make([]script, 0, len(mf.targets)+len(b.list))

	for _, t := range mf.targets {
		if b.shadows(t.name) {
			continue
		}

		sc := magexScript(t.name, t.description)
		sc.entry.Custom = true
		sc.hash = hashOf(magexForm{Source: SourceMagex, Target: t.name, Files: mf.files})

		switch {
		case mf.sumErr != nil:
			sc.entry.Runnable = false
			sc.entry.Why = magefilesMessage(mf.sumErr)
		case t.params:
			sc.entry.Runnable, sc.entry.Why = false, "Takes arguments, which "+brand.Name+" can't pass yet."
		}

		out = append(out, sc)
	}

	for _, c := range b.list {
		sc := magexScript(c.Name, c.Description)
		sc.entry.Args = true
		out = append(out, sc)
	}

	return out
}

// magexScript is the script that runs magex's target name.
func magexScript(name, detail string) script {
	command := "magex " + name

	return script{
		entry: Entry{
			Ref: ref(SourceMagex, name), Source: SourceMagex, Name: name, Command: command, Detail: detail,
			Runnable: true, Release: strings.Contains(command, releaseMarker), Inputs: []Input{},
		},
		name: name, parts: []part{{kind: partMagex, command: name}}, inputs: map[string]Input{},
	}
}

// fingerprint is what the clone at dir's magefiles look like on disk, from each entry's
// kind, size, and modification time, without reading a file: the page keeps the last read
// of them until it changes. A run reads them afresh.
func fingerprint(dir string) string {
	root, single := magefilesRoot(dir)
	if root == "" {
		return ""
	}

	var b strings.Builder

	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // what can't be read is left out; a run reads it again
		}

		info, statErr := os.Stat(path)
		if statErr != nil {
			info, statErr = os.Lstat(path)
		}

		if statErr == nil {
			b.WriteString(path + "\x00" + info.Mode().String() + "\x00" + strconv.FormatInt(info.Size(), 10) + "\x00" +
				strconv.FormatInt(info.ModTime().UnixNano(), 10) + "\n")
		}

		if single || b.Len() > maxMagefiles*256 {
			return filepath.SkipAll
		}

		return nil
	})

	return b.String()
}
