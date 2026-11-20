import { useId } from 'react'

import { useInspector } from '../../lib/useInspector'
import type { InspectorState } from '../../platform'
import { buttonClass } from '../controls'
import { Group, SectionIntro, SwitchRow } from './rows'

// keptWords say how the notes are kept.
const keptWords = 'Notes are kept, with their pictures, until you clear them.'

// notesText says how many of the notes the user left are open and done.
function notesText({ open, kept }: InspectorState): string {
  if (kept === 0) {
    return `None yet. ${keptWords}`
  }

  const done = kept - open

  return `${String(open)} open${done > 0 ? `, ${String(done)} done` : ''}. ${keptWords}`
}

// InspectorCard is the inspector's part of About, in a build from source alone, whose
// agent the window then runs (internal/devtools): whether ⌥⌘P, and the menu bar's Point at
// an Element, start pointing at an element to leave a note on it, and the notes left, to
// clear. A release runs no agent, so it shows nothing there. "Find a setting" leaves it
// out, as a release has none of it.
export function InspectorCard() {
  const id = useId()
  const { inspector, state } = useInspector()

  if (inspector === null || state === null) {
    return null
  }

  return (
    <section aria-labelledby={`${id}-inspector`} className="flex flex-col gap-2">
      <SectionIntro id={`${id}-inspector`} title="Inspector">
        This build from source carries the inspector, which the magex inspect targets talk to. Point at something in the
        window and say what is wrong with it, and magex inspect:notes hands your note on, with a picture.
      </SectionIntro>
      <Group>
        <SwitchRow
          label="Point at elements"
          controlId={`${id}-point`}
          description="⌥⌘P, or Point at an Element in the menu bar, outlines what the pointer is over; click it to leave a note."
          checked={state.enabled}
          onChange={(on) => {
            inspector.setEnabled(on)
          }}
        />
        <div className="flex items-center justify-between gap-8 px-4 py-3.5">
          <div className="min-w-0">
            <p className="text-sm font-medium">Notes</p>
            <p className="mt-0.5 text-xs text-muted">{notesText(state)}</p>
          </div>
          <button
            type="button"
            disabled={state.kept === 0}
            onClick={() => {
              inspector.clearNotes()
            }}
            className={`${buttonClass} shrink-0`}
          >
            Clear notes
          </button>
        </div>
      </Group>
    </section>
  )
}
