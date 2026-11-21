package manifests

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// maxWorkspaces is how many of a package's workspace patterns are kept.
const maxWorkspaces = 100

// errNotObject is a package.json that isn't a JSON object.
var errNotObject = errors.New("not a JSON object")

// packageJSON is what Deps reads of a package.json: its name, the manager its
// packageManager field names, its workspaces' patterns, and how many dependencies of
// each kind it names.
type packageJSON struct {
	name         string
	manager      Tool
	workspaces   []string
	deps         int
	devDeps      int
	peerDeps     int
	optionalDeps int
}

// rawPackage is the fields of a package.json Deps reads, each kept raw, so one of a type
// it doesn't expect leaves the rest readable.
type rawPackage struct {
	Name                 json.RawMessage `json:"name"`
	PackageManager       json.RawMessage `json:"packageManager"`
	Workspaces           json.RawMessage `json:"workspaces"`
	Dependencies         json.RawMessage `json:"dependencies"`
	DevDependencies      json.RawMessage `json:"devDependencies"`
	PeerDependencies     json.RawMessage `json:"peerDependencies"`
	OptionalDependencies json.RawMessage `json:"optionalDependencies"`
}

// rawWorkspaces is the object form of a package.json's workspaces, Yarn's, whose patterns
// are its packages.
type rawWorkspaces struct {
	Packages json.RawMessage `json:"packages"`
}

// utf8BOM is the byte order mark an editor may start a package.json with, which npm reads
// past.
const utf8BOM = "\ufeff"

// parsePackageJSON reads a package.json with encoding/json: its name, the manager its
// packageManager field names ("pnpm@9.1.0" is pnpm), its workspaces, as an array of
// patterns or an object of packages, and how many dependencies, dev dependencies, peer
// dependencies, and optional dependencies it names: counts, never the names themselves.
// A field of another type than npm's reads as unset. Bytes that aren't one JSON object
// fail with errNotObject.
func parsePackageJSON(data []byte) (packageJSON, error) {
	data = bytes.TrimPrefix(data, []byte(utf8BOM))
	if !bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) {
		return packageJSON{}, errNotObject
	}

	var raw rawPackage
	if err := json.Unmarshal(data, &raw); err != nil {
		return packageJSON{}, fmt.Errorf("%w: %w", errNotObject, err)
	}

	return packageJSON{
		name:         jsonString(raw.Name),
		manager:      managerOf(jsonString(raw.PackageManager)),
		workspaces:   workspacePatterns(raw.Workspaces),
		deps:         countKeys(raw.Dependencies),
		devDeps:      countKeys(raw.DevDependencies),
		peerDeps:     countKeys(raw.PeerDependencies),
		optionalDeps: countKeys(raw.OptionalDependencies),
	}, nil
}

// managerOf returns the manager a packageManager field names, as in "pnpm@9.1.0" or
// "yarn@4.5.0+sha512.…", or ToolNone for one it doesn't know.
func managerOf(field string) Tool {
	name, _, _ := strings.Cut(strings.TrimSpace(field), "@")

	for _, t := range []Tool{ToolPNPM, ToolNPM, ToolYarn, ToolBun} {
		if name == string(t) {
			return t
		}
	}

	return ToolNone
}

// workspacePatterns returns a package's workspace patterns, from the array form or the
// object form's packages, the strings alone, at most maxWorkspaces of them; never nil.
func workspacePatterns(raw json.RawMessage) []string {
	var object rawWorkspaces
	if err := json.Unmarshal(raw, &object); err == nil && object.Packages != nil {
		raw = object.Packages
	}

	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return []string{}
	}

	out := make([]string, 0, min(len(items), maxWorkspaces))

	for _, item := range items {
		if pattern := jsonString(item); pattern != "" && len(out) < maxWorkspaces {
			out = append(out, pattern)
		}
	}

	return out
}

// jsonString returns raw as a string, or "" when it is no JSON string.
func jsonString(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}

	return s
}

// countKeys returns how many keys raw holds, or 0 when it is no JSON object.
func countKeys(raw json.RawMessage) int {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return 0
	}

	return len(object)
}
