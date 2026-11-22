import { useEffect, useEffectEvent, useId, useRef, useState, type ReactElement } from 'react'

import { attempt } from '../../../lib/call'
import { agentWords, chipsFor, costWords, fallbackChips, withChips, type Chip } from '../../../lib/describeit'
import { thousands } from '../../../lib/runView'
import { useConversation } from '../../../lib/useConversation'
import { useQuery } from '../../../lib/useQuery'
import {
  describeItStatus,
  listAgents,
  listProfiles,
  type AutopilotCatalog,
  type AutopilotDraft,
  type DescribeAgentChoice,
  type DescribeAttachRef,
  type Settings,
} from '../../../platform'
import { ConfirmDialog } from '../../ConfirmDialog'
import { buttonClass, ErrorText, fieldClass, primaryButtonClass, quietButtonClass } from '../../controls'
import { ErrorBanner } from '../../ErrorBanner'
import { ChevronDownIcon, ChevronRightIcon, SparkIcon } from '../../icons'
import { Composer } from './Composer'
import { OutsideProposals } from './OutsideProposals'
import type { CardKit } from './parts'
import { Transcript } from './Transcript'

// DescribeRequest is what opens Describe it from elsewhere in the window: a message to send
// at once (the empty tab's composer), or what to attach to the next one (Change with
// Claude… attaches a routine, Describe a routine for this… an item). seq tells one request
// from the next, so the same one asked again acts again.
export interface DescribeRequest {
  seq: number
  text?: string
  attach?: DescribeAttachRef[]
}

// DescribeViewProps are what the Autopilot tab hands Describe it.
export interface DescribeViewProps {
  // request is the latest request from elsewhere, undefined for none.
  request?: DescribeRequest

  // settings are the stored settings, null until they load, whose mode new routines start
  // in the cards take; catalog is what a routine can do, undefined until it is read. Both
  // are the tab's own, so a change made elsewhere reaches the view while it is hidden.
  settings: Settings | null
  catalog: AutopilotCatalog | undefined

  // onEditInBuilder opens the builder on a draft, as a template's Customize does
  // (AutopilotView's newFrom).
  onEditInBuilder: (draft: AutopilotDraft) => void

  // onShowRoutine selects one of the user's routines in the tab: a card's Show it.
  onShowRoutine: (id: number) => void

  // onSettingsSaved hears the settings as a card's settings or rules change left them, so
  // the window's own show the change (AutopilotView's onSettingsSaved).
  onSettingsSaved: (settings: Settings) => void
}

// handled are the requests Describe it has acted on, kept while it is away: the tab hands
// the same request again each time Describe it shows, which must not send it twice.
const handled = new WeakSet<DescribeRequest>()

// inherit is the agent new conversations start with, as Settings chose it.
const inherit: DescribeAgentChoice = { profile: '', model: '', effort: '' }

// describeDelay is how long a card's routine rests before it is read again.
const describeDelay = 300

// sameRef reports two attachments of the same thing.
function sameRef(a: DescribeAttachRef, b: DescribeAttachRef): boolean {
  return a.kind === b.kind && a.id === b.id
}

interface AgentFormProps {
  initial: DescribeAgentChoice
  busy: boolean
  error: string
  onStart: (choice: DescribeAgentChoice) => void
  onCancel: () => void
}

// AgentForm is Change ▸: the profile, model, and effort a new conversation runs with,
// each Inherit to take Settings' own, which start a conversation anew.
function AgentForm({ initial, busy, error, onStart, onCancel }: AgentFormProps) {
  const id = useId()
  const [choice, setChoice] = useState(initial)
  const profiles = useQuery(() => listProfiles(), [], { log: 'could not list the profiles for Describe it' })
  const agents = useQuery(() => listAgents(), [], { log: 'could not list the agents for Describe it' })
  const claude = agents.data?.find((a) => a.id === 'claude')
  const names = (profiles.data ?? []).filter((p) => p.agent === 'claude').map((p) => p.name)
  const options = (values: readonly string[], current: string) =>
    current === '' || values.includes(current) ? values : [...values, current]

  const fields: readonly {
    key: keyof DescribeAgentChoice
    label: string
    values: readonly { value: string; label: string }[]
  }[] = [
    { key: 'profile', label: 'Profile', values: options(names, choice.profile).map((v) => ({ value: v, label: v })) },
    {
      key: 'model',
      label: 'Model',
      values: options(claude?.models.map((m) => m.value) ?? [], choice.model).map((v) => ({
        value: v,
        label: claude?.models.find((m) => m.value === v)?.label ?? v,
      })),
    },
    {
      key: 'effort',
      label: 'Effort',
      values: options(claude?.effortLevels.map((m) => m.value) ?? [], choice.effort).map((v) => ({
        value: v,
        label: claude?.effortLevels.find((m) => m.value === v)?.label ?? v,
      })),
    },
  ]

  return (
    <form
      aria-label="The agent for a new conversation"
      onSubmit={(e) => {
        e.preventDefault()
        onStart(choice)
      }}
      className="flex flex-col gap-3 rounded-md border border-border bg-surface px-3 py-3"
    >
      <div className="flex flex-wrap gap-3">
        {fields.map((f) => (
          <div key={f.key} className="flex flex-col gap-1">
            <label htmlFor={`${id}-${f.key}`} className="text-xs font-medium text-muted">
              {f.label}
            </label>
            <select
              id={`${id}-${f.key}`}
              value={choice[f.key]}
              onChange={(e) => {
                setChoice({ ...choice, [f.key]: e.target.value })
              }}
              className={fieldClass}
            >
              <option value="">Inherit</option>
              {f.values.map((v) => (
                <option key={v.value} value={v.value}>
                  {v.label}
                </option>
              ))}
            </select>
          </div>
        ))}
      </div>
      <p className="text-xs text-muted">
        Inherit takes what Settings → Autopilot chooses. Starting with them sets this conversation aside.
      </p>
      <ErrorText error={error} />
      <div className="flex flex-wrap gap-2">
        <button type="submit" disabled={busy} className={primaryButtonClass}>
          Start a new conversation
        </button>
        <button type="button" onClick={onCancel} className={buttonClass}>
          Cancel
        </button>
      </div>
    </form>
  )
}

// DescribeView is Describe it: the conversation with Claude Code in the Autopilot tab. Its
// header says the agent the conversation runs with (Change ▸ starts one with another), what
// it has used and cost, and New conversation; the cards from the user's own Claude Code
// come first; then the conversation, and the composer. A request from elsewhere sends its
// message at once, or puts what it attaches in the composer, once per request.
export function DescribeView({
  request,
  settings,
  catalog,
  onEditInBuilder,
  onShowRoutine,
  onSettingsSaved,
}: DescribeViewProps): ReactElement {
  const talk = useConversation()
  const status = useQuery(() => describeItStatus(), [])
  const [text, setText] = useState('')
  const [chips, setChips] = useState<Chip[]>([])
  const [changing, setChanging] = useState(false)
  const [asking, setAsking] = useState(false)
  const c = talk.conversation
  const started = c !== undefined && c.id !== ''
  const running = c?.running === true
  const turns = c?.turns ?? []
  const agent = started ? c : status.data
  const tokens = c === undefined ? 0 : c.tokensIn + c.tokensOut
  const cost = c === undefined ? '' : costWords(c.cost, c.estimated)
  const kit = {
    defaultMode: settings?.autopilot.defaultMode ?? 'ask',
    catalog,
    onEditInBuilder,
    onShowRoutine,
    // The settings a card leaves reach the window's own, which come back as settings.
    onSettingsSaved,
    describeDelay,
  } satisfies Omit<CardKit, 'onSettled'>

  // attach puts refs in the composer, each once, named as this Mac stores them.
  const attach = (refs: readonly DescribeAttachRef[]) => {
    if (refs.length === 0) {
      return
    }

    void attempt(chipsFor(refs)).then((read) => {
      setChips((was) => withChips(was, read.ok ? read.data : fallbackChips(refs)))
    })
  }

  // send sends words with what refs attach as the next turn, resolving with whether it went;
  // a refusal may be Claude Code's state, which is read again.
  const send = async (words: string, refs: DescribeAttachRef[]) => {
    const sent = await talk.send({ text: words, attach: refs })

    if (!sent.ok) {
      void status.refetch()
    }

    return sent.ok
  }

  const sendComposer = () => {
    const words = text
    const refs = chips.map((chip) => chip.ref)

    void send(words, refs).then((went) => {
      if (went) {
        setText((now) => (now === words ? '' : now))
        setChips((now) => now.filter((chip) => !refs.some((r) => sameRef(r, chip.ref))))
      }
    })
  }

  // A request from elsewhere acts once: its message is sent at once, and what it attaches
  // goes in the composer, or, when its message couldn't be sent, both do.
  const takeRequest = useEffectEvent(() => {
    if (request === undefined || handled.has(request)) {
      return
    }

    handled.add(request)

    const refs = request.attach ?? []
    const words = request.text?.trim() ?? ''

    if (words === '') {
      attach(refs)

      return
    }

    void send(words, refs).then((went) => {
      if (!went) {
        setText(words)
        attach(refs)
      }
    })
  })

  const seq = request?.seq ?? 0

  useEffect(() => {
    takeRequest()
  }, [seq])

  // The newest turn shows as the conversation is read, and as each turn starts: the view
  // scrolls to its end, which the composer sits over.
  const end = useRef<HTMLDivElement>(null)
  const count = turns.length

  useEffect(() => {
    if (count > 0) {
      end.current?.scrollIntoView({ block: 'end' })
    }
  }, [count])

  const startNew = (choice: DescribeAgentChoice) => {
    void talk.startNew(choice).then((made) => {
      if (made.ok) {
        setChanging(false)
        setAsking(false)
      }
    })
  }

  return (
    <section aria-label="Describe it" className="flex flex-col gap-4">
      <header className="flex flex-col gap-1.5">
        <div className="flex flex-wrap items-center gap-3">
          <h2 className="flex items-center gap-2 text-lg font-semibold">
            <SparkIcon className="size-5 shrink-0 text-accent" />
            Describe it
          </h2>
          <button
            type="button"
            disabled={!started || running || talk.starting}
            onClick={() => {
              if (turns.length > 0) {
                setAsking(true)
              } else {
                startNew(inherit)
              }
            }}
            className={`${buttonClass} ml-auto`}
          >
            New conversation
          </button>
        </div>
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted">
          {agent !== undefined && <span>{agentWords(agent)}</span>}
          <button
            type="button"
            aria-expanded={changing}
            disabled={running}
            onClick={() => {
              setChanging(!changing)
            }}
            className={quietButtonClass}
          >
            Change
            {changing ? <ChevronDownIcon className="size-3" /> : <ChevronRightIcon className="size-3" />}
          </button>
          {tokens > 0 && <span>{`${thousands(tokens)} tokens`}</span>}
          {cost !== '' && <span>{cost}</span>}
        </div>
        {changing && (
          <AgentForm
            initial={started ? c.agent : (status.data?.agent ?? inherit)}
            busy={talk.starting}
            error={talk.newError}
            onStart={startNew}
            onCancel={() => {
              setChanging(false)
            }}
          />
        )}
        {!changing && <ErrorText error={talk.newError} />}
      </header>
      <OutsideProposals kit={kit} />
      {talk.error !== '' && c === undefined && <ErrorBanner>{talk.error}</ErrorBanner>}
      {c === undefined ? (
        talk.error === '' && (
          <p role="status" className="text-sm text-muted">
            Reading the conversation…
          </p>
        )
      ) : turns.length === 0 ? (
        <p className="text-sm leading-relaxed text-muted">
          Say what you want Autopilot to do, as you would to a colleague: “when Dependabot’s security scans fail on my
          Go repositories, fix it in the background and tell me how it went”. Claude proposes the routines, prompts, and
          settings it needs as cards, and nothing changes until you apply one.
        </p>
      ) : (
        <Transcript conversation={c} eventsOf={talk.eventsOf} kit={{ ...kit, onSettled: talk.refresh }} />
      )}
      <Composer
        text={text}
        onText={setText}
        chips={chips}
        onRemove={(ref) => {
          setChips(chips.filter((chip) => !sameRef(chip.ref, ref)))
        }}
        running={running}
        sending={talk.sending}
        stopping={talk.stopping}
        error={talk.sendError !== '' ? talk.sendError : talk.stopError}
        status={status.data}
        checking={status.pending}
        onCheck={() => {
          void status.refetch()
        }}
        onSend={sendComposer}
        onStop={talk.stop}
      />
      <div ref={end} />
      {asking && (
        <ConfirmDialog
          title="Start a new conversation?"
          confirmLabel="Start a new conversation"
          tone="primary"
          busy={talk.starting}
          onConfirm={() => {
            startNew(inherit)
          }}
          onCancel={() => {
            setAsking(false)
          }}
        >
          Claude starts afresh, and this conversation is set aside with its cards. What you applied stays applied.
        </ConfirmDialog>
      )}
    </section>
  )
}
