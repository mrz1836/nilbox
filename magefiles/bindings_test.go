//go:build mage

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// enumModule is the module path of the fixture packages the enum tests write.
const enumModule = "example.com/acme"

// kindsSource declares the Kind enum of the fixture package kinds.
const kindsSource = `package kinds

// Kind is what an item is.
type Kind string

// The kinds.
const (
	KindA Kind = "a"
	KindB Kind = "b"
)
`

// kindsModels is the kinds package's models as the binding generator renders Kind with a
// DefaultKind constant.
const kindsModels = `/**
 * Kind is what an item is.
 */
export enum Kind {
    /**
     * The Go zero value for the underlying type of the enum.
     */
    $zero = "",

    /**
     * The kinds.
     */
    KindA = "a",
    KindB = "b",

    /**
     * DefaultKind is the kind of a new item.
     */
    DefaultKind = "a",
};
`

// enumGenerator is a fake go's answer that writes kindsModels into the folder after -d.
func enumGenerator(t *testing.T) answer {
	t.Helper()

	return func(args []string) testutil.FakeRun {
		out := filepath.Join(argAfter(args, "-d"), enumModule, "kinds")
		mkdir(t, out)

		return writes(filepath.Join(out, modelsFile), kindsModels)
	}
}

// writeKinds makes dir a module holding the kinds package, with DefaultKind declared in
// the file named defaultIn.
func writeKinds(t *testing.T, dir, defaultIn string) {
	t.Helper()

	testutil.WriteFile(t, filepath.Join(dir, goModFile), "module "+enumModule+"\n\ngo 1.26\n")
	testutil.WriteFile(t, filepath.Join(dir, "kinds", "kinds.go"), kindsSource)

	const defaultKind = "\n// DefaultKind is the kind of a new item.\nconst DefaultKind = KindA\n"

	path := filepath.Join(dir, "kinds", defaultIn)
	if defaultIn == "kinds.go" {
		testutil.WriteFile(t, path, kindsSource+defaultKind)

		return
	}

	testutil.WriteFile(t, path, "package kinds\n"+defaultKind)
}

func TestBindingsRefuseAnEnumSplitAcrossFiles(t *testing.T) {
	t.Parallel()

	t.Run("app:bindings names the enum and its files", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		writeKinds(t, g.dir, "defaults.go")
		fakeGo(g, enumGenerator(t))

		err := g.runner().generateBindings(bindingsDir)

		require.ErrorIs(t, err, errSplitEnum)
		assert.Contains(t, err.Error(), enumModule+"/kinds.Kind: defaults.go, kinds.go")
	})

	t.Run("app:bindingsCheck refuses it rather than report drift by chance", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		writeKinds(t, g.dir, "defaults.go")
		fakeGo(g, enumGenerator(t))
		require.ErrorIs(t, g.runner().generateBindings(bindingsDir), errSplitEnum)

		err := g.runner().checkBindings()

		require.ErrorIs(t, err, errSplitEnum)
		assert.NotErrorIs(t, err, errBindingsDrift)
	})

	t.Run("an enum declared in one file passes", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		writeKinds(t, g.dir, "kinds.go")
		fakeGo(g, enumGenerator(t))

		require.NoError(t, g.runner().generateBindings(bindingsDir))
		assert.Contains(t, readFile(t, filepath.Join(g.path(bindingsDir), enumModule, "kinds", modelsFile)),
			`export type Kind = "a" | "b";`)

		require.NoError(t, g.runner().checkBindings())
	})
}

func TestBoundEnumsAreEachDeclaredInOneFile(t *testing.T) {
	t.Parallel()

	r, _ := realRunner(t, repoRoot(t), nil)

	files, err := treeFiles(r.path(bindingsDir))
	require.NoError(t, err)

	module, err := modulePath(r.path(goModFile))
	require.NoError(t, err)

	enums := boundEnums(files)
	require.Contains(t, enums[module+"/internal/autopilot/catalog"]["Preset"], "DefaultPreset",
		"the check reads the committed bindings' enums")

	require.NoError(t, r.checkEnumFiles(r.path(bindingsDir)))
}

func TestCheckEnumFiles(t *testing.T) {
	t.Parallel()

	// models writes a tidy models file for pkg under root holding the named enums, each
	// with its members.
	models := func(t *testing.T, root, pkg string, enums map[string][]string) {
		t.Helper()

		body := ""

		for name, members := range enums {
			body += "export type " + name + ` = "x";` + "\n\nexport const " + name + " = {\n"
			for _, member := range members {
				body += "    /**\n     * Doc: not a member,\n     */\n    " + member + `: "x",` + "\n"
			}

			body += enumEnd + "\n\n"
		}

		testutil.WriteFile(t, filepath.Join(root, filepath.FromSlash(pkg), modelsFile), body)
	}

	t.Run("names each enum split across files, sorted", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		writeKinds(t, g.dir, "defaults.go")
		testutil.WriteFile(t, g.path("states/states.go"), "package states\n\ntype State int\n\nconst (\n\tOpen State = iota\n\tShut\n)\n")
		testutil.WriteFile(t, g.path("states/more.go"), "package states\n\nconst Gone State = 9\n")
		testutil.WriteFile(t, g.path("whole/whole.go"), "package whole\n\ntype Mode string\n\nconst (\n\tOn Mode = \"on\"\n\tOff Mode = \"off\"\n)\n")

		root := t.TempDir()
		models(t, root, enumModule+"/kinds", map[string][]string{"Kind": {"KindA", "KindB", "DefaultKind"}})
		models(t, root, enumModule+"/states", map[string][]string{"State": {"Open", "Shut", "Gone"}})
		models(t, root, enumModule+"/whole", map[string][]string{"Mode": {"On", "Off", "Stale"}})
		models(t, root, "example.org/other/split", map[string][]string{"Far": {"A", "B"}})

		err := g.runner().checkEnumFiles(root)

		require.ErrorIs(t, err, errSplitEnum)
		assert.Contains(t, err.Error(), ":\n  "+enumModule+"/kinds.Kind: defaults.go, kinds.go\n  "+enumModule+"/states.State: more.go, states.go")
		assert.NotContains(t, err.Error(), "whole", "an enum in one file, and a member it no longer declares, pass")
		assert.NotContains(t, err.Error(), "Far", "another module's package is not this repository's to change")
	})

	t.Run("reads the files the desktop build compiles", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		writeKinds(t, g.dir, "kinds.go")
		testutil.WriteFile(t, g.path("kinds/legacy.go"), "//go:build ignore\n\npackage kinds\n\nconst KindB = Kind(\"b\")\n")

		root := t.TempDir()
		models(t, root, enumModule+"/kinds", map[string][]string{"Kind": {"KindA", "KindB", "DefaultKind"}})

		require.NoError(t, g.runner().checkEnumFiles(root), "a file the build leaves out declares nothing")

		testutil.WriteFile(t, g.path("kinds/shell.go"), "//go:build "+desktopTag+"\n\npackage kinds\n\nconst KindC Kind = \"c\"\n")
		models(t, root, enumModule+"/kinds", map[string][]string{"Kind": {"KindA", "KindC"}})

		err := g.runner().checkEnumFiles(root)

		require.ErrorIs(t, err, errSplitEnum)
		assert.Contains(t, err.Error(), enumModule+"/kinds.Kind: kinds.go, shell.go")
	})

	t.Run("an enum in the module's root package", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		testutil.WriteFile(t, g.path(goModFile), "module "+enumModule+"\n")
		testutil.WriteFile(t, g.path("a.go"), "package acme\n\ntype T string\n\nconst A T = \"a\"\n")
		testutil.WriteFile(t, g.path("b.go"), "package acme\n\nconst B T = \"b\"\n")

		root := t.TempDir()
		models(t, root, enumModule, map[string][]string{"T": {"A", "B"}})

		err := g.runner().checkEnumFiles(root)

		require.ErrorIs(t, err, errSplitEnum)
		assert.Contains(t, err.Error(), enumModule+".T: a.go, b.go")
	})

	t.Run("bindings with no enums need no module", func(t *testing.T) {
		t.Parallel()

		r := newRig(t).runner()

		root := t.TempDir()
		testutil.WriteFile(t, filepath.Join(root, "models", "a.ts"), "export const a = {\n    b: 1,\n} as const;\n")
		testutil.WriteFile(t, filepath.Join(root, "x", modelsFile), "export interface X {\n}\n")

		require.NoError(t, r.checkEnumFiles(root))
		require.NoError(t, r.checkEnumFiles(filepath.Join(root, "missing")))
	})

	t.Run("go.mod missing", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)

		root := t.TempDir()
		models(t, root, enumModule+"/kinds", map[string][]string{"Kind": {"KindA"}})

		err := g.runner().checkEnumFiles(root)

		require.ErrorIs(t, err, os.ErrNotExist)
		assert.Contains(t, err.Error(), "read "+g.path(goModFile))
	})

	t.Run("the package is gone", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		testutil.WriteFile(t, g.path(goModFile), "module "+enumModule+"\n")

		root := t.TempDir()
		models(t, root, enumModule+"/kinds", map[string][]string{"Kind": {"KindA"}})

		err := g.runner().checkEnumFiles(root)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "read the package in kinds")
	})

	t.Run("a file that doesn't parse", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		writeKinds(t, g.dir, "kinds.go")
		testutil.WriteFile(t, g.path("kinds/broken.go"), "package kinds\n\nconst (\n")

		root := t.TempDir()
		models(t, root, enumModule+"/kinds", map[string][]string{"Kind": {"KindA"}})

		err := g.runner().checkEnumFiles(root)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "read the constants in kinds")
	})

	t.Run("unreadable bindings", func(t *testing.T) {
		t.Parallel()

		skipAsRoot(t)

		root := t.TempDir()
		testutil.WriteFile(t, filepath.Join(root, modelsFile), "")
		require.NoError(t, os.Chmod(filepath.Join(root, modelsFile), 0o000))

		require.Error(t, newRig(t).runner().checkEnumFiles(root))
	})
}

func TestBoundEnums(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"acme/theme/models.ts": unionEnums([]byte(generatedEnum)),
		"acme/theme/other.ts":  []byte("export const Skipped = {\n    A: 1,\n} as const;\n"),
		"acme/none/models.ts":  []byte("export interface None {\n    \"a\": string;\n}\n"),
	}

	assert.Equal(t, map[string]map[string][]string{
		"acme/theme": {"Theme": {"ThemeSystem", "ThemeLight", "DefaultTheme"}},
	}, boundEnums(files))
}

func TestModulePath(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, goMod, want string
	}{
		{"plain", "module example.com/acme\n\ngo 1.26\n", "example.com/acme"},
		{"after a comment", "// The acme module.\nmodule example.com/acme // its path\n", "example.com/acme"},
		{"quoted", "module \"example.com/acme\"\n", "example.com/acme"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			file := filepath.Join(t.TempDir(), goModFile)
			testutil.WriteFile(t, file, tc.goMod)

			got, err := modulePath(file)

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}

	t.Run("no module line", func(t *testing.T) {
		t.Parallel()

		file := filepath.Join(t.TempDir(), goModFile)
		testutil.WriteFile(t, file, "go 1.26\n")

		_, err := modulePath(file)

		require.ErrorIs(t, err, errNoModule)
	})
}

func TestModuleDir(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		pkg  string
		dir  string
		ours bool
	}{
		{enumModule, ".", true},
		{enumModule + "/internal/kinds", filepath.Join("internal", "kinds"), true},
		{enumModule + "x/kinds", "", false},
		{"example.org/other", "", false},
	} {
		dir, ours := moduleDir(enumModule, tc.pkg)

		assert.Equal(t, tc.ours, ours, tc.pkg)

		if tc.ours {
			assert.Equal(t, tc.dir, dir, tc.pkg)
		}
	}
}
