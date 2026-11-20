package assignees

import (
	"cmp"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/repo"
)

// maxCandidates is the most people a list offers: enough for every person GitHub's whole
// list names (one page of 100) beside the item's own people, so a list that named everyone
// is never cut short, though the picker shows pictures for the first 100 alone.
const maxCandidates = 300

// book is what rank knows of each person, by login lower-cased: who can be assigned, who
// works on the repository and on the owner's others, and whom the read just made found.
type book struct {
	assignable map[string]Assignable
	workers    map[string]Worker
	org        map[string]Worker
	found      map[string]Person
}

// bookOf indexes k and found.
func bookOf(k Known, found []Person) book {
	b := book{
		assignable: make(map[string]Assignable, len(k.Assignable)), workers: make(map[string]Worker, len(k.WorksHere)),
		org: make(map[string]Worker, len(k.Org)), found: make(map[string]Person, len(found)),
	}

	for _, a := range k.Assignable {
		b.assignable[strings.ToLower(a.Login)] = a
	}

	for _, w := range k.WorksHere {
		b.workers[strings.ToLower(w.Login)] = w
	}

	for _, w := range k.Org {
		b.org[strings.ToLower(w.Login)] = w
	}

	for _, p := range found {
		b.found[strings.ToLower(p.Login)] = p
	}

	return b
}

// listed reports that GitHub listed login as assignable in the repository.
func (b book) listed(login string) bool {
	return b.assignable[strings.ToLower(login)].Listed
}

// candidate is login as the list offers them for source, with all the book knows of them;
// viewer is the signed-in user.
func (b book) candidate(login string, source Source, viewer string) Candidate {
	key := strings.ToLower(login)
	a, w, org := b.assignable[key], b.workers[key], b.org[key]

	if source == SourceOrg {
		w = org
	}

	return Candidate{
		Login: login, Name: cmp.Or(b.found[key].Name, a.Name, w.Name, org.Name), Mine: isLogin(login, viewer),
		Source: source, Reviewed: w.Reviewed, Authored: w.Authored, Committed: w.Committed,
		AssignedAt: stamp(a.AssignedAt), LastAt: stamp(w.LastAt), Where: w.Where,
	}
}

// offer is a list rank builds: each person once, never a bot or a login GitHub could not
// have issued.
type offer struct {
	book   book
	viewer string
	listed map[string]bool
	people []Candidate
}

// add offers login for source, unless it is left out or offered already.
func (o *offer) add(login string, source Source) {
	key := strings.ToLower(login)
	if !repo.ValidOwner(login) || o.listed[key] || isBot(login) {
		return
	}

	o.listed[key] = true
	o.people = append(o.people, o.book.candidate(login, source, o.viewer))
}

// rank is whom the picker offers to assign to it, from what the directory knows (k) and
// what a search just found (found, GitHub's order), each once, in order: the user, the
// author, the participants, the people assigned in the repository before (the latest
// first), the people who work on it (highest ranked first), the people the search found that
// the list holds nothing else of, the rest of those GitHub listed (by login), and, while the
// list GitHub gave is cut short, the people who work on the owner's other repositories. Once
// GitHub's whole list named everyone, a person only the directory knows, not on it, is left
// out: GitHub would ignore their assignment. A query keeps those whose login or name matches
// it (matchRank), and everyone the search found, those that match best first.
func rank(it Item, k Known, found []Person, query string) []Candidate {
	o := offer{book: bookOf(k, found), viewer: it.Viewer, listed: map[string]bool{}, people: []Candidate{}}
	o.addItem(it)
	o.addKnown(k, found)

	out := matching(o.people, query)

	return out[:min(len(out), maxCandidates)]
}

// addItem offers the item's own people: the user, the author when a person, and the
// participants.
func (o *offer) addItem(it Item) {
	if it.Viewer != "" {
		o.add(it.Viewer, SourceYou)
	}

	if it.AuthorIsPerson && it.Author != "" {
		o.add(it.Author, SourceAuthor)
	}

	o.addEach(it.Participants, SourceParticipant, nil)
}

// addKnown offers, after the item's own people, those k knows and those found, in rank's
// order.
func (o *offer) addKnown(k Known, found []Person) {
	onList := func(login string) bool { return !k.Complete || o.book.listed(login) }
	listed := slices.DeleteFunc(slices.Clone(k.Assignable), func(a Assignable) bool { return !a.Listed })

	o.addEach(loginsOf(assignedBefore(k.Assignable), assignableLogin), SourceAssignedBefore, onList)
	o.addEach(loginsOf(k.WorksHere, workerLogin), SourceWorksHere, onList)
	o.addEach(loginsOf(found, personLogin), SourceFound, nil)
	o.addEach(loginsOf(listed, assignableLogin), SourceListed, nil)

	if !k.Complete {
		o.addEach(loginsOf(k.Org, workerLogin), SourceOrg, nil)
	}
}

// addEach offers each of logins for source, those keep keeps when it is set.
func (o *offer) addEach(logins []string, source Source, keep func(string) bool) {
	for _, login := range logins {
		if keep == nil || keep(login) {
			o.add(login, source)
		}
	}
}

// loginsOf are the logins of list, as login reads each.
func loginsOf[T any](list []T, login func(T) string) []string {
	out := make([]string, 0, len(list))
	for _, v := range list {
		out = append(out, login(v))
	}

	return out
}

// assignableLogin, workerLogin, and personLogin read each kind's login.
func assignableLogin(a Assignable) string { return a.Login }
func workerLogin(w Worker) string         { return w.Login }
func personLogin(p Person) string         { return p.Login }

// assignedBefore are the people of list seen assigned, the latest first, then by login.
func assignedBefore(list []Assignable) []Assignable {
	out := slices.DeleteFunc(slices.Clone(list), func(a Assignable) bool { return a.AssignedAt.IsZero() })

	slices.SortStableFunc(out, func(a, b Assignable) int {
		return cmp.Or(b.AssignedAt.Compare(a.AssignedAt), strings.Compare(strings.ToLower(a.Login), strings.ToLower(b.Login)))
	})

	return out
}

// matching is people whose login or name matches query (matchRank), those whom a search
// found always, those that match best first, each kind in the order given: all of people for
// an empty query.
func matching(people []Candidate, query string) []Candidate {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return people
	}

	type ranked struct {
		c    Candidate
		rank int
	}

	kept := make([]ranked, 0, len(people))

	for _, c := range people {
		r := matchRank(q, c.Login, c.Name)
		if r < 0 && c.Source == SourceFound {
			r = 1
		}

		if r >= 0 {
			kept = append(kept, ranked{c: c, rank: r})
		}
	}

	slices.SortStableFunc(kept, func(a, b ranked) int { return a.rank - b.rank })

	out := make([]Candidate, 0, len(kept))
	for _, k := range kept {
		out = append(out, k.c)
	}

	return out
}

// matchRank is how well q, lower-cased and trimmed, matches any of fields, as the picker's
// own filter ranks them: 0 when one, or a word of one, starts with it, 1 when one holds it,
// and -1 when none does. Words part at spaces, slashes, underscores, and hyphens.
func matchRank(q string, fields ...string) int {
	held := false

	for _, f := range fields {
		f = strings.ToLower(f)
		if strings.HasPrefix(f, q) || slices.ContainsFunc(strings.FieldsFunc(f, wordBreak), func(w string) bool {
			return strings.HasPrefix(w, q)
		}) {
			return 0
		}

		held = held || strings.Contains(f, q)
	}

	if held {
		return 1
	}

	return -1
}

// wordBreak reports a rune words part at.
func wordBreak(r rune) bool {
	return unicode.IsSpace(r) || r == '/' || r == '_' || r == '-'
}

// isBot reports a login only a bot has: one GitHub's REST API spells with "[bot]".
func isBot(login string) bool {
	return strings.HasSuffix(strings.ToLower(login), "[bot]")
}

// isLogin reports that login is viewer's, ignoring case; never for an empty viewer.
func isLogin(login, viewer string) bool {
	return viewer != "" && strings.EqualFold(login, viewer)
}

// stamp writes t as every time the window reads is written, or "" for the zero time.
func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	return clock.Format(t)
}
