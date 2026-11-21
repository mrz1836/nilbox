//go:build mage

package main

import (
	"cmp"
	"compress/gzip"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"net/url"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
)

// frontendDist is the built frontend, which vite build writes and the app binary embeds.
const frontendDist = frontendDir + "/dist"

// The budgets of what the window loads before its first paint, in bytes as written and
// gzipped (gzipSize), its JS and its CSS each held to its own. Each was set about 7% above
// the first paint it was set on (JS 885,029 bytes, 302,935 gzipped; CSS 127,907 bytes,
// 21,134 gzipped), rounded up, raw to the next 10 kB and gzipped to the next 5 kB, the
// headroom the 750 kB chunk limit in vite.config.ts leaves the entry chunk, so raise one
// only deliberately, in the same change that grows the first paint, never to quiet the
// gate.
const (
	firstPaintJSBytes    = 950_000
	firstPaintJSGzipped  = 325_000
	firstPaintCSSBytes   = 140_000
	firstPaintCSSGzipped = 25_000
)

var (
	// errNoFrontendBuild is returned when frontend/dist holds no index.html to measure.
	errNoFrontendBuild = errors.New(frontendDist + "/index.html is missing; build the frontend first: magex frontend:build")

	// errNoFirstPaintScript is returned when the built index.html loads no script, so there
	// is no first paint to measure.
	errNoFirstPaintScript = errors.New(frontendDist + "/index.html loads no script before the first paint")

	// errNotInDist is returned when the built index.html loads a file from outside
	// frontend/dist.
	errNotInDist = errors.New(frontendDist + "/index.html loads a file from outside " + frontendDist)

	// errOverBudget is returned when the first paint's JS or CSS is over its budget.
	errOverBudget = errors.New("the first paint is over its budget")
)

// htmlSpace is the whitespace HTML allows between a tag's name and its attributes.
const htmlSpace = " \t\n\f\r"

// paintSize is a size in bytes, as written and gzipped.
type paintSize struct {
	raw     int64
	gzipped int64
}

// paintBudgets are the budgets of the first paint's JS and of its CSS.
type paintBudgets struct {
	js  paintSize
	css paintSize
}

// firstPaintBudgets are the budgets the frontend gate holds the first paint to.
func firstPaintBudgets() paintBudgets {
	return paintBudgets{
		js:  paintSize{raw: firstPaintJSBytes, gzipped: firstPaintJSGzipped},
		css: paintSize{raw: firstPaintCSSBytes, gzipped: firstPaintCSSGzipped},
	}
}

// paintFile is one file the window loads before its first paint: its path under
// frontend/dist, slash-separated, and its size.
type paintFile struct {
	name string
	size paintSize
}

// paintPart is one kind of file the first paint loads, JS or CSS: the files, in the order
// index.html names them, and the budget their total is held to.
type paintPart struct {
	kind   string
	files  []paintFile
	budget paintSize
}

// total is the sum of the part's files' sizes.
func (p paintPart) total() paintSize {
	var t paintSize

	for _, f := range p.files {
		t.raw += f.size.raw
		t.gzipped += f.size.gzipped
	}

	return t
}

// line is the part's report: its totals, how many files they add up, and its budget.
func (p paintPart) line() string {
	t := p.total()

	return fmt.Sprintf("First paint %s: %s bytes (%s gzipped) in %s; budget %s (%s gzipped)",
		p.kind, grouped(t.raw), grouped(t.gzipped), fileCount(len(p.files)), grouped(p.budget.raw), grouped(p.budget.gzipped))
}

// over words each of the part's totals that is over its budget, raw and gzipped, or
// returns none.
func (p paintPart) over() []string {
	t := p.total()

	var over []string

	if t.raw > p.budget.raw {
		over = append(over, fmt.Sprintf("%s is %s bytes, over its %s", p.kind, grouped(t.raw), grouped(p.budget.raw)))
	}

	if t.gzipped > p.budget.gzipped {
		over = append(over, fmt.Sprintf("%s is %s bytes gzipped, over its %s", p.kind, grouped(t.gzipped), grouped(p.budget.gzipped)))
	}

	return over
}

// fileLines lists the part's files, biggest first, a line each, under a line that names
// the part.
func (p paintPart) fileLines() []string {
	files := slices.Clone(p.files)
	slices.SortStableFunc(files, func(a, b paintFile) int { return cmp.Compare(b.size.raw, a.size.raw) })

	lines := make([]string, 0, len(files)+1)
	lines = append(lines, "The "+p.kind+" the first paint loads, biggest first:")

	for _, f := range files {
		lines = append(lines, fmt.Sprintf("  %s: %s bytes (%s gzipped)", f.name, grouped(f.size.raw), grouped(f.size.gzipped)))
	}

	return lines
}

// checkFirstPaint measures what the built window loads before its first paint, from
// frontend/dist/index.html (readFirstPaint), and prints its JS's and its CSS's totals
// against their budgets. It fails when either is over its budget, raw or gzipped, naming
// the totals over it and the files that add up to them.
func (r runner) checkFirstPaint(budgets paintBudgets) error {
	paint, err := readFirstPaint(r.path(frontendDist), budgets)
	if err != nil {
		return err
	}

	var (
		all   paintSize
		over  []string
		files []string
	)

	for _, part := range paint {
		r.say(part.line())

		t := part.total()
		all.raw += t.raw
		all.gzipped += t.gzipped

		if o := part.over(); len(o) > 0 {
			over = append(over, o...)
			files = append(files, part.fileLines()...)
		}
	}

	r.say(fmt.Sprintf("First paint in all: %s bytes (%s gzipped)", grouped(all.raw), grouped(all.gzipped)))

	if len(over) > 0 {
		return fmt.Errorf("%w: %s (raise a budget in magefiles/firstpaint.go only on purpose, "+
			"in the change that grows the first paint)\n%s", errOverBudget, strings.Join(over, "; "), strings.Join(files, "\n"))
	}

	r.say("✅ the first paint is within its budgets")

	return nil
}

// readFirstPaint measures what the built index.html in dist has the window load before
// its first paint (measureFirstPaint), reading only inside dist.
func readFirstPaint(dist string, budgets paintBudgets) ([]paintPart, error) {
	root, err := os.OpenRoot(dist)
	if err != nil {
		return nil, noFrontendBuild(err)
	}
	defer func() { _ = root.Close() }()

	return measureFirstPaint(root.FS(), budgets)
}

// measureFirstPaint measures what index.html in fsys, the built frontend, has the window
// load before its first paint, as Vite writes it: the entry script and every module it
// preloads, the JS, and its stylesheets, the CSS, each held to its budget in budgets.
func measureFirstPaint(fsys fs.FS, budgets paintBudgets) ([]paintPart, error) {
	doc, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		return nil, noFrontendBuild(err)
	}

	refs, err := firstPaintRefs(string(doc))
	if err != nil {
		return nil, err
	}

	if len(refs.js) == 0 {
		return nil, errNoFirstPaintScript
	}

	js, err := measurePaintFiles(fsys, refs.js)
	if err != nil {
		return nil, err
	}

	css, err := measurePaintFiles(fsys, refs.css)
	if err != nil {
		return nil, err
	}

	return []paintPart{{kind: "JS", files: js, budget: budgets.js}, {kind: "CSS", files: css, budget: budgets.css}}, nil
}

// noFrontendBuild words err, the failure to read the built index.html: errNoFrontendBuild
// when there is none.
func noFrontendBuild(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w (%w)", errNoFrontendBuild, err)
	}

	return fmt.Errorf("read %s/index.html: %w", frontendDist, err)
}

// measurePaintFiles measures each of the files names, paths in fsys.
func measurePaintFiles(fsys fs.FS, names []string) ([]paintFile, error) {
	files := make([]paintFile, 0, len(names))

	for _, name := range names {
		body, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("read %s, which %s/index.html loads before the first paint: %w", name, frontendDist, err)
		}

		gzipped, err := gzipSize(body)
		if err != nil {
			return nil, fmt.Errorf("gzip %s: %w", name, err)
		}

		files = append(files, paintFile{name: name, size: paintSize{raw: int64(len(body)), gzipped: gzipped}})
	}

	return files, nil
}

// gzipSize returns the size of body gzipped at the default level, as Vite's size report
// gzips each file. Go's compressor is not zlib, which Vite's report uses, so its sizes
// come out about 2% above the report's.
func gzipSize(body []byte) (int64, error) {
	var n byteCount

	zw := gzip.NewWriter(&n)

	if _, err := zw.Write(body); err != nil {
		return 0, fmt.Errorf("gzip: %w", err)
	}

	if err := zw.Close(); err != nil {
		return 0, fmt.Errorf("gzip: %w", err)
	}

	return int64(n), nil
}

// byteCount is a writer that counts the bytes it is given and keeps none of them.
type byteCount int64

// Write counts p.
func (c *byteCount) Write(p []byte) (int, error) {
	*c += byteCount(len(p))

	return len(p), nil
}

// paintRefs are the files a built index.html has the window load before its first paint,
// by their paths under frontend/dist, slash-separated, each once, in the order the page
// names them: the JS, its scripts and the modules they preload, and the CSS, its
// stylesheets.
type paintRefs struct {
	js  []string
	css []string
}

// firstPaintRefs reads the files doc, the built index.html, has the window load before
// its first paint. It fails on a file outside frontend/dist.
func firstPaintRefs(doc string) (paintRefs, error) {
	refs := paintRefs{js: []string{}, css: []string{}}

	for _, tag := range startTags(doc) {
		css, ref := paintRef(tag)
		if ref == "" {
			continue
		}

		name, err := distFile(ref)
		if err != nil {
			return paintRefs{}, err
		}

		list := &refs.js
		if css {
			list = &refs.css
		}

		if !slices.Contains(*list, name) {
			*list = append(*list, name)
		}
	}

	return refs, nil
}

// paintRef returns the URL of the file tag has the window load before its first paint, ""
// for none, and whether that file is CSS rather than JS: a script's src, a module
// preload's href, or a stylesheet's href.
func paintRef(tag htmlTag) (css bool, ref string) {
	switch {
	case tag.name == "script" && loadsScript(tag):
		return false, strings.TrimSpace(tag.attrs["src"])
	case tag.name == "link" && tag.relHas("modulepreload"):
		return false, strings.TrimSpace(tag.attrs["href"])
	case tag.name == "link" && tag.relHas("stylesheet"):
		return true, strings.TrimSpace(tag.attrs["href"])
	default:
		return false, ""
	}
}

// loadsScript reports whether the script tag runs a script from its src, as a module or
// as a classic script: not a nomodule fallback, which an engine that runs modules skips,
// nor a data block of another type.
func loadsScript(tag htmlTag) bool {
	if _, nomodule := tag.attrs["nomodule"]; nomodule {
		return false
	}

	switch strings.ToLower(strings.TrimSpace(tag.attrs["type"])) {
	case "", "module", "text/javascript", "application/javascript":
		return true
	default:
		return false
	}
}

// distFile returns the path under frontend/dist of the file ref, a URL as the built
// index.html writes it, names: "/assets/a.js", "./assets/a.js", and "assets/a.js?v=1" all
// name assets/a.js. A URL with a scheme or a host names a file outside frontend/dist, and
// so does one that names the folder itself.
func distFile(ref string) (string, error) {
	u, err := url.Parse(ref)
	if err != nil {
		return "", fmt.Errorf("%w: %w", errNotInDist, err)
	}

	name := strings.TrimPrefix(path.Clean("/"+u.Path), "/")
	if u.Scheme != "" || u.Host != "" || u.Opaque != "" || name == "" {
		return "", fmt.Errorf("%w: %s", errNotInDist, ref)
	}

	return name, nil
}

// htmlTag is a start tag of an HTML document: its name and its attributes, with names
// lowercased and values unescaped, keeping the first of an attribute written twice, as a
// browser does.
type htmlTag struct {
	name  string
	attrs map[string]string
}

// relHas reports whether the tag's rel attribute holds token, a lowercase keyword, read as
// a browser reads the list: split at whitespace, in any case.
func (t htmlTag) relHas(token string) bool {
	return slices.Contains(strings.Fields(strings.ToLower(t.attrs["rel"])), token)
}

// startTags returns the start tags of doc, an HTML document, in order. Comments, end tags,
// and doctypes are skipped, and so is the text of an element whose content is not markup
// (rawTextElement), so a tag written in a comment or in a script's text is not one.
func startTags(doc string) []htmlTag {
	var tags []htmlTag

	for rest := doc; rest != ""; {
		var (
			tag htmlTag
			ok  bool
		)

		tag, rest, ok = nextStartTag(rest)
		if ok {
			tags = append(tags, tag)
		}
	}

	return tags
}

// nextStartTag reads doc up to the end of what its first '<' opens, and returns the start
// tag it opens, if it opens one (ok), and the rest of doc after it: after the element's
// text and up to its end tag, for an element whose content is not markup.
func nextStartTag(doc string) (tag htmlTag, rest string, ok bool) {
	i := strings.IndexByte(doc, '<')
	if i < 0 {
		return htmlTag{}, "", false
	}

	rest = doc[i+1:]

	switch {
	case strings.HasPrefix(rest, "!--"):
		return htmlTag{}, after(rest[len("!--"):], "-->"), false
	case rest != "" && isASCIILetter(rest[0]):
		tag, rest = readStartTag(rest)
		if rawTextElement(tag.name) {
			rest = skipRawText(rest, tag.name)
		}

		return tag, rest, true
	case strings.HasPrefix(rest, "/"), strings.HasPrefix(rest, "!"), strings.HasPrefix(rest, "?"):
		return htmlTag{}, after(rest, ">"), false
	default:
		return htmlTag{}, rest, false // a '<' that opens nothing is text
	}
}

// readStartTag reads a start tag from s, which begins with its name, and returns the tag
// and the rest of s after it.
func readStartTag(s string) (htmlTag, string) {
	n := strings.IndexAny(s, htmlSpace+"/>")
	if n < 0 {
		n = len(s)
	}

	tag := htmlTag{name: strings.ToLower(s[:n]), attrs: map[string]string{}}
	rest := s[n:]

	for {
		rest = strings.TrimLeft(rest, htmlSpace+"/")
		if rest == "" {
			return tag, ""
		}

		if rest[0] == '>' {
			return tag, rest[1:]
		}

		var name, value string

		name, value, rest = readAttr(rest)
		if _, seen := tag.attrs[name]; !seen {
			tag.attrs[name] = value
		}
	}
}

// readAttr reads one attribute from s, which begins with its name, and returns its name,
// lowercased, its value, unescaped ("" when it has none), and the rest of s after it.
func readAttr(s string) (name, value, rest string) {
	// A name runs to whitespace, '/', '>', or '=', but may begin with '='.
	n := 1 + strings.IndexAny(s[1:], htmlSpace+"/>=")
	if n == 0 {
		n = len(s)
	}

	name, rest = strings.ToLower(s[:n]), strings.TrimLeft(s[n:], htmlSpace)
	if !strings.HasPrefix(rest, "=") {
		return name, "", rest
	}

	value, rest = readAttrValue(strings.TrimLeft(rest[1:], htmlSpace))

	return name, html.UnescapeString(value), rest
}

// readAttrValue reads an attribute's value from the start of s, quoted, up to its closing
// quote, or unquoted, up to whitespace or the tag's end, and returns it and the rest of s
// after it.
func readAttrValue(s string) (value, rest string) {
	if s != "" && (s[0] == '"' || s[0] == '\'') {
		value, rest, _ = strings.Cut(s[1:], s[:1])

		return value, rest
	}

	n := strings.IndexAny(s, htmlSpace+">")
	if n < 0 {
		n = len(s)
	}

	return s[:n], s[n:]
}

// rawTextElement reports whether the element name's content is text rather than markup,
// up to its end tag, in a window that runs scripts.
func rawTextElement(name string) bool {
	return slices.Contains([]string{"script", "style", "title", "textarea", "xmp", "iframe", "noembed", "noframes", "noscript"}, name)
}

// skipRawText returns s from the end tag of the element name, whose text s begins with,
// or "" when it has none.
func skipRawText(s, name string) string {
	for i := 0; ; i += len("</") {
		j := strings.Index(s[i:], "</")
		if j < 0 {
			return ""
		}

		i += j
		if end := s[i+len("</"):]; len(end) >= len(name) && strings.EqualFold(end[:len(name)], name) {
			return s[i:]
		}
	}
}

// after returns s after the first sep in it, or "" when it holds none.
func after(s, sep string) string {
	_, rest, found := strings.Cut(s, sep)
	if !found {
		return ""
	}

	return rest
}

// isASCIILetter reports whether c is an ASCII letter, which a tag's name begins with.
func isASCIILetter(c byte) bool {
	return ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z')
}

// grouped writes n with a comma between each group of three digits: 884,598.
func grouped(n int64) string {
	digits := strconv.FormatInt(n, 10)

	var b strings.Builder

	for i, c := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}

		b.WriteRune(c)
	}

	return b.String()
}

// fileCount words n files: "1 file", "21 files".
func fileCount(n int) string {
	if n == 1 {
		return "1 file"
	}

	return strconv.Itoa(n) + " files"
}
