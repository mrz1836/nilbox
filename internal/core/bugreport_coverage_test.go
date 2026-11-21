package core

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/appinfo"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/integrations"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/settings"
)

// bugReportLeavesOut are the settings a bug report doesn't word, on purpose, each with
// the reason. A key names a field of settings.Settings by its path, and a struct's path
// leaves out every field under it. TestABugReportWordsEverySetting fails on any other
// setting the report doesn't word, and on a key that names no field.
func bugReportLeavesOut() map[string]string {
	return map[string]string{
		// Catalogs, the same in every build.
		"SensitiveCategories": "the built-in categories of sensitive files, the same in every build",
		"StatusProviders":     "the status pages the check can watch, the same in every build",
		"RefreshChoices":      "the intervals each refresh can be set to, the same in every build",
		"DefaultWindowSize":   "the size a fresh database opens the window at, the same in every build",

		// What the window remembers of how it was last left, which no bug turns on.
		"ReviewTab":                "the review sheet's tab chosen last",
		"NotificationsTab":         "the Notifications page's tab chosen last",
		"ProjectsTipDismissed":     "whether the inbox's tip about projects was put away",
		"Integrations.Announced":   "whether the Scripts tab was announced once",
		"Integrations.LastPage":    "the Scripts tab's page shown last",
		"Integrations.Sections":    "the Scripts tab's sections opened or folded",
		"Integrations.ShowRuns":    "whether Repo scripts shows a run beside its scripts at once",
		"Integrations.ReleaseSort": "the order the Release page was last sorted in",
		"Integrations.DepsSort":    "the order the Deps page was last sorted in",

		// Private: a path of the user's own.
		"Integrations.Paths":    "each tool's chosen copy, a path, which the tools' lines tell as \"a copy the user chose\"",
		"Integrations.LastRepo": "the clone Repo scripts showed last, a path",

		// Worded from another source, which tells what the setting does there.
		"UpdateMode": "the Updates section words it from update.Status.Mode",
		"Integrations.Switches": "each tool's line words its switch from integrations.Tool.Switch; Scripts' own is " +
			"asserted in TestABugReportStatesTheSettingsAndIntegrationsWithNothingPersonal",
	}
}

// settingsLeaf is one setting: a field of settings.Settings that isn't a struct, by its
// path and its index for reflect.Value.FieldByIndex.
type settingsLeaf struct {
	path  string
	index []int
}

// settingsFields walks t's exported fields, recursing into structs: it returns every leaf,
// and every path, a struct's among them, for the check of bugReportLeavesOut's keys.
func settingsFields(t reflect.Type, prefix string, index []int, paths map[string]bool) []settingsLeaf {
	var out []settingsLeaf

	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}

		path, at := prefix+f.Name, slices.Concat(index, []int{i})
		paths[path] = true

		if f.Type.Kind() == reflect.Struct {
			out = append(out, settingsFields(f.Type, path+".", at, paths)...)

			continue
		}

		out = append(out, settingsLeaf{path: path, index: at})
	}

	return out
}

// leftOut reports whether path, or a struct it is under, is in leaves.
func leftOut(leaves map[string]string, path string) bool {
	for p := path; ; {
		if _, ok := leaves[p]; ok {
			return true
		}

		i := strings.LastIndexByte(p, '.')
		if i < 0 {
			return false
		}

		p = p[:i]
	}
}

// changeSetting changes v, a copy's setting, from what it holds: a switch flipped, a number
// one more, a word with "x" after it, a list one zero value longer, and a map with a
// zero key added, each into a fresh slice or map, so the baseline's own is left as it is.
// It returns false for a kind it doesn't know how to change, such as a pointer.
func changeSetting(v reflect.Value) bool {
	switch kind := v.Kind(); {
	case kind == reflect.Bool:
		v.SetBool(!v.Bool())
	case v.CanInt():
		v.SetInt(v.Int() + 1)
	case kind == reflect.String:
		v.SetString(v.String() + "x")
	case kind == reflect.Slice:
		fresh := reflect.MakeSlice(v.Type(), v.Len(), v.Len()+1)
		reflect.Copy(fresh, v)
		v.Set(reflect.Append(fresh, reflect.Zero(v.Type().Elem())))
	case kind == reflect.Map:
		fresh := reflect.MakeMapWithSize(v.Type(), v.Len()+1)
		for it := v.MapRange(); it.Next(); {
			fresh.SetMapIndex(it.Key(), it.Value())
		}

		fresh.SetMapIndex(reflect.Zero(v.Type().Key()), reflect.Zero(v.Type().Elem()))
		v.Set(fresh)
	default:
		return false
	}

	return true
}

// renderedSettings is every line a bug report words from s: the Settings section,
// Autopilot's settings, and the integrations' lines.
func renderedSettings(s settings.Settings) string {
	return joined([]appinfo.StateSection{
		settingsSection(s, otherSettings{}),
		{Title: "Autopilot", Lines: []string{autopilotSettingsLine(s.Autopilot)}},
		integrationsSection(integrations.Tools{}, s.Integrations),
	})
}

func TestABugReportWordsEverySetting(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, testOptions(t, ".claude"))

	baseline, err := c.Settings.Get(t.Context())
	require.NoError(t, err)

	// A cap, so its unit shows.
	baseline.Autopilot.DailyCap = autopilotprefs.DailyCap{Amount: 5, Unit: autopilotprefs.UnitUSD}
	before := renderedSettings(baseline)

	leaves, paths := bugReportLeavesOut(), map[string]bool{}

	for _, leaf := range settingsFields(reflect.TypeFor[settings.Settings](), "", nil, paths) {
		if leftOut(leaves, leaf.path) {
			continue
		}

		changed := baseline
		if !changeSetting(reflect.ValueOf(&changed).Elem().FieldByIndex(leaf.index)) {
			t.Errorf("settings.%s is a %s, which the test can't change: handle the kind in changeSetting, "+
				"then word the setting in bugreport_settings.go", leaf.path,
				reflect.ValueOf(changed).FieldByIndex(leaf.index).Kind())

			continue
		}

		assert.NotEqual(t, before, renderedSettings(changed),
			"settings.%s changed, and the bug report didn't: word it in internal/core/bugreport_settings.go, "+
				"or add it to bugReportLeavesOut with the reason it stays out", leaf.path)
	}

	for path := range leaves {
		assert.True(t, paths[path], "bugReportLeavesOut names settings.%s, which is no longer a setting", path)
	}
}

func TestABugReportWordsEveryDefaultPrompt(t *testing.T) {
	t.Parallel()

	library := map[int64]prompts.Prompt{7: {ID: 7, StarterKey: "review"}}
	words := func(d prompts.DefaultPrompts) string {
		return joined([]appinfo.StateSection{{
			Title: "Prompts", Lines: append(replyParts(d, library), launchPromptParts(d, library)...),
		}})
	}

	var d prompts.DefaultPrompts

	before := words(d)

	var walk func(v reflect.Value, path string)

	walk = func(v reflect.Value, path string) {
		for i := range v.NumField() {
			f, name := v.Field(i), path+v.Type().Field(i).Name

			switch {
			case f.Kind() == reflect.Struct:
				walk(f, name+".")
			case f.CanInt():
				f.SetInt(7)
				assert.NotEqual(t, before, words(d),
					"prompts.DefaultPrompts.%s isn't in the bug report: word it in replyParts or launchPromptParts", name)
				f.SetInt(0)
			default:
				t.Errorf("prompts.DefaultPrompts.%s is a %s, which the test can't change", name, f.Kind())
			}
		}
	}

	walk(reflect.ValueOf(&d).Elem(), "")
}
