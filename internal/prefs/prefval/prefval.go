// Package prefval reads and writes the two shapes of value most preferences hold: a
// switch, stored as a boolean, and a list, stored as a JSON array of strings. Settings and
// the leaves that declare its keys (trust, sensitive, notifyprefs, branchrules, and
// integrationprefs) share it, so every switch and list is spelled one way.
//
// It is a leaf: it imports only the standard library, never the store or a service.
package prefval

import (
	"encoding/json"
	"strconv"
)

// Bool reads a stored switch: as stored, when it reads as a boolean (strconv.ParseBool's
// spellings: 1, t, T, TRUE, true, True, and 0, f, F, FALSE, false, False), else fallback,
// so a switch never stored ("") or holding anything else keeps its default.
func Bool(stored string, fallback bool) bool {
	if on, err := strconv.ParseBool(stored); err == nil {
		return on
	}

	return fallback
}

// EncodeBool writes a switch as Bool reads it: "true" or "false".
func EncodeBool(on bool) string {
	return strconv.FormatBool(on)
}

// List reads a list stored as a JSON array of strings. ok is false, with a nil list, when
// stored is not such an array, "" included; a stored null reads as a nil list.
func List[T ~string](stored string) (list []T, ok bool) {
	if err := json.Unmarshal([]byte(stored), &list); err != nil {
		return nil, false
	}

	return list, true
}

// EncodeList writes a list as a JSON array of strings, a nil list as [], never null.
func EncodeList[T ~string](list []T) string {
	if list == nil {
		list = []T{}
	}

	b, _ := json.Marshal(list) //nolint:errchkjson // a list of strings always encodes

	return string(b)
}
