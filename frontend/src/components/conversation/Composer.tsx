import { useId, type ReactNode, type RefObject } from 'react'

import type { LaunchOptions } from '../../lib/launch'
import { runningSession } from '../../lib/sessions'
import type { PickReply } from '../../lib/usePickReply'
import type { AgentSession, InboxItem, ItemResolution } from '../../platform'
import { ErrorText, fieldClass, quietButtonClass } from '../controls'
import { ChevronDownIcon } from '../icons'
import { LaunchControls } from '../LaunchControls'
import { MenuButton, type MenuItem } from '../Menu'

export interface ComposerProps {
  // label names what the user writes, as in "Your review": the section and its text box.
  label: string
  // replies builds the Replies menu's rows as it opens, and picking fills in the one the
  // user picks, which their own typing drops, saying why it couldn't when it fails.
  replies: () => readonly MenuItem[]
  picking: PickReply
  // text is what the box holds, and onText hears the user type, once picking dropped any
  // pick under way.
  text: string
  onText: (text: string) => void
  // textRef is the text box, which a quoted reply focuses.
  textRef?: RefObject<HTMLTextAreaElement | null> | undefined
  // lineComments are what goes with the text, under it, as a review's line comments.
  lineComments?: ReactNode
  // error says why the last action failed, or ''.
  error: string
  // actions are the action button, which the launch follows.
  actions: ReactNode
  // launch, the item, its resolved settings, and its sessions make the launch that hands
  // the item to a coding agent, once the launch options and the item's card are there.
  launch: LaunchOptions | null
  item: InboxItem | undefined
  resolution: ItemResolution | null
  sessions: readonly AgentSession[] | undefined
  // note says what the action does with the text, or ''; noteBlocks marks a note that says
  // why the text can't be posted, which holds the action back.
  note: string
  noteBlocks: boolean
}

// Composer is where the user writes to an item from its sheet, the review composer's and
// the issue composer's alike: the label and the Replies menu over the text (markdown),
// why a picked reply couldn't be filled in, what goes with the text (lineComments), why
// the last action failed, the action button and the launch, and the note on what the
// action does with the text.
export function Composer({
  label,
  replies,
  picking,
  text,
  onText,
  textRef,
  lineComments,
  error,
  actions,
  launch,
  item,
  resolution,
  sessions,
  note,
  noteBlocks,
}: ComposerProps) {
  const id = useId()

  return (
    <section aria-label={label} className="space-y-2">
      <div className="flex items-center justify-between gap-2">
        <label htmlFor={id} className="text-xs font-medium text-muted">
          {label}
        </label>
        <MenuButton label="Choose a reply" items={replies} className={quietButtonClass} align="right">
          Replies <ChevronDownIcon className="size-3" />
        </MenuButton>
      </div>
      <textarea
        ref={textRef}
        id={id}
        rows={5}
        value={text}
        placeholder="Leave a comment (markdown)"
        onChange={(e) => {
          const typed = e.target.value

          picking.drop()
          onText(typed)
        }}
        className={`${fieldClass} block w-full font-mono text-xs`}
      />
      <ErrorText error={picking.error}>Couldn’t fill in the reply: </ErrorText>
      {lineComments}
      <ErrorText error={error} />
      <div className="flex flex-wrap items-center gap-2">
        {actions}
        {launch !== null && item !== undefined && (
          <LaunchControls
            // The sheet steps through its list in place: each item's launch starts from its
            // own settings, never the choices made for the last one.
            key={item.id}
            item={item}
            resolution={resolution}
            {...launch}
            running={runningSession(sessions)}
          />
        )}
      </div>
      {note !== '' && <p className={`text-xs ${noteBlocks ? 'text-warning' : 'text-muted'}`}>{note}</p>}
    </section>
  )
}
