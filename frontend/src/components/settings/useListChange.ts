import { startTransition, useActionState, useState } from 'react'

import type { SettingsSave } from '../../lib/useSettingsSave'
import type { Settings } from '../../platform'

// Refusal is why a change was refused, and the draft typed when it was: typing again
// clears a refused draft. field is where it shows, in a section that shows refusals in
// more than one place; a section with one place leaves it out.
export interface Refusal<F extends string> {
  field?: F | undefined
  draft: string
  message: string
}

// ListChange is a change made through useListChange: how it shows at once, the call that
// stores it, where a refusal of it shows, and what to do once it is stored.
export interface ListChange<F extends string> {
  shows: Partial<Settings>
  call: () => PromiseLike<Settings>
  field?: F | undefined
  then?: (() => void) | undefined
}

// ListChanges are a settings section's list as it edits it.
export interface ListChanges<F extends string> {
  // draft is what the field holds, to add to the list.
  draft: string
  setDraft: (draft: string) => void
  // refusal is why the draft or the last change was refused: the section's own refusal
  // of the draft while it stands, none while a change runs, and otherwise the last
  // change's, or none.
  refusal: Refusal<F>
  // refuse refuses the draft before asking the backend, with message, under field, until
  // the next change.
  refuse: (message: string, field?: F) => void
  // store makes a change through the section's one action, and busy reports one running.
  store: (change: ListChange<F>) => void
  busy: boolean
}

const noRefusal: Refusal<never> = { draft: '', message: '' }

// useListChange keeps a settings section's list the one way each one does, the field that
// adds to it and why a change was refused: every change runs through one action, one at a
// time, with the controls held (busy) while it runs, and its refusal remembers the draft
// typed when it was made. The section keeps its own rules: what it adds and removes, what
// it refuses first, where each refusal shows, and when the draft is cleared (then).
export function useListChange<F extends string = never>(save: SettingsSave['save']): ListChanges<F> {
  const [draft, setDraft] = useState('')
  // invalid is why the draft cannot be added, found before asking the backend.
  const [invalid, setInvalid] = useState<Refusal<F>>(noRefusal)
  const [refused, change, busy] = useActionState(
    async (_: Refusal<F>, next: ListChange<F> & { draft: string }): Promise<Refusal<F>> => {
      const result = await save(next.shows, next.call)

      if (!result.ok) {
        return { field: next.field, draft: next.draft, message: result.error }
      }

      next.then?.()

      return noRefusal
    },
    noRefusal,
  )

  const refuse = (message: string, field?: F) => {
    setInvalid({ field, draft, message })
  }

  const store = (next: ListChange<F>) => {
    setInvalid(noRefusal)
    startTransition(() => {
      change({ ...next, draft })
    })
  }

  return {
    draft,
    setDraft,
    refusal: invalid.message !== '' ? invalid : busy ? noRefusal : refused,
    refuse,
    store,
    busy,
  }
}
