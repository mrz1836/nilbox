package describeit

import (
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/kit"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// nullLists returns the path of every nil list or map in v the JSON encoding would write
// as null, as core's bound test finds them: a nil one in a field tagged omitempty is left
// out, so no null.
func nullLists(v reflect.Value, path string) []string {
	kind := v.Kind()

	switch {
	case (kind == reflect.Pointer || kind == reflect.Interface) && !v.IsNil():
		return nullLists(v.Elem(), path)
	case (kind == reflect.Slice || kind == reflect.Map) && v.IsNil():
		return []string{path}
	case kind == reflect.Struct:
		return nullFields(v, path)
	case kind == reflect.Slice:
		return nullElems(v, path)
	default:
		return nil
	}
}

// nullElems returns nullLists for each element of the list v, a byte list aside.
func nullElems(v reflect.Value, path string) []string {
	if v.Type().Elem().Kind() == reflect.Uint8 {
		return nil
	}

	var out []string
	for i := range v.Len() {
		out = append(out, nullLists(v.Index(i), path+"["+strconv.Itoa(i)+"]")...)
	}

	return out
}

// nullFields returns nullLists for each field of the struct v the encoding writes.
func nullFields(v reflect.Value, path string) []string {
	var out []string

	for i := range v.NumField() {
		f, value := v.Type().Field(i), v.Field(i)
		name, opts, _ := strings.Cut(f.Tag.Get("json"), ",")

		omitted := slices.Contains(strings.Split(opts, ","), "omitempty") &&
			(value.Kind() == reflect.Slice || value.Kind() == reflect.Map) && value.IsNil()
		if f.IsExported() && name != "-" && !omitted {
			out = append(out, nullLists(value, path+"."+f.Name)...)
		}
	}

	return out
}

// TestBoundResultsHoldNoNullLists calls every bound method that returns a list, with a
// conversation, its cards, and cards from outside to show, as the window does: core's own
// bound test calls them on an empty service.
func TestBoundResultsHoldNoNullLists(t *testing.T) {
	t.Parallel()

	r, _ := pluginRig(t, map[string]string{"plugin list": pluginFixture(t, "list-installed.json")})
	id, _, _ := firstTurn(t, r, Message{Text: pinText, Attach: []AttachRef{{Kind: AttachRoutine, ID: 7}}})

	p := fullProposal()
	p.Routines[0].Spec.Steps = nil
	p.Routines[0].Spec.Conditions.Of = nil
	r.writeProposal(Outside, testNow, 0, p)

	results := map[string]any{}

	conv, err := r.svc.Conversation(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, conv.Turns[0].Cards)

	results["Conversation"] = conv

	cards, err := r.svc.Proposals(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, cards)

	results["Proposals"] = cards

	page, err := r.svc.Events(t.Context(), id, 1, 0)
	require.NoError(t, err)

	results["Events"] = page

	states, err := r.svc.Plugins(t.Context())
	require.NoError(t, err)

	results["Plugins"] = states

	r.claude(testutil.FakeRun{Stdout: streamFixture(t, "turn-resumed.jsonl", 0)})

	sent, err := r.svc.Send(t.Context(), Message{Text: "Skip drafts."})
	require.NoError(t, err)
	r.waitTurn()

	results["Send"] = sent

	fresh, err := r.svc.NewConversation(t.Context(), AgentChoice{})
	require.NoError(t, err)

	results["NewConversation"] = fresh

	empty, err := New(Config{Dir: t.TempDir()}).Conversation(t.Context())
	require.NoError(t, err)

	results["Conversation, none"] = empty

	for name, result := range results {
		assert.Empty(t, nullLists(reflect.ValueOf(result), name), "a list or map the window reads is null")
	}

	assert.Empty(t, nullLists(reflect.ValueOf(kit.Proposal{Summary: "x"}), "a proposal"),
		"a proposal's lists left out are no null")
}
