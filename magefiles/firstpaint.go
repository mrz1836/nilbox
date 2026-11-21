//go:build mage

package main

import (
	"cmp"
	"compress/gzip"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"regexp"
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

var (
	// viteTag matches a script or link tag as Vite writes it into the built index.html,
	// its name in the first group and its attributes in the second.
	viteTag = regexp.MustCompile(`<(script|link)\b([^>]*)>`)

	// viteAttr matches one attribute of such a tag as Vite writes it: a lowercase name and
	// a double-quoted value, or a name alone (crossorigin).
	viteAttr = regexp.MustCompile(`([a-z-]+)(?:="([^"]*)")?`)
)

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

// firstPaintRefs reads the files doc, the built index.html as Vite writes it, has the
// window load before its first paint. It fails on a file outside frontend/dist.
func firstPaintRefs(doc string) (paintRefs, error) {
	refs := paintRefs{js: []string{}, css: []string{}}

	for _, tag := range viteTag.FindAllStringSubmatch(doc, -1) {
		css, ref := viteRef(tag[1], tag[2])
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

// viteRef returns the URL of the file a tag viteTag found, by its name and the text of
// its attributes, has the window load before its first paint, "" for none, and whether
// that file is CSS rather than JS: a script's src, a module preload's href, or a
// stylesheet's href.
func viteRef(name, attrText string) (css bool, ref string) {
	attrs := map[string]string{}

	for _, attr := range viteAttr.FindAllStringSubmatch(attrText, -1) {
		attrs[attr[1]] = attr[2]
	}

	switch {
	case name == "script":
		return false, attrs["src"]
	case attrs["rel"] == "modulepreload":
		return false, attrs["href"]
	case attrs["rel"] == "stylesheet":
		return true, attrs["href"]
	default:
		return false, ""
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
