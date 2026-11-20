import { useId, useState } from 'react'

import { modeLabel } from '../../../lib/autopilot'
import type { Result } from '../../../lib/call'
import {
  changeDraft,
  changedId,
  claudeModeHint,
  isEdit,
  listsOnly,
  newRoutineDraft,
  storedMatchWords,
  type CardDraft,
  type RoutinePart,
} from '../../../lib/describeit'
import { actsOn, canonical } from '../../../lib/routineBuilder'
import { useQuery } from '../../../lib/useQuery'
import { routineMatchStored, type AutopilotMode, type AutopilotRoutine } from '../../../platform'
import { buttonClass } from '../../controls'
import { Notes } from '../builder/parts'
import { DraftTestPanel, ModeChoice, ThisMac, useDraftReview } from '../DraftReview'
import { CantApply, OutcomeLine, Risks, RowActions, rowClass, RowTitle, useAct, type RowProps } from './parts'

export interface RoutineChangeProps extends RowProps<RoutinePart> {
  // mode is the mode a new routine starts in, as chosen here, and onMode chooses another.
  mode: AutopilotMode
  onMode: (mode: AutopilotMode) => void
  // saveChange saves a change to one of the user's routines, asking first when it can act
  // on what the routine matches now; an error of '' is the question closed unanswered.
  saveChange: (p: RoutinePart) => Promise<Result<AutopilotRoutine>>
  // saving reports the question's save under way.
  saving: boolean
}

// changedName is the name of the routine a change changes, as Claude read it.
function changedName(p: RoutinePart): string {
  return p.checked.before?.name ?? p.checked.name
}

// Heading is a routine row's heading: the name it takes here, and whether it is new or
// which routine it changes, which may have changed since Claude read it.
function Heading({ id, p }: { id: string; p: RoutinePart }) {
  return (
    <RowTitle id={id} title={p.checked.name} kind={isEdit(p) ? `Changes “${changedName(p)}”` : 'New'}>
      {p.checked.stale && <span className="text-xs text-warning">changed since Claude read it</span>}
    </RowTitle>
  )
}

// Sentences are what the routine does: for a change, as it is and as it would be.
function Sentences({ p, sentence }: { p: RoutinePart; sentence: string }) {
  if (!isEdit(p)) {
    return <p className="text-sm leading-relaxed text-text">{sentence === '' ? 'It doesn’t read yet.' : sentence}</p>
  }

  return (
    <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 text-sm leading-relaxed">
      <dt className="text-xs text-muted">Now</dt>
      <dd className="text-muted">{p.checked.before?.sentence ?? ''}</dd>
      <dt className="text-xs text-muted">With the change</dt>
      <dd className="text-text">{sentence === '' ? 'It doesn’t read yet.' : sentence}</dd>
    </dl>
  )
}

// RoutineChange is a card's new routine, or its change to one of the user's routines, as
// quick setup reviews a draft: the name it takes here, New or which routine it changes
// (and that it changed since Claude read it), what it does (for a change, as it is and as it
// would be), the mode a new routine starts in, with Claude's suggestion as a hint (a change
// keeps the routine's own mode and switch), its problems and warnings as read live, what
// this Mac has for it, what it matches in the inbox now, its risks, and Add or Save changes
// (asking first about what a routine that is on matches now), Edit in builder or Open the
// routine, Test, and Discard.
export function RoutineChange(props: RoutineChangeProps) {
  const id = useId()

  return (
    <div role="group" aria-labelledby={`${id}-title`} className={rowClass}>
      <Heading id={`${id}-title`} p={props.p} />
      {props.outcome !== undefined ? (
        <>
          <Sentences p={props.p} sentence={props.p.checked.sentence} />
          <OutcomeLine part="routine" outcome={props.outcome} onShowRoutine={props.kit.onShowRoutine} />
        </>
      ) : (
        <OpenRoutine {...props} />
      )}
    </div>
  )
}

// draftOf is the routine as a row reviews it: a change over the routine as Claude read it,
// or a new one in the mode chosen.
function draftOf(p: RoutinePart, mode: AutopilotMode, defaultMode: AutopilotMode): CardDraft {
  if (!isEdit(p)) {
    return newRoutineDraft(p, mode)
  }

  return changeDraft(p.checked.before ?? { id: changedId(p), mode: defaultMode, description: '' }, p)
}

// OpenRoutine is the body of a routine's row no one has dealt with yet, under its heading.
function OpenRoutine({ p, held, onSettle, kit, mode, onMode, saveChange, saving }: RoutineChangeProps) {
  const act = useAct(p, onSettle)
  const [testing, setTesting] = useState(false)
  const edit = isEdit(p)
  const draft = draftOf(p, mode, kit.defaultMode)
  const review = useDraftReview(draft, kit.describeDelay)
  const reading = review.fresh ? review.reading : undefined
  const problems = reading?.problems ?? p.checked.problems
  const warnings = reading?.warnings ?? p.checked.warnings
  const blocked = p.checked.gone || problems.length > 0
  const sentence = reading !== undefined && reading.sentence !== '' ? reading.sentence : p.checked.sentence
  const lists = listsOnly(draft.spec)
  const stored = useQuery(() => routineMatchStored(draft), [draft.id, canonical(draft.spec)], {
    enabled: lists && !blocked,
  })
  const busy = held || act.busy || saving

  const matchLine = (() => {
    if (!lists) {
      return 'It watches a search, which Test reads.'
    }

    if (blocked) {
      return ''
    }

    if (stored.data !== undefined) {
      return `${storedMatchWords(stored.data)}.`
    }

    return stored.error === '' ? 'Counting what it matches in your inbox…' : stored.error
  })()

  return (
    <>
      <Sentences p={p} sentence={sentence} />
      {p.checked.gone && (
        <p className="text-xs text-danger">{`“${p.checked.name}” is gone, so this change can’t be saved.`}</p>
      )}
      {edit ? (
        p.checked.before !== null && (
          <p className="text-xs text-muted">
            {`It keeps its own mode, ${modeLabel(p.checked.before.mode)}, and stays switched ${p.checked.before.enabled ? 'on' : 'off'}.`}
          </p>
        )
      ) : (
        <ModeChoice mode={mode} hint={claudeModeHint(p.change?.mode, mode)} onChange={onMode} />
      )}
      <Notes problems={problems} warnings={warnings} />
      {blocked && <CantApply />}
      <ThisMac readiness={review.readiness} />
      {matchLine !== '' && <p className="text-xs text-muted">{matchLine}</p>}
      <Risks risks={p.checked.risks} />
      <RowActions
        label={edit ? 'Save changes' : 'Add'}
        blocked={blocked}
        busy={busy}
        error={act.error}
        onApply={() => {
          void act.act(edit ? 'saved' : 'added', async () => {
            const saved = edit ? await saveChange(p) : await review.save()

            return saved.ok ? { ok: true, data: saved.data.id } : saved
          })
        }}
        onDiscard={() => {
          void act.act('discarded')
        }}
      >
        {edit ? (
          <button
            type="button"
            disabled={p.checked.gone}
            onClick={() => {
              kit.onShowRoutine(changedId(p))
            }}
            className={buttonClass}
          >
            Open the routine
          </button>
        ) : (
          <button
            type="button"
            disabled={busy}
            onClick={() => {
              void act.act('builder')
              kit.onEditInBuilder(draft)
            }}
            className={buttonClass}
          >
            Edit in builder
          </button>
        )}
        <button
          type="button"
          aria-expanded={testing}
          onClick={() => {
            if (!testing && !blocked) {
              review.test.run()
            }

            setTesting(!testing)
          }}
          className={buttonClass}
        >
          {testing ? 'Hide the test' : 'Test'}
        </button>
      </RowActions>
      {testing && (
        <DraftTestPanel
          test={review.test}
          ready={!blocked && reading !== undefined}
          kinds={kit.catalog === undefined ? [] : actsOn(kit.catalog, draft.spec)}
          mode={draft.mode}
          draft={draft}
        />
      )}
    </>
  )
}
