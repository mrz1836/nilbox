package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// ErrInvalidSchema is returned for a report's schema the strict form can't be made of.
var ErrInvalidSchema = errors.New("invalid report schema")

// StrictSchema returns a report's JSON schema in the strict form an agent that keeps to
// its schema strictly needs (Codex's --output-schema, as S6 found on codex-cli 0.163.0,
// which refused the draft schema): every object closed to other properties
// (additionalProperties false) with every property required, a property the schema left
// optional nullable instead, and an enum given the type of its values. The schema is one
// object; what it says otherwise is kept as it is. The result is on one line, its keys
// sorted.
func StrictSchema(schema string) (string, error) {
	var root map[string]any

	if err := json.Unmarshal([]byte(schema), &root); err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidSchema, err)
	}

	if root["type"] != "object" {
		return "", fmt.Errorf("%w: it isn't an object's", ErrInvalidSchema)
	}

	strictObject(root)

	out, err := json.Marshal(root)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidSchema, err)
	}

	return string(out), nil
}

// strictObject makes an object schema strict, and each of its properties.
func strictObject(obj map[string]any) {
	props, _ := obj["properties"].(map[string]any)
	required := requiredOf(obj)

	names := make([]string, 0, len(props))

	for name, raw := range props {
		names = append(names, name)

		prop, ok := raw.(map[string]any)
		if !ok {
			continue
		}

		strictProperty(prop)

		if !required[name] {
			nullable(prop)
		}
	}

	slices.Sort(names)

	all := make([]any, 0, len(names))
	for _, name := range names {
		all = append(all, name)
	}

	obj["required"], obj["additionalProperties"] = all, false
}

// requiredOf returns the properties an object schema requires.
func requiredOf(obj map[string]any) map[string]bool {
	required := map[string]bool{}

	list, _ := obj["required"].([]any)
	for _, r := range list {
		if name, ok := r.(string); ok {
			required[name] = true
		}
	}

	return required
}

// strictProperty gives an enum the type of its values, and makes an object property
// strict.
func strictProperty(prop map[string]any) {
	if _, typed := prop["type"]; !typed {
		if values, ok := prop["enum"].([]any); ok && len(values) > 0 && allStrings(values) {
			prop["type"] = "string"
		}
	}

	if prop["type"] == "object" {
		strictObject(prop)
	}
}

// nullable lets a property be null too: its type, or each of its types, with "null".
func nullable(prop map[string]any) {
	switch t := prop["type"].(type) {
	case string:
		if t != "null" {
			prop["type"] = []any{t, "null"}
		}
	case []any:
		if !slices.Contains(t, any("null")) {
			prop["type"] = append(t, "null")
		}
	default:
		inner := map[string]any{}
		for k, v := range prop {
			inner[k] = v
			delete(prop, k)
		}

		prop["anyOf"] = []any{inner, map[string]any{"type": "null"}}
	}

	if values, ok := prop["enum"].([]any); ok && !slices.Contains(values, any(nil)) {
		prop["enum"] = append(values, nil)
	}
}

// allStrings reports a list of strings alone.
func allStrings(values []any) bool {
	for _, v := range values {
		if _, ok := v.(string); !ok {
			return false
		}
	}

	return true
}

// Background is what an agent can do in the background, as the routine builder shows it:
// its headless capabilities, and whether it keeps each tools preset, or why not.
type Background struct {
	Headless

	// Presets are the tools presets, from the narrowest, each kept or not.
	Presets []PresetKeep `json:"presets"`
}

// backgroundOf returns the agent with id's Background.
func backgroundOf(id ID) Background {
	h, err := HeadlessOf(id)
	if err != nil {
		return Background{Presets: []PresetKeep{}}
	}

	return Background{Headless: h, Presets: Presets(id)}
}
