package agent

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
)

// check returns every way v breaks schema, each with the path it was found at.
func (c schemaChecker) check(schema map[string]any, v any, path string) []string {
	if ref, ok := schema["$ref"].(string); ok {
		return c.checkRef(ref, v, path)
	}

	if anyOf, ok := schema["anyOf"].([]any); ok {
		return c.checkAnyOf(anyOf, v, path)
	}

	if problems := checkType(schema, v, path); len(problems) > 0 {
		return problems
	}

	problems := checkValue(schema, v, path)

	switch val := v.(type) {
	case map[string]any:
		problems = append(problems, c.checkObject(schema, val, path)...)
	case []any:
		problems = append(problems, c.checkArray(schema, val, path)...)
	}

	return problems
}

// checkRef checks v against the definition ref names.
func (c schemaChecker) checkRef(ref string, v any, path string) []string {
	def, found := c.defs[strings.TrimPrefix(ref, "#/$defs/")].(map[string]any)
	if !found {
		return []string{path + ": unknown $ref " + ref}
	}

	return c.check(def, v, path)
}

// checkAnyOf checks that v matches one of the choices.
func (c schemaChecker) checkAnyOf(anyOf []any, v any, path string) []string {
	all := []string{path + ": matches no anyOf choice"}

	for _, alt := range anyOf {
		problems := c.check(alt.(map[string]any), v, path)
		if len(problems) == 0 {
			return nil
		}

		all = append(all, problems...)
	}

	return all
}

// typeOf names a JSON value's type as a schema does.
func typeOf(v any, integerWanted bool) string {
	switch val := v.(type) {
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case bool:
		return "boolean"
	case float64:
		if integerWanted && val == math.Trunc(val) {
			return "integer"
		}

		return "number"
	default:
		return "null"
	}
}

// checkType checks the type keyword.
func checkType(schema map[string]any, v any, path string) []string {
	want, ok := schema["type"].(string)
	if !ok {
		return nil
	}

	if got := typeOf(v, want == "integer"); got != want {
		return []string{fmt.Sprintf("%s: is %s, the schema says %s", path, got, want)}
	}

	return nil
}

// checkValue checks enum, const, and the string and number bounds.
func checkValue(schema map[string]any, v any, path string) []string {
	var problems []string

	if enum, ok := schema["enum"].([]any); ok && !slices.Contains(enum, v) {
		problems = append(problems, fmt.Sprintf("%s: %v isn't one of %v", path, v, enum))
	}

	if want, ok := schema["const"]; ok && want != v {
		problems = append(problems, fmt.Sprintf("%s: %v isn't %v", path, v, want))
	}

	switch val := v.(type) {
	case string:
		problems = append(problems, checkString(schema, val, path)...)
	case float64:
		problems = append(problems, checkNumber(schema, val, path)...)
	}

	return problems
}

// checkString checks a string's length and pattern.
func checkString(schema map[string]any, s, path string) []string {
	var problems []string

	if n, has := schema["minLength"].(float64); has && float64(len(s)) < n {
		problems = append(problems, fmt.Sprintf("%s: %q is shorter than %v", path, s, n))
	}

	if p, has := schema["pattern"].(string); has && !regexp.MustCompile(p).MatchString(s) {
		problems = append(problems, fmt.Sprintf("%s: %q doesn't match the schema's pattern", path, s))
	}

	return problems
}

// checkNumber checks a number's bounds.
func checkNumber(schema map[string]any, n float64, path string) []string {
	var problems []string

	bounds := []struct {
		key  string
		bad  func(n, bound float64) bool
		word string
	}{
		{"minimum", func(n, b float64) bool { return n < b }, "is under"},
		{"maximum", func(n, b float64) bool { return n > b }, "is over"},
		{"exclusiveMinimum", func(n, b float64) bool { return n <= b }, "isn't over"},
	}

	for _, b := range bounds {
		if bound, has := schema[b.key].(float64); has && b.bad(n, bound) {
			problems = append(problems, fmt.Sprintf("%s: %v %s %v", path, n, b.word, bound))
		}
	}

	return problems
}

// checkObject checks an object's required keys and each of its keys.
func (c schemaChecker) checkObject(schema, obj map[string]any, path string) []string {
	var problems []string

	required, _ := schema["required"].([]any)
	for _, key := range required {
		if _, has := obj[key.(string)]; !has {
			problems = append(problems, fmt.Sprintf("%s: lacks %v", path, key))
		}
	}

	for key, val := range obj {
		problems = append(problems, c.checkKey(schema, key, val, path)...)
	}

	return problems
}

// checkKey checks one key of an object: by its property's schema, or as an additional one.
func (c schemaChecker) checkKey(schema map[string]any, key string, val any, path string) []string {
	props, _ := schema["properties"].(map[string]any)

	if sub, known := props[key].(map[string]any); known {
		return c.check(sub, val, path+"."+key)
	}

	switch addl := schema["additionalProperties"].(type) {
	case bool:
		if !addl {
			return []string{fmt.Sprintf("%s: %q isn't a key the schema knows", path, key)}
		}
	case map[string]any:
		return c.check(addl, val, path+"."+key)
	}

	return nil
}

// checkArray checks an array's items and their uniqueness.
func (c schemaChecker) checkArray(schema map[string]any, arr []any, path string) []string {
	var problems []string

	if items, ok := schema["items"].(map[string]any); ok {
		for i, item := range arr {
			problems = append(problems, c.check(items, item, fmt.Sprintf("%s[%d]", path, i))...)
		}
	}

	if unique, _ := schema["uniqueItems"].(bool); unique {
		problems = append(problems, repeated(arr, path)...)
	}

	return problems
}

// repeated returns a problem for each item that appears again.
func repeated(arr []any, path string) []string {
	var problems []string

	seen := map[string]bool{}

	for _, item := range arr {
		key := fmt.Sprint(item)
		if seen[key] {
			problems = append(problems, fmt.Sprintf("%s: %v appears twice", path, item))
		}

		seen[key] = true
	}

	return problems
}
