package manifests

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePackageJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data string
		want packageJSON
	}{
		{
			name: "every field",
			data: `{"name": "@acme-org/web", "packageManager": "pnpm@9.1.0+sha512.abc", "workspaces": ["packages/*", "apps/*"],
				"dependencies": {"react": "^19.0.0", "zod": "3"}, "devDependencies": {"vite": "6"},
				"peerDependencies": {"react-dom": "*"}, "optionalDependencies": {"fsevents": "2", "a": "1", "b": "2"}}`,
			want: packageJSON{
				name: "@acme-org/web", manager: ToolPNPM, workspaces: []string{"packages/*", "apps/*"}, deps: 2, devDeps: 1,
				peerDeps: 1, optionalDeps: 3,
			},
		},
		{
			name: "workspaces as Yarn's object, a byte order mark first",
			data: "\ufeff" + `{"packageManager": "yarn@4.5.0", "workspaces": {"packages": ["libs/*", 7, ""], "nohoist": ["x"]}}`,
			want: packageJSON{manager: ToolYarn, workspaces: []string{"libs/*"}},
		},
		{
			name: "fields of other types read as unset",
			data: `{"name": 5, "packageManager": ["npm"], "workspaces": "packages/*", "dependencies": ["react"],
				"devDependencies": null}`,
			want: packageJSON{workspaces: []string{}},
		},
		{
			name: "managers it doesn't know, and bun and npm",
			data: `{"packageManager": "deno@2.0.0"}`,
			want: packageJSON{workspaces: []string{}},
		},
		{name: "bun", data: `{"packageManager": "bun@1.1.0"}`, want: packageJSON{manager: ToolBun, workspaces: []string{}}},
		{name: "npm", data: `{"packageManager": " npm@10.0.0 "}`, want: packageJSON{manager: ToolNPM, workspaces: []string{}}},
		{name: "an empty object", data: "  {}  ", want: packageJSON{workspaces: []string{}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parsePackageJSON([]byte(tt.data))
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParsePackageJSONRefusesWhatIsntAnObject(t *testing.T) {
	t.Parallel()

	for _, data := range []string{"", "null", "[]", `"name"`, "{", `{"name": "x"} trailing`, "\x00{}", "name: x"} {
		_, err := parsePackageJSON([]byte(data))
		require.ErrorIs(t, err, errNotObject, "%q", data)
	}
}

func TestWorkspacesAreCapped(t *testing.T) {
	t.Parallel()

	patterns := make([]string, 0, maxWorkspaces+20)
	for i := range maxWorkspaces + 20 {
		patterns = append(patterns, fmt.Sprintf("packages/p%d", i))
	}

	data, err := json.Marshal(map[string]any{"workspaces": patterns})
	require.NoError(t, err)

	got, err := parsePackageJSON(data)
	require.NoError(t, err)
	assert.Equal(t, patterns[:maxWorkspaces], got.workspaces)
}

// FuzzParsePackageJSON checks that any bytes either fail as no object or parse into
// counts that are never negative and workspaces that are never nil, and that a
// package.json written as npm writes one, from any name, manager, and dependency count,
// parses back to them.
func FuzzParsePackageJSON(f *testing.F) {
	for _, seed := range []struct {
		data, name, manager string
		deps                uint8
	}{
		{"", "web", "pnpm", 2},
		{`{"name": "x", "workspaces": {"packages": ["a"]}, "dependencies": {"a": "1"}}`, "@acme-org/ui", "yarn", 0},
		{`{"dependencies": {"a": 1, "a": 2}, "devDependencies": [1]}`, "é", "deno", 255},
		{"\ufeff{\"packageManager\": \"bun@\"}", "", "npm", 1},
		{"0", "\x84\x82", "0", 1},
	} {
		f.Add([]byte(seed.data), seed.name, seed.manager, seed.deps)
	}

	f.Fuzz(func(t *testing.T, data []byte, name, manager string, deps uint8) {
		got, err := parsePackageJSON(data)
		if err != nil {
			require.ErrorIs(t, err, errNotObject)
		} else {
			assert.NotNil(t, got.workspaces)
			assert.LessOrEqual(t, len(got.workspaces), maxWorkspaces)
			assert.GreaterOrEqual(t, min(got.deps, got.devDeps, got.peerDeps, got.optionalDeps), 0)
		}

		dependencies := map[string]string{}
		for i := range int(deps) {
			dependencies[fmt.Sprintf("dep-%d", i)] = "^1.0.0"
		}

		written, err := json.Marshal(map[string]any{
			"name": name, "packageManager": manager + "@1.0.0", "dependencies": dependencies,
		})
		require.NoError(t, err)

		var asWritten struct {
			Name string `json:"name"`
		}

		require.NoError(t, json.Unmarshal(written, &asWritten), "the name as JSON carries it, bytes it can't replaced")

		got, err = parsePackageJSON(written)
		require.NoError(t, err)
		assert.Equal(t, asWritten.Name, got.name)
		assert.Equal(t, managerOf(manager), got.manager)
		assert.Equal(t, int(deps), got.deps)
	})
}
