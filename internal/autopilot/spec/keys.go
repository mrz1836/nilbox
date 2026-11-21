package spec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
)

// Why DecodeExact refuses a document.
var (
	errTooLarge     = errors.New("larger than")
	errAfter        = errors.New("more after the document")
	errDuplicateKey = errors.New("a key is given twice")
	errUnknownKey   = errors.New("a key isn't one the format has, spelled exactly")
	errTooDeep      = errors.New("it nests too deep")

	// errMalformed stops the walk at JSON that isn't well formed, which the decoder
	// refuses with its own words.
	errMalformed = errors.New("malformed")
)

// DecodeExact reads data as a T as strictly as Decode reads a spec, at every depth, so a
// spec a T holds, in a list, a map, or behind a pointer, is held to Decode's rules. It
// walks the document beside T's type and holds every object's keys, byte for byte, to
// its struct's JSON names as encoding/json finds them, a struct embedded without a name
// lending its own, since encoding/json alone would match a key ignoring case, Unicode's
// folding included ("Trigger" for "trigger"). It refuses a key given twice, a map's too
// (which encoding/json would read as the last), a field T doesn't have, a value of the
// wrong type, anything after the document, nesting past maxDepth levels, and more than
// maxBytes. Each refusal says what is wrong, for the caller to say what it was reading,
// as Decode does with ErrUnreadable. A struct that decodes itself (UnmarshalJSON) is
// still held to its own fields' names.
func DecodeExact[T any](data []byte, maxBytes, maxDepth int) (T, error) {
	var zero T

	if len(data) > maxBytes {
		return zero, fmt.Errorf("%w %d bytes", errTooLarge, maxBytes)
	}

	if err := checkKeys(data, reflect.TypeFor[T](), maxDepth); err != nil {
		return zero, err
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	var out T
	if err := dec.Decode(&out); err != nil {
		return zero, err
	}

	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return zero, errAfter
	}

	return out, nil
}

// checkKeys refuses JSON whose keys aren't exactly those of t, the type it decodes into:
// it walks the document beside t, holding every object's keys, byte for byte, to its
// struct's JSON names, refusing a key given twice and nesting past maxDepth. JSON that
// isn't well formed is left for the decoder to refuse.
func checkKeys(data []byte, t reflect.Type, maxDepth int) error {
	w := walker{dec: json.NewDecoder(bytes.NewReader(data)), maxDepth: maxDepth}

	err := w.value(t, 0)
	if errors.Is(err, errMalformed) {
		return nil
	}

	return err
}

// walker walks a document beside the type it decodes into.
type walker struct {
	dec *json.Decoder

	// maxDepth caps how deep objects and arrays nest.
	maxDepth int
}

// value checks the next value of the document, which a field of type t holds.
func (w walker) value(t reflect.Type, depth int) error {
	tok, err := w.dec.Token()
	if err != nil {
		return errMalformed
	}

	d, ok := tok.(json.Delim)
	if !ok {
		return nil
	}

	if depth >= w.maxDepth {
		return errTooDeep
	}

	if d == '{' {
		return w.object(t, depth+1)
	}

	return w.array(t, depth+1)
}

// object checks an object's keys against those type t takes (keysOf), each once, and
// each value beside the type it decodes into.
func (w walker) object(t reflect.Type, depth int) error {
	keys := keysOf(t)
	seen := map[string]bool{}

	for w.dec.More() {
		tok, err := w.dec.Token()
		if err != nil {
			return errMalformed
		}

		key, _ := tok.(string)
		inner, known := keys.of(key)

		switch {
		case seen[key]:
			return fmt.Errorf("%w: %q", errDuplicateKey, key)
		case !known:
			return fmt.Errorf("%w: %q", errUnknownKey, key)
		}

		seen[key] = true

		if err := w.value(inner, depth); err != nil {
			return err
		}
	}

	if _, err := w.dec.Token(); err != nil {
		return errMalformed
	}

	return nil
}

// array checks each element of an array, which a slice or an array of type t holds,
// through pointers.
func (w walker) array(t reflect.Type, depth int) error {
	var elem reflect.Type
	if t = deref(t); t != nil && (t.Kind() == reflect.Slice || t.Kind() == reflect.Array) {
		elem = t.Elem()
	}

	for w.dec.More() {
		if err := w.value(elem, depth); err != nil {
			return err
		}
	}

	if _, err := w.dec.Token(); err != nil {
		return errMalformed
	}

	return nil
}

// objectKeys are the keys an object may hold, with the type each one's value decodes
// into.
type objectKeys struct {
	// fields are a struct's JSON names; nil for an object that takes any key.
	fields map[string]reflect.Type

	// elem is what any key's value decodes into: a map's element, or nil for an object
	// whose type holds no struct, which the decoder refuses as the wrong type unless it
	// takes any value.
	elem reflect.Type
}

// of returns the type key's value decodes into, and whether the object may hold the key.
func (k objectKeys) of(key string) (reflect.Type, bool) {
	if k.fields == nil {
		return k.elem, true
	}

	t, ok := k.fields[key]

	return t, ok
}

// keysOf returns the keys an object of type t may hold, through pointers: a struct's JSON
// names; any key for a map, each value decoding into its element; and any key for any
// other type.
func keysOf(t reflect.Type) objectKeys {
	if t = deref(t); t != nil && t.Kind() == reflect.Map {
		return objectKeys{elem: t.Elem()}
	}

	return objectKeys{fields: jsonFields(t)}
}

// deref returns the type t points to, through every pointer; nil for nil.
func deref(t reflect.Type) reflect.Type {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	return t
}

// jsonField is one JSON name a struct's field gives, as encoding/json reads it: the type
// its value decodes into, and whether its tag names it.
type jsonField struct {
	name   string
	typ    reflect.Type
	tagged bool
}

// jsonFields returns the JSON names of the struct t holds, through pointers, with the
// type each holds; nil for a type that holds no struct, and none for a struct with none.
// They are encoding/json's: a field's name is its tag's, else its own, and a field tagged
// "-" or unexported has none; a struct embedded without a name lends its fields as the
// struct's own, level by level. A name an upper level gives wins over a deeper one; of the
// fields one level gives a name, a lone tagged one wins over untagged ones, and otherwise
// none does.
func jsonFields(t reflect.Type) map[string]reflect.Type {
	if t = deref(t); t == nil || t.Kind() != reflect.Struct {
		return nil
	}

	out := map[string]reflect.Type{}
	settled := map[string]bool{}
	visited := map[reflect.Type]bool{}

	for level := map[reflect.Type]int{t: 1}; len(level) > 0; {
		var found map[string][]jsonField

		found, level = readLevel(level, visited)

		for name, fields := range found {
			if f, ok := dominant(fields); ok && !settled[name] {
				out[name] = f.typ
			}

			settled[name] = true
		}
	}

	return out
}

// readLevel reads one level of embedding: each struct's fields, by name, as many times as
// the level embeds the struct (twice at most, all that dominant tells apart), and the
// structs they embed without a name, for the next level, each with how often. A struct
// read at a level above is read no more.
func readLevel(level map[reflect.Type]int, visited map[reflect.Type]bool) (
	map[string][]jsonField, map[reflect.Type]int,
) {
	found := map[string][]jsonField{}
	next := map[reflect.Type]int{}

	for st, count := range level {
		if visited[st] {
			continue
		}

		visited[st] = true

		for field := range st.Fields() {
			f, embeds, ok := fieldName(field)

			switch {
			case !ok:
			case embeds != nil:
				next[embeds]++
			default:
				found[f.name] = append(found[f.name], slices.Repeat([]jsonField{f}, min(count, 2))...)
			}
		}
	}

	return found, next
}

// fieldName reads one struct field as encoding/json does: the JSON name it gives, or the
// struct it embeds without a name, whose fields it lends; ok is false for a field
// encoding/json leaves alone.
func fieldName(f reflect.StructField) (named jsonField, embeds reflect.Type, ok bool) {
	tag := f.Tag.Get("json")
	name, _, _ := strings.Cut(tag, ",")

	// encoding/json follows a pointer to the struct a field embeds, if the pointer has no
	// name of its own.
	inner := f.Type
	if inner.Kind() == reflect.Pointer && inner.Name() == "" {
		inner = inner.Elem()
	}

	switch {
	case tag == "-":
		return jsonField{}, nil, false
	case f.Anonymous && name == "" && inner.Kind() == reflect.Struct:
		return jsonField{}, inner, true
	case !f.IsExported():
		return jsonField{}, nil, false
	case name == "":
		return jsonField{name: f.Name, typ: f.Type}, nil, true
	default:
		return jsonField{name: name, typ: f.Type, tagged: true}, nil, true
	}
}

// dominant returns the field a name decodes into, of those one level gives it: the one,
// or the one tagged among untagged ones; ok is false for a name two give alike, which
// encoding/json leaves to neither.
func dominant(fields []jsonField) (jsonField, bool) {
	if len(fields) == 1 {
		return fields[0], true
	}

	tagged := slices.DeleteFunc(slices.Clone(fields), func(f jsonField) bool { return !f.tagged })
	if len(tagged) == 1 {
		return tagged[0], true
	}

	return jsonField{}, false
}
