//go:build mage

package main

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// viteIndex is a built index.html as Vite writes it: the entry script, a module it
// preloads, and its stylesheet.
const viteIndex = `<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>example-app</title>
    <script type="module" crossorigin src="/assets/index-a1.js"></script>
    <link rel="modulepreload" crossorigin href="/assets/shared-b2.js">
    <link rel="stylesheet" crossorigin href="/assets/index-c3.css">
  </head>
  <body>
    <div id="root"></div>
  </body>
</html>
`

// writeDist writes files, each content by its slash-separated path under frontend/dist,
// into the frontend/dist of the checkout dir.
func writeDist(t *testing.T, dir string, files map[string]string) {
	t.Helper()

	for name, content := range files {
		testutil.WriteFile(t, filepath.Join(dir, filepath.FromSlash(frontendDist+"/"+name)), content)
	}
}

// viteDist is the build viteIndex names: a 1,000-byte entry script, a module it preloads,
// and a stylesheet.
func viteDist() map[string]string {
	return map[string]string{
		"index.html":           viteIndex,
		"assets/index-a1.js":   strings.Repeat("a", 1000),
		"assets/shared-b2.js":  "export const b = 2\n",
		"assets/index-c3.css":  "body{color:red}\n",
		"assets/lazy-d4.js":    "export const lazy = 4\n",
		"assets/unused-e5.css": "p{margin:0}\n",
	}
}

// sizeOf is the size of content, as written and gzipped.
func sizeOf(t *testing.T, content ...string) paintSize {
	t.Helper()

	var size paintSize

	for _, c := range content {
		gzipped, err := gzipSize([]byte(c))
		require.NoError(t, err)

		size.raw += int64(len(c))
		size.gzipped += gzipped
	}

	return size
}

// roomy are budgets no test build comes near.
func roomy() paintBudgets {
	return paintBudgets{js: paintSize{raw: 10_000, gzipped: 5_000}, css: paintSize{raw: 1_000, gzipped: 500}}
}

func TestFirstPaintRefs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		doc  string
		js   []string
		css  []string
	}{
		{
			name: "the index.html Vite writes",
			doc:  viteIndex,
			js:   []string{"assets/index-a1.js", "assets/shared-b2.js"},
			css:  []string{"assets/index-c3.css"},
		},
		{
			name: "names and values in any case and quoting",
			doc: `<SCRIPT TYPE=MODULE SRC=/assets/a.js></SCRIPT>` +
				`<link REL='ModulePreload' href='./assets/b.js'/>` +
				`<link rel="preload  stylesheet" href="assets/c.css?v=1#top">`,
			js:  []string{"assets/a.js", "assets/b.js"},
			css: []string{"assets/c.css"},
		},
		{
			name: "values unescaped and trimmed",
			doc:  `<script type="module" src=" /assets/a&amp;b.js "></script><link rel=stylesheet href="/assets/c%20d.css">`,
			js:   []string{"assets/a&b.js"},
			css:  []string{"assets/c d.css"},
		},
		{
			name: "tags in comments and in a script's text are not tags",
			doc: `<!-- <script type="module" src="/assets/old.js"></script> -->` +
				`<script>if (a<b) document.write('<link rel=stylesheet href=/assets/x.css>')</script>` +
				`<style>a::after{content:"<link rel=stylesheet href=/assets/y.css>"}</style>` +
				`<script type="module" src="/assets/a.js"></script>`,
			js:  []string{"assets/a.js"},
			css: []string{},
		},
		{
			name: "only what the first paint loads",
			doc: `<script type="module" src="/assets/a.js"></script>` +
				`<script nomodule src="/assets/legacy.js"></script>` +
				`<script type="text/plain" src="/assets/data.txt"></script>` +
				`<script type="module">import "./inline.js"</script>` +
				`<script src="/assets/classic.js"></script>` +
				`<link rel="modulepreload">` +
				`<link rel="preload" as="font" href="/assets/font.woff2">` +
				`<link rel="icon" href="/favicon.ico">` +
				`<a href="/assets/link.css">a link</a>`,
			js:  []string{"assets/a.js", "assets/classic.js"},
			css: []string{},
		},
		{
			name: "each file once, the first of an attribute written twice",
			doc: `<script type="module" src="/assets/a.js" src="/assets/b.js"></script>` +
				`<link rel="modulepreload" href="/assets/a.js">` +
				`<link rel="stylesheet" href="/assets/c.css"><link rel="stylesheet" href="./assets/c.css">`,
			js:  []string{"assets/a.js"},
			css: []string{"assets/c.css"},
		},
		{
			name: "a stray '<' is text",
			doc:  `<p>1 < 2 <script type="module" src="/assets/a.js"></script></p>`,
			js:   []string{"assets/a.js"},
			css:  []string{},
		},
		{
			name: "a path that climbs stays in dist",
			doc:  `<script type="module" src="/../../assets/a.js"></script>`,
			js:   []string{"assets/a.js"},
			css:  []string{},
		},
		{
			name: "a tag cut short",
			doc:  `<script type="module" src="/assets/a.js`,
			js:   []string{"assets/a.js"},
			css:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			refs, err := firstPaintRefs(tt.doc)
			require.NoError(t, err)

			assert.Equal(t, tt.js, refs.js, "JS")
			assert.Equal(t, tt.css, refs.css, "CSS")
		})
	}
}

func TestFirstPaintRefsOutsideDist(t *testing.T) {
	t.Parallel()

	for name, doc := range map[string]string{
		"a URL with a scheme":     `<script type="module" src="https://cdn.example.com/a.js"></script>`,
		"a URL with a host":       `<link rel="stylesheet" href="//cdn.example.com/a.css">`,
		"a data URL":              `<script type="module" src="data:text/javascript,export%20{}"></script>`,
		"the folder itself":       `<link rel="modulepreload" href="/">`,
		"a URL that doesn't read": `<script type="module" src="/assets/%zz.js"></script>`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := firstPaintRefs(doc)

			require.ErrorIs(t, err, errNotInDist)
		})
	}
}

func TestCheckFirstPaint(t *testing.T) {
	t.Parallel()

	dist := viteDist()
	js := sizeOf(t, dist["assets/index-a1.js"], dist["assets/shared-b2.js"])
	css := sizeOf(t, dist["assets/index-c3.css"])

	// totals is what the check prints of the build viteDist is, against budgets.
	totals := func(budgets paintBudgets) []string {
		return []string{
			fmt.Sprintf("First paint JS: %s bytes (%s gzipped) in 2 files; budget %s (%s gzipped)",
				grouped(js.raw), grouped(js.gzipped), grouped(budgets.js.raw), grouped(budgets.js.gzipped)),
			fmt.Sprintf("First paint CSS: %s bytes (%s gzipped) in 1 file; budget %s (%s gzipped)",
				grouped(css.raw), grouped(css.gzipped), grouped(budgets.css.raw), grouped(budgets.css.gzipped)),
			fmt.Sprintf("First paint in all: %s bytes (%s gzipped)", grouped(js.raw+css.raw), grouped(js.gzipped+css.gzipped)),
		}
	}

	t.Run("within its budgets, it prints the totals", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		writeDist(t, g.dir, dist)

		require.NoError(t, g.runner().checkFirstPaint(roomy()))

		want := append(totals(roomy()), "✅ the first paint is within its budgets")
		assert.Equal(t, strings.Join(want, "\n")+"\n", g.out.String(), "the preloaded module and the stylesheet count; the lazy chunk does not")
	})

	t.Run("over its budget, it fails naming the totals and the files", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		writeDist(t, g.dir, dist)

		budgets := roomy()
		budgets.js.raw = 1_000

		err := g.runner().checkFirstPaint(budgets)

		require.ErrorIs(t, err, errOverBudget)
		assert.Contains(t, err.Error(), "JS is "+grouped(js.raw)+" bytes, over its 1,000")
		assert.NotContains(t, err.Error(), "gzipped, over", "the gzipped JS is within its budget")
		assert.NotContains(t, err.Error(), "CSS", "the CSS is within its budget")
		assert.Contains(t, err.Error(), "The JS the first paint loads, biggest first:\n"+
			"  assets/index-a1.js: 1,000 bytes ("+grouped(sizeOf(t, dist["assets/index-a1.js"]).gzipped)+" gzipped)\n"+
			"  assets/shared-b2.js: "+grouped(int64(len(dist["assets/shared-b2.js"])))+" bytes")
		assert.Equal(t, strings.Join(totals(budgets), "\n")+"\n", g.out.String(), "the numbers print before it fails")
	})

	t.Run("gzipped over its budget, it fails", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		writeDist(t, g.dir, dist)

		budgets := roomy()
		budgets.js.gzipped = 10

		err := g.runner().checkFirstPaint(budgets)

		require.ErrorIs(t, err, errOverBudget)
		assert.Contains(t, err.Error(), "JS is "+grouped(js.gzipped)+" bytes gzipped, over its 10")
		assert.NotContains(t, err.Error(), "bytes, over", "the JS as written is within its budget")
	})

	t.Run("CSS over its budget, it names the CSS", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		writeDist(t, g.dir, dist)

		budgets := roomy()
		budgets.css.raw = 5

		err := g.runner().checkFirstPaint(budgets)

		require.ErrorIs(t, err, errOverBudget)
		assert.Contains(t, err.Error(), "CSS is "+grouped(css.raw)+" bytes, over its 5")
		assert.Contains(t, err.Error(), "The CSS the first paint loads, biggest first:\n  assets/index-c3.css: ")
		assert.NotContains(t, err.Error(), "assets/index-a1.js", "the JS is within its budget")
	})

	t.Run("no build", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)

		err := g.runner().checkFirstPaint(roomy())

		require.ErrorIs(t, err, errNoFrontendBuild)
		assert.Contains(t, err.Error(), "frontend/dist/index.html is missing; build the frontend first: magex frontend:build")
	})

	t.Run("a build without index.html", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		writeDist(t, g.dir, map[string]string{"assets/index-a1.js": "export {}\n"})

		require.ErrorIs(t, g.runner().checkFirstPaint(roomy()), errNoFrontendBuild)
	})

	t.Run("a file index.html loads is missing", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		writeDist(t, g.dir, map[string]string{"index.html": viteIndex, "assets/index-a1.js": "export {}\n"})

		err := g.runner().checkFirstPaint(roomy())

		require.ErrorIs(t, err, fs.ErrNotExist)
		assert.Contains(t, err.Error(), "read assets/shared-b2.js, which frontend/dist/index.html loads before the first paint")
	})

	t.Run("an index.html that loads no script", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		writeDist(t, g.dir, map[string]string{"index.html": `<link rel="stylesheet" href="/assets/index-c3.css">`})

		require.ErrorIs(t, g.runner().checkFirstPaint(roomy()), errNoFirstPaintScript)
	})

	t.Run("an index.html that loads a file from elsewhere", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		writeDist(t, g.dir, map[string]string{"index.html": `<script type="module" src="https://cdn.example.com/a.js"></script>`})

		require.ErrorIs(t, g.runner().checkFirstPaint(roomy()), errNotInDist)
	})

	t.Run("a link out of dist is not followed", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		writeDist(t, g.dir, map[string]string{"index.html": `<script type="module" src="/assets/a.js"></script>`})

		outside := g.path("outside.js")
		testutil.WriteFile(t, outside, "export {}\n")
		mkdir(t, g.path(frontendDist+"/assets"))
		require.NoError(t, os.Symlink(outside, g.path(frontendDist+"/assets/a.js")))

		err := g.runner().checkFirstPaint(roomy())

		require.Error(t, err)
		assert.Contains(t, err.Error(), "read assets/a.js")
	})
}

func TestGzipSize(t *testing.T) {
	t.Parallel()

	random := make([]byte, 4096)
	_, err := rand.Read(random)
	require.NoError(t, err)

	for name, body := range map[string][]byte{
		"compressible":   bytes.Repeat([]byte("export const a = 1;\n"), 5000),
		"incompressible": random,
		"empty":          nil,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			zw, err := gzip.NewWriterLevel(&buf, gzip.DefaultCompression)
			require.NoError(t, err)

			_, err = zw.Write(body)
			require.NoError(t, err)
			require.NoError(t, zw.Close())

			got, err := gzipSize(body)
			require.NoError(t, err)

			assert.Equal(t, int64(buf.Len()), got, "gzipped at the default level")

			zr, err := gzip.NewReader(&buf)
			require.NoError(t, err)

			back, err := io.ReadAll(zr)
			require.NoError(t, err)
			assert.Len(t, back, len(body), "a gzip stream of the whole body")
		})
	}

	t.Run("smaller than the body when it compresses", func(t *testing.T) {
		t.Parallel()

		body := bytes.Repeat([]byte("export const a = 1;\n"), 5000)

		got, err := gzipSize(body)
		require.NoError(t, err)

		assert.Less(t, got, int64(len(body))/10)
	})
}

func TestGrouped(t *testing.T) {
	t.Parallel()

	for n, want := range map[int64]string{
		0:         "0",
		7:         "7",
		999:       "999",
		1_000:     "1,000",
		25_000:    "25,000",
		884_598:   "884,598",
		1_012_936: "1,012,936",
	} {
		assert.Equal(t, want, grouped(n))
	}
}

func TestFileCount(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "0 files", fileCount(0))
	assert.Equal(t, "1 file", fileCount(1))
	assert.Equal(t, "21 files", fileCount(21))
}
