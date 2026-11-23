package spec

import "slices"

// PromptRefs returns the library prompts s's steps name, to read or to change in place:
// each agent step's prompt but the item's own default, and each GitHub step's replies, in
// the steps' order.
func PromptRefs(s *Spec) []*PromptRef {
	var out []*PromptRef

	for i := range s.Steps {
		if a := s.Steps[i].Agent; a != nil && !a.Prompt.ItemDefault {
			out = append(out, &a.Prompt)
		}

		for j := range s.Steps[i].GitHub {
			if r := s.Steps[i].GitHub[j].Reply; r != nil {
				out = append(out, &r.Prompt)
			}
		}
	}

	return out
}

// ScriptSteps returns the scripts s's steps run, to read or to change in place, in the
// steps' order.
func ScriptSteps(s *Spec) []*ScriptStep {
	var out []*ScriptStep

	for i := range s.Steps {
		if sc := s.Steps[i].Script; sc != nil {
			out = append(out, sc)
		}
	}

	return out
}

// Clone returns a copy of s whose steps, and the prompts and scripts they name, are its
// own, so changing what PromptRefs and ScriptSteps return on it never changes s.
func Clone(s Spec) Spec {
	out := s
	out.Steps = slices.Clone(s.Steps)

	for i := range out.Steps {
		st := &out.Steps[i]

		if st.Agent != nil {
			a := *st.Agent
			st.Agent = &a
		}

		if st.Script != nil {
			sc := *st.Script
			st.Script = &sc
		}

		st.GitHub = slices.Clone(st.GitHub)

		for j := range st.GitHub {
			if r := st.GitHub[j].Reply; r != nil {
				reply := *r
				st.GitHub[j].Reply = &reply
			}
		}
	}

	return out
}

// WithoutUUIDs returns a copy of s with no prompt's or script's UUID, as Describe it reads
// a routine: by the names the user knows, the fallbacks every reference keeps.
func WithoutUUIDs(s Spec) Spec {
	out := Clone(s)

	for _, ref := range PromptRefs(&out) {
		ref.UUID = ""
	}

	for _, sc := range ScriptSteps(&out) {
		sc.UUID = ""
	}

	return out
}
