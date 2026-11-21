package manifests

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseGoMod(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data string
		want goMod
		ok   bool
	}{
		{
			name: "every directive, single-line and block",
			data: "// The example module.\nmodule github.com/acme-org/example-repo // the module\n\ngo 1.25.0\n\n" +
				"toolchain go1.25.3\n\nrequire github.com/acme-org/one v1.0.0\nrequire golang.org/x/sys v0.30.0 // indirect\n\n" +
				"require (\n\tgithub.com/acme-org/two v2.1.0\n\tgolang.org/x/text v0.20.0 // indirect\n" +
				"\t\"github.com/acme-org/quoted\" v0.1.0 // indirect; from a tool\n\t// a comment line\n\n)\n",
			want: goMod{
				module: "github.com/acme-org/example-repo", goVersion: "1.25.0", toolchain: "go1.25.3", direct: 2, indirect: 3,
			},
			ok: true,
		},
		{
			name: "a quoted module path, a block without its space",
			data: "module \"example.com/m\"\nrequire(\n\texample.com/a v1.0.0\n)\n",
			want: goMod{module: "example.com/m", direct: 1},
			ok:   true,
		},
		{
			name: "blocks it doesn't read are skipped, requirements in them not counted",
			data: "module example.com/m\n\nreplace (\n\texample.com/a => ../a\n\texample.com/b v1.0.0 => example.com/c v1.1.0\n)\n\n" +
				"tool (\n\texample.com/tool\n)\n\nexclude example.com/d v1.0.0\nretract [v1.0.0, v1.0.5]\ngodebug default=go1.21\n" +
				"ignore ./node_modules\n\nrequire example.com/e v1.0.0\n",
			want: goMod{module: "example.com/m", direct: 1},
			ok:   true,
		},
		{
			name: "the first module directive counts, and a comment that only mentions indirect doesn't",
			data: "module example.com/first\nmodule example.com/second\nrequire example.com/a v1.0.0 // not indirect\n" +
				"require example.com/b v1.0.0 // indirectly needed\n",
			want: goMod{module: "example.com/first", direct: 2},
			ok:   true,
		},
		{
			name: "CRLF line ends",
			data: "module example.com/m\r\ngo 1.24\r\nrequire (\r\n\texample.com/a v1.0.0 // indirect\r\n)\r\n",
			want: goMod{module: "example.com/m", goVersion: "1.24", indirect: 1},
			ok:   true,
		},
		{
			name: "no module",
			data: "go 1.22\nrequire example.com/a v1.0.0\n",
			want: goMod{goVersion: "1.22", direct: 1},
		},
		{
			name: "an unclosed block",
			data: "module example.com/m\nrequire (\n\texample.com/a v1.0.0\n\tgo 1.99\n",
			want: goMod{module: "example.com/m", direct: 2},
			ok:   true,
		},
		{name: "nothing", data: ""},
		{name: "not a go.mod", data: "\x00\x01\xff{\"module\": 1}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := parseGoMod([]byte(tt.data))
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

// FuzzParseGoMod checks that any bytes parse without counting more requirements than they
// have lines, ok only with a module, and that a go.mod written the way go writes one,
// from any module path, version, and requirement, parses back to them.
func FuzzParseGoMod(f *testing.F) {
	for _, seed := range []struct {
		data, module, version, require string
		indirect                       bool
	}{
		{"", "example.com/m", "1.25.0", "example.com/a", false},
		{"module x\nrequire (\n a v1\n b v2 // indirect\n)\n", "github.com/acme-org/example-repo", "1.21", "a.b/c", true},
		{"require(\n)\nmodule \"q\"\ngo\n", "m", "1", "r", false},
		{"module\nrequire\nrequire a\n)\n(\n// indirect\n", "x/y/z", "1.2.3", "g.co/v2", true},
	} {
		f.Add([]byte(seed.data), seed.module, seed.version, seed.require, seed.indirect)
	}

	f.Fuzz(func(t *testing.T, data []byte, module, version, require string, indirect bool) {
		got, ok := parseGoMod(data)
		assert.LessOrEqual(t, got.direct+got.indirect, strings.Count(string(data), "\n")+1)
		assert.Equal(t, got.module != "", ok)

		for _, word := range []string{module, version, require} {
			if !plainWord(word) {
				return
			}
		}

		comment := ""
		if indirect {
			comment = " // indirect"
		}

		written := "module " + module + "\n\ngo " + version + "\n\nrequire (\n\t" + require + " v1.0.0" + comment + "\n)\n"
		want := goMod{module: module, goVersion: version, direct: 1}

		if indirect {
			want.direct, want.indirect = 0, 1
		}

		got, ok = parseGoMod([]byte(written))
		assert.True(t, ok)
		assert.Equal(t, want, got)
	})
}

// plainWord reports a word go.mod writes as it is: something, with no space of any kind,
// quote, parenthesis, or comment in it.
func plainWord(word string) bool {
	fields := strings.Fields(word)

	return len(fields) == 1 && fields[0] == word && !strings.ContainsAny(word, "\"'`()") && !strings.Contains(word, "//")
}
