package prompts

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/textutil"
)

// RoutinePrompts carries the text of the prompts an Autopilot export names, in the prompts'
// own file format, and brings it in on an import. It is a type of its own, which core
// hands Autopilot, never a method of the bound Service, so the window never reaches it.
type RoutinePrompts struct {
	s *Service
}

// NewRoutinePrompts returns the prompts an export of routines carries, over s.
func NewRoutinePrompts(s *Service) *RoutinePrompts {
	return &RoutinePrompts{s: s}
}

// RoutineArrival says what bringing in one of a routine export's prompts would do: it is
// added (New), the prompt here it matches is the same (Same), or it differs, and the one
// here is kept.
type RoutineArrival struct {
	Nickname string
	New      bool
	Same     bool
}

// Bundled returns the library's prompts with nicknames (ignoring case), in the prompts'
// file format, and how many it holds; none found is nil and 0.
func (r *RoutinePrompts) Bundled(ctx context.Context, nicknames []string) ([]byte, int, error) {
	lib, err := r.s.store.Library(ctx)
	if err != nil {
		return nil, 0, err
	}

	var ids []int64

	for _, p := range lib.Prompts {
		if textutil.ContainsFold(nicknames, p.Nickname) {
			ids = append(ids, p.ID)
		}
	}

	if len(ids) == 0 {
		return nil, 0, nil
	}

	b := newBundle(lib, records{}, ids, r.s.clock.Now(), r.s.app)
	b.Defaults = BundleDefaults{}

	data, err := EncodeBundle(b)
	if err != nil {
		return nil, 0, err
	}

	return data, len(b.Prompts), nil
}

// Arrivals says what bringing in each of data's prompts would do, by its UUID, else its
// nickname: only a prompt matching no prompt here is added, and one that matches keeps the
// copy here, which the import says when the two differ. A prompt whose UUID a prompt here
// has is that prompt, whatever either is called now.
func (r *RoutinePrompts) Arrivals(ctx context.Context, data []byte) ([]RoutineArrival, error) {
	b, err := DecodeBundle(data)
	if err != nil {
		return nil, err
	}

	all, err := r.s.store.All(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]RoutineArrival, 0, len(b.Prompts))

	for _, p := range b.Prompts {
		a := RoutineArrival{Nickname: p.Nickname, New: true}

		if i := arrivalMatch(all, p); i >= 0 {
			a.New, a.Same = false, sameContent(all[i], p)
		}

		out = append(out, a)
	}

	return out, nil
}

// AddNew adds data's prompts that match no prompt here, by UUID or by nickname, as an
// import of prompts adds them (checked as the editor checks them), all of them or none,
// and returns the nicknames it added.
func (r *RoutinePrompts) AddNew(ctx context.Context, data []byte) ([]string, error) {
	b, err := DecodeBundle(data)
	if err != nil {
		return nil, err
	}

	all, err := r.s.store.All(ctx)
	if err != nil {
		return nil, err
	}

	fresh := slices.DeleteFunc(slices.Clone(b.Prompts), func(p BundlePrompt) bool { return arrivalMatch(all, p) >= 0 })

	if len(fresh) == 0 {
		return []string{}, nil
	}

	if _, err = r.s.Import(ctx, fresh, BundleDefaults{}); err != nil {
		return nil, fmt.Errorf("add the routines' prompts: %w", err)
	}

	out := make([]string, 0, len(fresh))
	for _, p := range fresh {
		out = append(out, p.Nickname)
	}

	return out, nil
}

// arrivalMatch returns the index in all of the prompt one of an export's prompts is: the
// one with its UUID, else the one with its nickname, ignoring case; -1 for none.
func arrivalMatch(all []Prompt, p BundlePrompt) int {
	if u, ok := textutil.CanonicalUUID(strings.TrimSpace(p.UUID)); ok {
		if i := slices.IndexFunc(all, func(q Prompt) bool { return q.UUID == u }); i >= 0 {
			return i
		}
	}

	return slices.IndexFunc(all, func(q Prompt) bool { return strings.EqualFold(q.Nickname, p.Nickname) })
}
