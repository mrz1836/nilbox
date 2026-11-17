import { startTransition, useActionState, useId, useState } from 'react'

import { useSettingsSave } from '../../lib/useSettingsSave'
import { setHiddenLabels } from '../../platform'
import { quietButtonClass } from '../controls'
import { Note, SectionIntro } from './rows'
import { StringListEditor } from './StringListEditor'
import type { SectionProps } from './types'

// Refusal is why a change was refused, and the draft typed when it was: typing again
// clears a refused pattern.
interface Refusal {
  draft: string
  message: string
}

const noRefusal: Refusal = { draft: '', message: '' }

// CardsSection is the settings page's Cards section: the labels the inbox's cards leave
// out, such as the size labels a card's own size grade already shows. Each change is
// stored as it is made; Restore the defaults puts the list back as a fresh install has it.
export function CardsSection({ settings: stored, onSaved }: SectionProps) {
  const id = useId()
  const { settings, save } = useSettingsSave(stored, onSaved)
  const [draft, setDraft] = useState('')
  // invalid is why the draft cannot be added, found before asking the backend.
  const [invalid, setInvalid] = useState<Refusal>(noRefusal)
  // Every change runs through one action: one at a time, with the controls held while it
  // runs.
  const [refused, change, busy] = useActionState(
    async (_: Refusal, next: { patterns: string[]; draft: string; then?: () => void }): Promise<Refusal> => {
      const result = await save({ hiddenLabels: next.patterns }, () => setHiddenLabels(next.patterns))

      if (!result.ok) {
        return { draft: next.draft, message: result.error }
      }

      next.then?.()

      return noRefusal
    },
    noRefusal,
  )

  const patterns = settings.hiddenLabels
  const defaults = settings.defaultHiddenLabels
  // A pattern matches ignoring case, so one of the defaults typed in another case is one.
  const isDefault = (pattern: string) => defaults.some((d) => d.toLowerCase() === pattern.toLowerCase())
  const atDefaults = patterns.length === defaults.length && patterns.every(isDefault)
  const refusal = invalid.message !== '' ? invalid : busy ? noRefusal : refused
  const error = refusal.draft === draft ? refusal.message : ''

  // store replaces the hidden labels.
  const store = (next: string[], then?: () => void) => {
    setInvalid(noRefusal)
    startTransition(() => {
      change({ patterns: next, draft, ...(then === undefined ? {} : { then }) })
    })
  }

  const add = () => {
    // The button is disabled while the field is blank, so the pattern is never empty.
    const pattern = draft.trim()

    if (patterns.some((p) => p.toLowerCase() === pattern.toLowerCase())) {
      setInvalid({ draft, message: `${pattern} is already hidden.` })

      return
    }

    store([...patterns, pattern], () => {
      setDraft('')
    })
  }

  return (
    <div className="flex flex-col gap-8">
      <section aria-labelledby={`${id}-hidden`} className="flex flex-col gap-3">
        <SectionIntro
          id={`${id}-hidden`}
          title="Hidden labels"
          action={
            !atDefaults && (
              <button
                type="button"
                disabled={busy}
                onClick={() => {
                  store([...defaults])
                }}
                className={`${quietButtonClass} shrink-0`}
              >
                Restore the defaults
              </button>
            )
          }
        >
          Labels the inbox’s cards leave out, in every repository. A card still shows its size, its checks, and the rest
          of its labels.
        </SectionIntro>
        <StringListEditor
          label="Hidden labels"
          items={patterns}
          empty="Every label shows on the cards."
          removeLabel={(pattern) => `Show ${pattern} again`}
          onRemove={(pattern) => {
            store(patterns.filter((p) => p !== pattern))
          }}
          extra={(pattern) => isDefault(pattern) && <span className="text-xs text-muted">default</span>}
          inputLabel="Label to hide"
          placeholder="A label, or a pattern such as size/*"
          submitLabel="Hide label"
          draft={draft}
          onDraftChange={setDraft}
          onAdd={add}
          error={error}
          describedBy={`${id}-syntax`}
          busy={busy}
        />
        <Note id={`${id}-syntax`}>
          Patterns: <code className="font-mono">*</code> matches anything, as in{' '}
          <code className="font-mono">size/*</code>, and <code className="font-mono">?</code> any one character,
          ignoring case. Hidden labels still match the filter.
        </Note>
      </section>
    </div>
  )
}
