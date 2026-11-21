import { useId } from 'react'

import { useSettingsSave } from '../../lib/useSettingsSave'
import { resetTrustedBots, setTrustedBots, type Settings } from '../../platform'
import { quietButtonClass } from '../controls'
import { Note, SectionIntro } from './rows'
import { StringListEditor } from './StringListEditor'
import type { SectionProps } from './types'
import { useListChange } from './useListChange'

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
  // Every change, the form's and the list's, runs through one action: one at a time, with
  // the controls held while it runs, and the draft cleared once it is stored.
  const { draft, setDraft, refusal, refuse, store: change, busy } = useListChange(save)

  const bots = settings.trustedBots
  const defaults = settings.defaultTrustedBots
  const changed = !sameList(bots, defaults)
  const error = refusal.draft === draft ? refusal.message : ''

  // store replaces the trusted bots with next, which call stores, through the action, and
  // clears the draft once it is stored.
  const store = (next: string[], call: () => PromiseLike<Settings>) => {
    change({
      shows: { trustedBots: next },
      call,
      then: () => {
        setDraft('')
      },
    })
  }

  const add = () => {
    const login = foldBot(draft)

    if (login === '') {
      refuse('Enter a bot’s login, such as renovate.')

      return
    }

    if (bots.includes(login)) {
      refuse(`${login} is already trusted.`)

      return
    }

    const next = [...bots, login]

    store(next, () => setTrustedBots(next))
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
                  store(defaults, () => resetTrustedBots())
                }}
                className={`${quietButtonClass} shrink-0`}
              >
                Restore the defaults
              </button>
            )
          }
        >
          Their pull requests and issues are trusted like a collaborator&apos;s: launches use the profile&apos;s
          permissions instead of plan or read-only mode.
        </SectionIntro>
        <StringListEditor
          label="Trusted bots"
          items={bots}
          empty="No bot is trusted: every bot's items start in plan or read-only mode."
          removeLabel={(bot) => `Stop trusting ${bot}`}
          onRemove={(bot) => {
            const next = bots.filter((b) => b !== bot)

            store(next, () => setTrustedBots(next))
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
            list, and every pull request from a fork. Their items start in plan mode (read-only for Codex), and their
            prompts end with a reminder to treat their text as information. A launch can lift the restriction with{' '}
            <span className="text-text">Use profile permissions for this item</span>.
          </li>
        </ul>
      </section>
    </div>
  )
}
