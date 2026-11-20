package describeit

import (
	"reflect"
	"slices"
	"strings"
)

// fillLists makes every nil list or map v points at hold nothing instead, wherever the
// JSON encoding would write it as null: in v and every value it holds, a field tagged
// omitempty aside, which the encoding leaves out. A card holds the agent's proposal as it
// wrote it, whose definitions may leave a list out, and the window gets [] for each, never
// null. A byte list is left alone: the encoding writes one as text, and json.RawMessage
// holds JSON.
func fillLists(v any) {
	fill(reflect.ValueOf(v))
}

// fill fills the lists of the value v.
func fill(v reflect.Value) {
	kind := v.Kind()

	if (kind == reflect.Pointer || kind == reflect.Interface) && !v.IsNil() {
		fill(v.Elem())

		return
	}

	if kind == reflect.Struct {
		for i := range v.NumField() {
			fillField(v.Type().Field(i), v.Field(i))
		}

		return
	}

	if kind == reflect.Slice {
		for i := range v.Len() {
			fill(v.Index(i))
		}
	}
}

// fillField fills the lists of the field f, whose value is value, when the encoding
// writes it.
func fillField(f reflect.StructField, value reflect.Value) {
	name, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
	if (!f.IsExported() && !f.Anonymous) || (name == "-" && opts == "") {
		return
	}

	if nilList(value) && value.CanSet() && !slices.Contains(strings.Split(opts, ","), "omitempty") {
		if value.Kind() == reflect.Map {
			value.Set(reflect.MakeMap(value.Type()))
		} else {
			value.Set(reflect.MakeSlice(value.Type(), 0, 0))
		}
	}

	fill(value)
}

// nilList reports a nil list or map, a byte list aside.
func nilList(v reflect.Value) bool {
	if v.Kind() == reflect.Map {
		return v.IsNil()
	}

	return v.Kind() == reflect.Slice && v.Type().Elem().Kind() != reflect.Uint8 && v.IsNil()
}
