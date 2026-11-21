package kit_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/kit"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
)

// TestSchemaGolden pins the proposal's JSON Schema.
func TestSchemaGolden(t *testing.T) {
	t.Parallel()

	wordsGolden(t, "schema.golden.json", string(kit.Schema()))
}

// TestTheSchemaIsTheSameOnEveryCall holds the schema to one output.
func TestTheSchemaIsTheSameOnEveryCall(t *testing.T) {
	t.Parallel()

	first, second := kit.Schema(), kit.Schema()
	assert.Equal(t, first, second)
	assert.True(t, json.Valid(first))
}

// wordsSchema reads the proposal's schema.
func wordsSchema(t *testing.T) map[string]any {
	t.Helper()

	var schema map[string]any
	require.NoError(t, json.Unmarshal(kit.Schema(), &schema))
	assert.Equal(t, "https://json-schema.org/draft/2020-12/schema", schema["$schema"])

	return schema
}

// wordsDef returns the schema's definition of the struct type typ.
func wordsDef(t *testing.T, schema map[string]any, typ reflect.Type) map[string]any {
	t.Helper()

	defs, ok := schema["$defs"].(map[string]any)
	require.True(t, ok)

	def, ok := defs[typ.Name()].(map[string]any)
	require.True(t, ok, "the schema has no definition of %s", typ)

	return def
}

// wordsSets are the values of every closed set a proposal holds, by the type that holds
// it: the spec's, a new prompt's, and Autopilot's settings'.
func wordsSets() map[reflect.Type][]string {
	out := map[reflect.Type][]string{
		reflect.TypeFor[kit.Purpose]():     {string(kit.PurposeAgent), string(kit.PurposeReply)},
		reflect.TypeFor[kit.ReplyAction](): {string(kit.ReplyApprove), string(kit.ReplyRequestChanges), string(kit.ReplyComment)},
		reflect.TypeFor[kit.AppliesTo]():   {string(kit.AppliesToPR), string(kit.AppliesToIssue), string(kit.AppliesToAny)},
	}

	for _, m := range autopilotprefs.Modes() {
		out[reflect.TypeFor[autopilotprefs.Mode]()] = append(out[reflect.TypeFor[autopilotprefs.Mode]()], string(m))
	}

	for _, u := range autopilotprefs.Units() {
		out[reflect.TypeFor[autopilotprefs.Unit]()] = append(out[reflect.TypeFor[autopilotprefs.Unit]()], string(u))
	}

	for _, set := range spec.ClosedSets() {
		out[set.Type] = set.Values
	}

	return out
}

// TestTheSchemaEnumeratesEveryClosedSet walks the proposal's types as encoding/json does
// and holds every key of a named string type to an enum of exactly its closed set's
// values, so a value the catalog adds is in the schema, and a named type without a closed
// set fails here.
func TestTheSchemaEnumeratesEveryClosedSet(t *testing.T) {
	t.Parallel()

	schema := wordsSchema(t)
	enums := 0

	for _, typ := range wordsStructs(reflect.TypeFor[kit.Proposal]()) {
		props, _ := wordsDef(t, schema, typ)["properties"].(map[string]any)

		for field := range typ.Fields() {
			if wordsCheckEnum(t, typ, field, props) {
				enums++
			}
		}
	}

	assert.Greater(t, enums, 20, "the walk reached the format's closed sets")
}

// wordsCheckEnum holds one key of the struct type typ to the schema's properties of it: a
// key of a named string type, or a list of one, to an enum of exactly its closed set's
// values. It reports whether the key holds a closed set.
func wordsCheckEnum(t *testing.T, typ reflect.Type, f reflect.StructField, props map[string]any) bool {
	t.Helper()

	key, _, _ := strings.Cut(f.Tag.Get("json"), ",")
	prop, ok := props[key].(map[string]any)
	require.True(t, ok, "%s.%s isn't in the schema", typ.Name(), key)

	inner, list := wordsElem(f.Type)
	if inner.Kind() != reflect.String || inner.PkgPath() == "" {
		return false
	}

	values, ok := wordsSets()[inner]
	require.True(t, ok, "%s.%s is a %s, which has no closed set", typ.Name(), key, inner)

	if list {
		prop, _ = prop["items"].(map[string]any)
	}

	assert.Equal(t, wordsAnyList(values), prop["enum"], "%s.%s", typ.Name(), key)

	return true
}

// wordsAnyList is values as encoding/json reads a list of strings.
func wordsAnyList(values []string) []any {
	out := make([]any, 0, len(values))
	for _, v := range values {
		out = append(out, v)
	}

	return out
}

// TestTheSchemaRequiresWhatEncodingJSONAlwaysWrites holds every definition to its type:
// a key is required exactly when it has no omitempty, and no other key is allowed.
func TestTheSchemaRequiresWhatEncodingJSONAlwaysWrites(t *testing.T) {
	t.Parallel()

	schema := wordsSchema(t)

	defs, ok := schema["$defs"].(map[string]any)
	require.True(t, ok)

	for name, raw := range defs {
		def, ok := raw.(map[string]any)
		require.True(t, ok, name)
		assert.Equal(t, false, def["additionalProperties"], name)
		assert.Equal(t, "object", def["type"], name)
	}

	def := wordsDef(t, schema, reflect.TypeFor[kit.RoutineChange]())
	assert.Equal(t, []any{"name", "spec"}, def["required"])

	def = wordsDef(t, schema, reflect.TypeFor[spec.Spec]())
	assert.Equal(t, []any{"version", "watch", "trigger", "conditions", "steps", "limits"}, def["required"])
	assert.NotContains(t, wordsDef(t, schema, reflect.TypeFor[kit.SettingsChange]())["properties"], "enabled")
	assert.NotContains(t, wordsDef(t, schema, reflect.TypeFor[kit.SettingsChange]())["properties"], "paused")
}

// wordsExample reads the worked example the proposal topic shows.
func wordsExample(t *testing.T) []byte {
	t.Helper()

	text, ok := kit.Topic("proposal", wordsVocabulary())
	require.True(t, ok)

	m := regexp.MustCompile("(?s)```json\n(.*?)\n```").FindStringSubmatch(text)
	require.Len(t, m, 2, "the proposal topic shows a worked example")

	return []byte(m[1])
}

// TestTheWorkedExampleIsTheFormat holds the proposal topic's example to the format: it
// reads strictly as a proposal, its routine's definition loads, it runs the prompt it
// adds, and the schema takes it.
func TestTheWorkedExampleIsTheFormat(t *testing.T) {
	t.Parallel()

	data := wordsExample(t)

	p, err := spec.DecodeExact[kit.Proposal](data, 1<<20, 32)
	require.NoError(t, err)
	require.Len(t, p.Prompts, 1)
	require.Len(t, p.Routines, 1)
	require.NotNil(t, p.Settings)
	require.NotNil(t, p.Rules)

	encoded, err := spec.Encode(p.Routines[0].Spec)
	require.NoError(t, err)

	loaded, err := spec.Load(encoded)
	require.NoError(t, err)

	agent := slices.IndexFunc(loaded.Steps, func(s spec.Step) bool { return s.Agent != nil })
	require.GreaterOrEqual(t, agent, 0)
	assert.Equal(t, p.Prompts[0].Nickname, loaded.Steps[agent].Agent.Prompt.Nickname)
	assert.LessOrEqual(t, utf8.RuneCountInString(p.Prompts[0].Nickname), spec.MaxNicknameRunes)
	assert.True(t, p.Routines[0].Mode.Valid())
	assert.Empty(t, p.Routines[0].ID)

	var doc any
	require.NoError(t, json.Unmarshal(data, &doc))
	assert.Empty(t, wordsSchemaErrors(wordsSchema(t), doc))
}

// TestTheSchemaTakesEveryTemplate holds the schema to the format: a proposal of each of
// the gallery's templates, as encoding/json writes it, is one the schema takes, and one
// with a key the format doesn't have, or a value out of its set, is not.
func TestTheSchemaTakesEveryTemplate(t *testing.T) {
	t.Parallel()

	schema := wordsSchema(t)

	for _, tmpl := range wordsVocabulary().Templates {
		p := kit.Proposal{
			Summary:  tmpl.Summary,
			Routines: []kit.RoutineChange{{Name: tmpl.Name, Template: tmpl.Key, Mode: tmpl.Suggests, Spec: tmpl.Spec}},
		}

		data, err := json.Marshal(p)
		require.NoError(t, err)

		var doc any
		require.NoError(t, json.Unmarshal(data, &doc))
		assert.Empty(t, wordsSchemaErrors(schema, doc), tmpl.Key)
	}

	for _, bad := range []string{
		`{"summary": "x", "settings": {"enabled": true}}`,
		`{"summary": "x", "settings": {"paused": true}}`,
		`{"summary": "x", "settings": {"agentsAtOnce": 9}}`,
		`{"summary": "x", "Routines": []}`,
		`{"summary": "x", "prompts": [{"name": "a", "nickname": "b", "purpose": "agent", "appliesTo": "repo", "body": "c"}]}`,
		`{"summary": "x", "routines": [{"name": "a", "mode": "fast", "spec": {}}]}`,
		`{}`,
	} {
		var doc any
		require.NoError(t, json.Unmarshal([]byte(bad), &doc))
		assert.NotEmpty(t, wordsSchemaErrors(schema, doc), bad)
	}
}

// wordsSchemaErrors checks doc against the proposal's schema, as far as the keywords the
// schema uses go, and returns each place it breaks one.
func wordsSchemaErrors(schema map[string]any, doc any) []string {
	defs, _ := schema["$defs"].(map[string]any)
	v := wordsValidator{defs: defs}
	v.check(schema, doc, "$")

	return v.errs
}

// wordsValidator checks a document against the keywords the proposal's schema uses.
type wordsValidator struct {
	defs map[string]any
	errs []string
}

func (v *wordsValidator) fail(path, format string, args ...any) {
	v.errs = append(v.errs, path+": "+fmt.Sprintf(format, args...))
}

func (v *wordsValidator) check(node map[string]any, doc any, path string) {
	if ref, ok := node["$ref"].(string); ok {
		def, _ := v.defs[strings.TrimPrefix(ref, "#/$defs/")].(map[string]any)
		v.check(def, doc, path)
	}

	if want, ok := node["type"].(string); ok && !wordsTypeIs(want, doc) {
		v.fail(path, "is not a %s", want)

		return
	}

	v.value(node, doc, path)

	switch d := doc.(type) {
	case []any:
		v.list(node, d, path)
	case map[string]any:
		v.object(node, d, path)
	}
}

// value checks a value against its enum, const, and bounds.
func (v *wordsValidator) value(node map[string]any, doc any, path string) {
	if enum, ok := node["enum"].([]any); ok && !slices.Contains(enum, doc) {
		v.fail(path, "%v is not one of %v", doc, enum)
	}

	if want, ok := node["const"]; ok && want != doc {
		v.fail(path, "%v is not %v", doc, want)
	}

	if s, ok := doc.(string); ok {
		v.bound(path, "maxLength", float64(utf8.RuneCountInString(s)), node, 1)
	}

	if n, ok := doc.(float64); ok {
		v.bound(path, "minimum", n, node, -1)
		v.bound(path, "maximum", n, node, 1)
	}
}

// bound checks n against the keyword kw of node: a maximum when sign is 1, a minimum when
// it is -1.
func (v *wordsValidator) bound(path, kw string, n float64, node map[string]any, sign float64) {
	if limit, ok := node[kw].(float64); ok && sign*n > sign*limit {
		v.fail(path, "%v breaks %s %v", n, kw, limit)
	}
}

func (v *wordsValidator) list(node map[string]any, list []any, path string) {
	v.bound(path, "minItems", float64(len(list)), node, -1)
	v.bound(path, "maxItems", float64(len(list)), node, 1)

	if items, ok := node["items"].(map[string]any); ok {
		for i, item := range list {
			v.check(items, item, fmt.Sprintf("%s[%d]", path, i))
		}
	}
}

func (v *wordsValidator) object(node, obj map[string]any, path string) {
	props, _ := node["properties"].(map[string]any)

	for key, value := range obj {
		if prop, ok := props[key].(map[string]any); ok {
			v.check(prop, value, path+"."+key)
		} else if node["additionalProperties"] == false {
			v.fail(path, "has the key %q, which the format doesn't", key)
		}
	}

	required, _ := node["required"].([]any)
	for _, key := range required {
		if _, ok := obj[fmt.Sprint(key)]; !ok {
			v.fail(path, "lacks %q", key)
		}
	}

	v.bound(path, "minProperties", float64(len(obj)), node, -1)
	v.bound(path, "maxProperties", float64(len(obj)), node, 1)
}

// wordsTypeIs reports whether a JSON value, as encoding/json reads one, is of a schema's
// type.
func wordsTypeIs(want string, doc any) bool {
	switch want {
	case "object":
		_, ok := doc.(map[string]any)

		return ok
	case "array":
		_, ok := doc.([]any)

		return ok
	case "string":
		_, ok := doc.(string)

		return ok
	case "boolean":
		_, ok := doc.(bool)

		return ok
	case "integer":
		n, ok := doc.(float64)

		return ok && n == float64(int64(n))
	case "number":
		_, ok := doc.(float64)

		return ok
	}

	return false
}
