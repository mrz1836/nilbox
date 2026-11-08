import { startTransition, useActionState, useId, useState } from 'react'

import { useSettingsSave } from '../../lib/useSettingsSave'
import { resetTrustedBots, setTrustedBots, type Settings } from '../../platform'
import { quietButtonClass } from '../controls'
import { Note, SectionIntro } from './rows'
import { StringListEditor } from './StringListEditor'
import type { SectionProps } from './types'

// Refusal is why a change was refused, with the draft typed when it was, since typing
// again clears it.
interface Refusal {
  draft: string
  message: string
}

// Change is a change to the trusted bots: the list it leaves, the call that stores it,
// and the draft typed when it was made.
interface Change {
  bots: string[]
  call: () => PromiseLike<Settings>
  draft: string
}

const noRefusal: Refusal = { draft: '', message: '' }

// foldBot writes a bot's login the way the trusted list keeps it: lowercase, without an
// "app/" prefix or "[bot]" suffix.
function foldBot(login: string): string {
  return login
    .trim()
    .toLowerCase()
    .replace(/^app\//, '')
    .replace(/\[bot\]$/, '')
    .trim()
}

// sameList reports whether two lists hold the same logins in the same order.
function sameList(a: readonly string[], b: readonly string[]): boolean {
  return a.length === b.length && a.every((v, i) => v === b[i])
}

// TrustSection is the settings page's Trust section: who launches trust, and the list of
// bots and apps (GitHub Apps) trusted like a collaborator, which the user can add to,
// remove from, or restore.
export function TrustSection({ settings: stored, onSaved }: SectionProps) {
  const id = useId()
  const { settings, save } = useSettingsSave(stored, onSaved)
  const [draft, setDraft] = useState('')
  // invalid is why the draft cannot be added, found before asking the backend.
  const [invalid, setInvalid] = useState<Refusal>(noRefusal)
  // Every change, the form's and the list's, runs through one action: one at a time, with
  // the controls held while it runs, and the draft cleared once it is stored.
  const [refused, change, busy] = useActionState(async (_: Refusal, next: Change): Promise<Refusal> => {
    const result = await save({ trustedBots: next.bots }, next.call)

    if (!result.ok) {
      return { draft: next.draft, message: result.error }
    }

    setDraft('')

    return noRefusal
  }, noRefusal)

  const bots = settings.trustedBots
  const defaults = settings.defaultTrustedBots
  const changed = !sameList(bots, defaults)
  const refusal = invalid.message !== '' ? invalid : busy ? noRefusal : refused
  const error = refusal.draft === draft ? refusal.message : ''

  // store makes a change through the action.
  const store = (next: Omit<Change, 'draft'>) => {
    setInvalid(noRefusal)
    startTransition(() => {
      change({ ...next, draft })
    })
  }

  const add = () => {
    const login = foldBot(draft)

    if (login === '') {
      setInvalid({ draft, message: 'Enter a bot’s login, such as renovate.' })

      return
    }

    if (bots.includes(login)) {
      setInvalid({ draft, message: `${login} is already trusted.` })

      return
    }

    const next = [...bots, login]

    store({ bots: next, call: () => setTrustedBots(next) })
  }

  return (
    <div className="flex flex-col gap-6">
      <section aria-labelledby={`${id}-bots`} className="flex flex-col gap-3">
        <SectionIntro
          id={`${id}-bots`}
          title="Trusted bots and apps"
          action={
            changed && (
              <button
                type="button"
                disabled={busy}
                onClick={() => {
                  store({ bots: defaults, call: () => resetTrustedBots() })
                }}
                className={`${quietButtonClass} shrink-0`}
              >
                Restore the defaults
              </button>
            )
          }
        >
          Their pull requests and issues are trusted like a collaborator&apos;s: launches use the profile&apos;s
          permissions instead of plan mode.
        </SectionIntro>
        <StringListEditor
          label="Trusted bots"
          items={bots}
          empty="No bot is trusted: every bot's items start in plan mode."
          removeLabel={(bot) => `Stop trusting ${bot}`}
          onRemove={(bot) => {
            const next = bots.filter((b) => b !== bot)

            store({ bots: next, call: () => setTrustedBots(next) })
          }}
          extra={(bot) => defaults.includes(bot) && <span className="text-xs text-muted">default</span>}
          inputLabel="Bot to trust"
          placeholder="A bot’s login, such as my-release-bot"
          submitLabel="Trust bot"
          draft={draft}
          onDraftChange={setDraft}
          onAdd={add}
          error={error}
          busy={busy}
        />
        <Note>
          Use the login GitHub shows for the bot, with or without <code className="font-mono">[bot]</code>. A bot is
          trusted only when GitHub reports the author as a bot, so a person with a similar login never is, and a pull
          request from a fork never is. A bot&apos;s description can quote other projects&apos; text, such as release
          notes, so its prompts end with a reminder to treat that text as information.
        </Note>
      </section>

      <section aria-labelledby={`${id}-rule`} className="flex flex-col gap-2">
        <SectionIntro id={`${id}-rule`} title="How trust works" />
        <ul className="flex flex-col gap-1.5 text-xs leading-relaxed text-muted">
          <li>
            <span className="font-medium text-text">Trusted:</span> the repository&apos;s owner, members of its org, and
            collaborators, plus the trusted bots above, when a pull request&apos;s branch is in the repository itself.
          </li>
          <li>
            <span className="font-medium text-text">Outside contributor:</span> everyone else, every bot not on the
            list, and every pull request from a fork. Their items start in plan mode, and their prompts end with a
            reminder to treat their text as information. A launch can lift the restriction with{' '}
            <span className="text-text">Use profile permissions for this item</span>.
          </li>
        </ul>
      </section>
    </div>
  )
}
