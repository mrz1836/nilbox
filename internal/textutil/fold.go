package textutil

import (
	"slices"
	"strings"
)

// The helpers below treat a list of names as a set ignoring case, as GitHub matches a
// label, a login, or a team's slug: two names are the same when strings.EqualFold says
// so, which folds case simply, so the Kelvin sign is a k and the capital sharp s a sharp
// s, but the dotted capital I is no i and a sharp s is no "ss".

// ContainsFold reports whether list holds s, ignoring case. Nothing is trimmed.
func ContainsFold(list []string, s string) bool {
	return slices.ContainsFunc(list, func(o string) bool { return strings.EqualFold(o, s) })
}

// UniqueFold returns list's names, each passed through clean first (nil leaves a name as
// it is), without the ones clean leaves empty and with each name once, ignoring case: the
// first spelling is kept, in the order list gives them. It never returns nil, so the
// window gets [] for none, and it leaves list as it was.
func UniqueFold(list []string, clean func(string) string) []string {
	out := make([]string, 0, len(list))

	for _, s := range list {
		if clean != nil {
			s = clean(s)
		}

		if s != "" && !ContainsFold(out, s) {
			out = append(out, s)
		}
	}

	return out
}

// ApartFold returns the names of list that remove doesn't hold, ignoring case, in order
// and with repeats kept; nothing is trimmed. A nil list gives nil, and any other a list of
// its own, empty when remove holds every name, so list is left as it was.
func ApartFold(list, remove []string) []string {
	return slices.DeleteFunc(slices.Clone(list), func(s string) bool { return ContainsFold(remove, s) })
}
