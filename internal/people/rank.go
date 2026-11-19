package people

import (
	"cmp"
	"math"
	"slices"
	"strings"
	"time"
)

// halfLife is how long it takes for what was seen to count half as much.
const halfLife = 60 * 24 * time.Hour

// weights is how much one pull request counts toward a person's rank, by the role they had
// on it.
func weights() map[Role]float64 {
	return map[Role]float64{
		RoleReviewer: 3, RolePicked: 2.5, RoleRequested: 2, RoleAuthor: 2, RoleSuggested: 1, RoleCommitter: 1,
	}
}

// seenRow is one row of what was seen, as rank reads it.
type seenRow struct {
	key    string
	login  string
	name   string
	number int
	role   Role
	access bool
	at     time.Time
}

// tally is what rank adds up of one person.
type tally struct {
	person   Person
	eligible bool
}

// rank turns what was seen on a repository's pull requests into the people who work on it
// (eligible), the highest score first, then the latest seen, then by login. Each row is one
// pull request in one role, which counts its role's weight, halved every halfLife of its
// age at now.
func rank(rows []seenRow, now time.Time) []Person {
	w := weights()
	byKey := map[string]*tally{}

	for _, r := range rows {
		t, ok := byKey[r.key]
		if !ok {
			t = &tally{person: Person{Login: r.login, Name: r.name}}
			byKey[r.key] = t
		}

		t.add(r, w[r.role], now)
	}

	out := []Person{}

	for _, t := range byKey {
		if t.eligible {
			out = append(out, t.person)
		}
	}

	slices.SortFunc(out, func(a, b Person) int {
		return cmp.Or(
			cmp.Compare(b.Score, a.Score), b.LastAt.Compare(a.LastAt),
			strings.Compare(strings.ToLower(a.Login), strings.ToLower(b.Login)),
		)
	})

	return out
}

// add counts r, worth weight at its age at now, into t.
func (t *tally) add(r seenRow, weight float64, now time.Time) {
	t.eligible = t.eligible || r.access || vouches(r.role)
	t.person.Score += weight * math.Exp2(-float64(max(now.Sub(r.at), 0))/float64(halfLife))

	if r.at.After(t.person.LastAt) {
		t.person.LastAt = r.at
	}

	switch r.role {
	case RoleReviewer:
		t.person.Reviewed++
	case RoleAuthor:
		t.person.Authored++
	case RoleCommitter:
		t.person.Committed++
	case RoleRequested, RoleSuggested, RolePicked:
	}
}

// vouches reports a role someone was given by another: asked to review by someone with
// access, suggested by GitHub, or picked by the user. Such a person counts as working on
// the repository whatever their association.
func vouches(role Role) bool {
	return role == RoleRequested || role == RoleSuggested || role == RolePicked
}
