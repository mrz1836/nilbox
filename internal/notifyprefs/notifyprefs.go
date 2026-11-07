// Package notifyprefs reads and writes which kinds of notification show a banner, the
// pref the settings page changes and the notify package obeys. It imports nothing of the
// app's, so the settings service does not depend on the notify package, nor through it
// on the inbox and the GitHub client.
package notifyprefs

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// KeyBannersOff is the prefs key listing, as a JSON array, the kinds of notification whose
// banners are off. Kinds are on unless listed, so a kind added later starts on. The
// settings package writes it.
const KeyBannersOff = "notify.bannersOff"

// Kinds of notification. They are the inbox's kinds of arrival (see inbox.Arrival).
const (
	// KindReviewRequested is a pull request that newly asks for the user's review.
	KindReviewRequested = "review_requested"

	// KindAssigned is an issue or pull request newly assigned to the user.
	KindAssigned = "assigned"

	// KindWoke is a snoozed item that new activity brought back early.
	KindWoke = "woke"
)

// ErrUnknownKind is returned for a kind of notification that is not one of Kinds.
var ErrUnknownKind = errors.New("unknown kind of notification")

// Kinds returns the kinds of notification in the order the settings list them.
func Kinds() []string {
	return []string{KindReviewRequested, KindAssigned, KindWoke}
}

// DecodeBannersOff reads the kinds whose banners are off from the stored value of
// KeyBannersOff, in the order of Kinds. A value that is unset ("") or cannot be read
// turns none off, and kinds that are not known are dropped.
func DecodeBannersOff(stored string) []string {
	var listed []string
	if json.Unmarshal([]byte(stored), &listed) != nil {
		return []string{}
	}

	off := []string{}

	for _, kind := range Kinds() {
		if slices.Contains(listed, kind) {
			off = append(off, kind)
		}
	}

	return off
}

// SetBanner turns a kind's banners on or off in off, the kinds whose banners are off, and
// returns the new list in the order of Kinds.
func SetBanner(off []string, kind string, on bool) ([]string, error) {
	if !slices.Contains(Kinds(), kind) {
		return nil, fmt.Errorf("%w %q", ErrUnknownKind, kind)
	}

	out := []string{}

	for _, k := range Kinds() {
		isOff := slices.Contains(off, k)
		if k == kind {
			isOff = !on
		}

		if isOff {
			out = append(out, k)
		}
	}

	return out, nil
}

// EncodeBannersOff writes the kinds whose banners are off as the value of KeyBannersOff.
func EncodeBannersOff(off []string) string {
	if off == nil {
		off = []string{}
	}

	b, _ := json.Marshal(off) //nolint:errchkjson // a []string always encodes

	return string(b)
}
