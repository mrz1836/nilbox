package draft

import "github.com/mrz1836/nilbox/internal/autopilot/spec"

// Stamp writes into sp, in place, what each prompt and script its steps name is now in
// library and in mine, wherever the reference resolves (PromptIndex, FindScript): the
// record's UUID, so the reference finds it whatever it is called next, and its fallback
// brought up to date, a prompt's nickname, or its starter's key while the prompt is still
// that starter's copy, and a script's ID and name. A reference that resolves to nothing,
// and a script step that waits to be picked again, are left as they are, and nothing is
// ever written as "". It reports whether it changed sp.
func Stamp(library []Prompt, mine []Script, sp *spec.Spec) bool {
	prompts := stampPrompts(library, sp)

	return stampScripts(mine, sp) || prompts
}

// stampPrompts stamps the prompts sp's steps name (Stamp), and reports whether it changed
// any.
func stampPrompts(library []Prompt, sp *spec.Spec) bool {
	changed := false

	for _, ref := range spec.PromptRefs(sp) {
		i := PromptIndex(library, *ref)
		if i < 0 {
			continue
		}

		if next := stampedPrompt(*ref, library[i]); next != *ref {
			*ref, changed = next, true
		}
	}

	return changed
}

// stampScripts stamps the scripts sp's steps run (Stamp), and reports whether it changed
// any.
func stampScripts(mine []Script, sp *spec.Spec) bool {
	changed := false

	for _, step := range spec.ScriptSteps(sp) {
		at, renamed := FindScript(mine, *step)
		if at < 0 || renamed {
			continue
		}

		if next := stampedScript(*step, mine[at]); next != *step {
			*step, changed = next, true
		}
	}

	return changed
}

// Stamped returns a copy of sp stamped (Stamp), as it loads back, and encoded as it is
// stored, when stamping changed anything and the copy still loads, within spec.MaxBytes;
// otherwise sp, no text, and false, so the caller keeps what it has.
func Stamped(library []Prompt, mine []Script, sp spec.Spec) (spec.Spec, []byte, bool) {
	out := spec.Clone(sp)
	if !Stamp(library, mine, &out) {
		return sp, nil, false
	}

	data, err := spec.Encode(spec.Normalize(out))
	if err != nil {
		return sp, nil, false
	}

	loaded, err := spec.Load(data)
	if err != nil {
		return sp, nil, false
	}

	return loaded, data, true
}

// stampedPrompt is ref naming p: by p's UUID, and by its starter's key while p is still
// the copy of the starter ref names, else by p's nickname now.
func stampedPrompt(ref spec.PromptRef, p Prompt) spec.PromptRef {
	out := ref
	if p.UUID != "" {
		out.UUID = p.UUID
	}

	if (ref.Starter == "" || p.Starter != ref.Starter) && p.Nickname != "" {
		out.Starter, out.Nickname = "", p.Nickname
	}

	return out
}

// stampedScript is step naming s: by its UUID, its ID, and its name now.
func stampedScript(step spec.ScriptStep, s Script) spec.ScriptStep {
	out := step

	if s.UUID != "" {
		out.UUID = s.UUID
	}

	if s.ID > 0 {
		out.ID = s.ID
	}

	if s.Name != "" {
		out.Name = s.Name
	}

	return out
}
