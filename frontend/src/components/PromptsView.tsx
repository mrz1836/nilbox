import { useEffect, useEffectEvent, useId, useRef, useState } from 'react'

import { brand } from '../generated/brand'
import { counted, errorMessage } from '../lib/format'
import { isFindKey } from '../lib/keys'
import { kindPlural, promptApplies, usageCount, usagePlaces } from '../lib/launch'
import { replyDefaultsText, replySlotPlaces, replySlots } from '../lib/replies'
import { useMutation } from '../lib/useMutation'
import { useDebouncedQuery } from '../lib/useQuery'
import {
  checkPrompt,
  chooseFileToOpen,
  chooseFileToSave,
  createPrompt,
  deletePrompt,
  duplicatePrompt,
  exportPrompts,
  resetPrompt,
  setDefaultPrompt,
  setReplyDefault,
  updatePrompt,
  type AgentInfo,
  type DefaultPrompts,
  type Diagnostic,
  type ImportResult,
  type InboxItem,
  type PerAgent,
  type Prompt,
  type PromptInput,
  type PromptPurpose,
  type PromptVariable,
} from '../platform'
import { ConfirmDialog, type ConfirmDialogProps } from './ConfirmDialog'
import { buttonClass, primaryButtonClass } from './controls'
import {
  CopyIcon,
  DocumentIcon,
  ExportIcon,
  HistoryIcon,
  ImportIcon,
  MoreIcon,
  PlusIcon,
  ResetIcon,
  StarIcon,
  TrashIcon,
} from './icons'
import { ImportPromptsDialog, type ImportStart } from './ImportPromptsDialog'
import { MenuButton, type MenuItem } from './Menu'
import { PromptEditor } from './PromptEditor'
import { PromptLibrary, type Selection } from './PromptLibrary'
import { PromptPreview } from './PromptPreview'
import { TabPanel, Tabs } from './Tabs'
import type { TemplateEditorHandle } from './TemplateEditor'
import { VariablesPanel } from './VariablesPanel'

export interface PromptsViewProps {
  // agents are the coding agents, whose models and flags a prompt can set.
  agents: readonly AgentInfo[]
  // prompts are the agent prompts, and replies the review replies, each as the library
  // lists them.
  prompts: readonly Prompt[]
  replies: readonly Prompt[]
  defaults: DefaultPrompts | null
  variables: readonly PromptVariable[]
  // items are the inbox's items, offered to preview a prompt with.
  items: readonly InboxItem[]
  // onChanged reloads the prompts and everything that depends on them; it resolves
  // once they are loaded again.
  onChanged: () => Promise<void>
}

// Library is the whole prompt library: its agent prompts and its review replies.
interface Library {
  prompts: readonly Prompt[]
  replies: readonly Prompt[]
}

// Pending is a question the page is asking before it acts.
type Pending =
  | { kind: 'discard'; then: Selection | null; mode: PromptPurpose }
  | { kind: 'delete'; prompt: Prompt }
  | { kind: 'in-use'; prompt: Prompt }
  | { kind: 'reset'; prompt: Prompt }

// Question is what the page asks before acting on a Pending: the confirm dialog's words,
// and what confirming does.
type Question = Omit<ConfirmDialogProps, 'onCancel'>

// sidePanels are the tabs beside the editor.
const sidePanels = [
  { value: 'preview', label: 'Preview' },
  { value: 'variables', label: 'Variables' },
] as const

// newPromptBody starts a new prompt with the item's address, so the variables show at
// work from the first keystroke.
const newPromptBody = 'Look at {{.Kind}} #{{.Number}} in {{.Repo}}: {{.Title}}\n{{.URL}}\n\n'

const blankInput: PromptInput = {
  name: '',
  nickname: '',
  appliesTo: 'pr',
  body: newPromptBody,
  purpose: 'agent',
  replyAction: '',
  models: {},
  effort: '',
  planMode: null,
  worktree: null,
  extraFlags: {},
}

// newReplyBody starts a new reply by thanking the pull request's author.
const newReplyBody = 'Thanks, @{{.Author}}!\n'

// blankReply is a new review reply: an approval, which applies to pull requests and sets
// nothing for a launch.
const blankReply: PromptInput = { ...blankInput, body: newReplyBody, purpose: 'reply', replyAction: 'approve' }

// nounOf names what a prompt is: a prompt, or a reply.
function nounOf(purpose: PromptPurpose): string {
  return purpose === 'reply' ? 'reply' : 'prompt'
}

// toInput is a stored prompt as the editor holds it.
function toInput(p: Prompt): PromptInput {
  return {
    name: p.name,
    nickname: p.nickname,
    appliesTo: p.appliesTo,
    body: p.body,
    purpose: p.purpose,
    replyAction: p.replyAction,
    models: { ...p.models },
    effort: p.effort,
    planMode: p.planMode,
    worktree: p.worktree,
    extraFlags: { ...p.extraFlags },
  }
}

// samePerAgent reports whether two per-agent settings are the same, a blank value and no
// entry being the same.
function samePerAgent(a: PerAgent<string>, b: PerAgent<string>): boolean {
  const keys = new Set([...Object.keys(a), ...Object.keys(b)]) as Set<keyof PerAgent<string>>

  return [...keys].every((k) => (a[k] ?? '').trim() === (b[k] ?? '').trim())
}

// sameInput reports whether two drafts hold the same prompt.
function sameInput(a: PromptInput, b: PromptInput): boolean {
  return (
    a.name === b.name &&
    a.nickname === b.nickname &&
    a.appliesTo === b.appliesTo &&
    a.replyAction === b.replyAction &&
    a.body === b.body &&
    samePerAgent(a.models, b.models) &&
    a.effort === b.effort &&
    a.planMode === b.planMode &&
    a.worktree === b.worktree &&
    samePerAgent(a.extraFlags, b.extraFlags)
  )
}

// promptFiles is the type of file prompts are exported to and imported from. An import
// also reads a copy of the app's database, such as one restored from a backup.
const promptFiles = [{ name: `${brand.name} prompts`, pattern: '*.json' }]
const importFiles = [...promptFiles, { name: `${brand.name} database`, pattern: '*.db' }]

// datePart is a date as a file name carries it: 2026-11-08.
function datePart(now: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0')

  return `${String(now.getFullYear())}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`
}

// fileSafe is text with the characters a file name cannot hold replaced.
function fileSafe(text: string): string {
  return text.replace(/[/\\:*?"<>|]+/g, '-').trim()
}

// baseName is the last part of a path.
function baseName(path: string): string {
  return path.split(/[\\/]/).at(-1) ?? path
}

// importedText says what an import, or a restore from a backup, did.
function importedText(result: ImportResult, restored: boolean): string {
  const done = `${restored ? 'Restored' : 'Imported'} ${counted(result.added + result.replaced, 'prompt', 'prompts')}.`

  if (result.skipped === 0) {
    return done
  }

  return `${done} ${result.skipped === 1 ? 'One was' : `${String(result.skipped)} were`} already in your library.`
}

// touchedField reports whether a field of the draft was edited away from its start.
function touchedField(field: string, draft: PromptInput, start: PromptInput): boolean {
  const key = field as keyof PromptInput

  return key in draft && draft[key] !== start[key]
}

// PromptsView is the Prompts page: the prompt library on the left, the selected prompt's
// editor in the middle, and its live preview and the variables cheat sheet on the right.
// The library shows either the agent prompts or the review replies, and a stored prompt
// always opens in its own part. Edits are checked by the backend as you type and saved
// with Save (or ⌘S). Leaving a prompt with unsaved changes asks first.
export function PromptsView({ agents, prompts, replies, defaults, variables, items, onChanged }: PromptsViewProps) {
  const id = useId()
  const editorRef = useRef<TemplateEditorHandle>(null)
  // searchRef is the library's search field, which ⌘F focuses.
  const searchRef = useRef<HTMLInputElement>(null)
  // mode is the part of the library on show: the agent prompts or the review replies.
  const [mode, setMode] = useState<PromptPurpose>('agent')
  const [selection, setSelection] = useState<Selection | null>(null)
  const [baseline, setBaseline] = useState<PromptInput>(blankInput)
  const [draft, setDraft] = useState<PromptInput>(blankInput)
  // opened counts the prompts opened in the editor, so a check made for one never shows
  // on the next.
  const [opened, setOpened] = useState(0)
  // pickedItem is the preview item picked in the panel, or null to pick automatically.
  const [pickedItem, setPickedItem] = useState<number | null>(null)
  const [inWorktree, setInWorktree] = useState(true)
  const [atPrHead, setAtPrHead] = useState(true)
  const [panel, setPanel] = useState<'preview' | 'variables'>('preview')
  const [status, setStatus] = useState<{ tone: 'error' | 'success'; text: string } | null>(null)
  const [pending, setPending] = useState<Pending | null>(null)
  // importing is the import under way in its dialog, or null.
  const [importing, setImporting] = useState<ImportStart | null>(null)

  const replyMode = mode === 'reply'
  const noun = nounOf(mode)
  // listed is the part of the library on show.
  const listed = replyMode ? replies : prompts

  // applicableItems are the inbox items the draft applies to, which it can be previewed
  // with: a reply applies to pull requests.
  const applicableItems = items.filter((item) => promptApplies({ appliesTo: draft.appliesTo }, item.kind))

  // previewItem is the item chosen to preview with: the one picked while the prompt
  // applies to it (0 for the sample), else the first item it applies to.
  const previewItem =
    pickedItem !== null && (pickedItem === 0 || applicableItems.some((item) => item.id === pickedItem))
      ? pickedItem
      : (applicableItems[0]?.id ?? 0)

  // The draft is checked, once typing rests, whenever it or the preview changes.
  const checked = useDebouncedQuery(
    () =>
      checkPrompt({
        id: selection?.kind === 'prompt' ? selection.id : 0,
        input: draft,
        itemId: previewItem,
        // A reply starts no session.
        inWorktree: !replyMode && inWorktree,
        atPrHead: !replyMode && atPrHead,
      }).then((result) => ({ opened, result })),
    [selection, draft, previewItem, inWorktree, atPrHead, replyMode, opened],
    150,
    { enabled: selection !== null },
  )
  const check = checked.data?.opened === opened ? checked.data.result : null
  const checkError = !checked.pending && checked.error !== '' ? `Could not check the prompt: ${checked.error}` : ''

  const saved = selection?.kind === 'prompt' ? (listed.find((p) => p.id === selection.id) ?? null) : null
  const dirty = selection !== null && !sameInput(draft, baseline)
  // A new prompt's empty fields are not problems to point at until they are edited.
  const shownFields = (check?.fields ?? []).filter(
    (f) => selection?.kind !== 'new' || touchedField(f.field, draft, blankInput),
  )
  const fieldErrors = Object.fromEntries(shownFields.map((f) => [f.field, f.message])) as Record<string, string>
  const diagnostics = check?.diagnostics ?? []
  const hiddenProblems = (check?.fields.length ?? 0) - shownFields.length
  const problemCount = shownFields.length + diagnostics.filter((d: Diagnostic) => d.severity === 'error').length

  // open shows a prompt from lists in the editor, a new one, or none, and reports whether
  // it could: a stored prompt must be in the library, and shows in its own part of it;
  // anything else shows in the part in.
  const open = (lists: Library, next: Selection | null, part: PromptPurpose): boolean => {
    const stored =
      next?.kind === 'prompt' ? [...lists.prompts, ...lists.replies].find((p) => p.id === next.id) : undefined

    if (next?.kind === 'prompt' && stored === undefined) {
      return false
    }

    const shows = stored?.purpose ?? part
    const input = stored === undefined ? (shows === 'reply' ? blankReply : blankInput) : toInput(stored)

    setMode(shows)
    setSelection(next)
    setBaseline(input)
    setDraft(input)
    setOpened((n) => n + 1)

    return true
  }

  const select = (next: Selection | null, part: PromptPurpose = mode) => {
    open({ prompts, replies }, next, part)
  }

  // afterReload is the prompt an action asked to open once the library reloads.
  const [afterReload, setAfterReload] = useState<Selection | null>(null)
  // library is the library last seen, to notice when a reload brings a new one.
  const [library, setLibrary] = useState<Library | null>(null)

  // Whenever the library loads, open the prompt an action asked for, or else the first
  // one on show when none is open or the open one was deleted, or none when nothing is
  // left on show. This adjusts state while rendering, as React recommends for reacting
  // to a changed prop.
  if (library === null || library.prompts !== prompts || library.replies !== replies) {
    const lists = { prompts, replies }

    setLibrary(lists)

    const wanted = afterReload !== null && open(lists, afterReload, mode)

    if (wanted) {
      setAfterReload(null)
    } else if (selection === null || (selection.kind === 'prompt' && !listed.some((p) => p.id === selection.id))) {
      const first = listed[0]

      if (first !== undefined) {
        open(lists, { kind: 'prompt', id: first.id }, mode)
      } else if (selection !== null) {
        open(lists, null, mode)
      }
    }
  }

  // ask switches prompts, or to another part of the library, asking first when the
  // current prompt has unsaved changes.
  const ask = (next: Selection | null, part: PromptPurpose = mode) => {
    if (dirty) {
      setPending({ kind: 'discard', then: next, mode: part })
    } else {
      select(next, part)
    }
  }

  // switchMode shows the other part of the library, opening its first prompt, or none
  // when it has none.
  const switchMode = (part: PromptPurpose) => {
    const first = (part === 'reply' ? replies : prompts)[0]

    ask(first === undefined ? null : { kind: 'prompt', id: first.id }, part)
  }

  // saver stores the draft as a new prompt or over the open one, then reloads the library.
  const saver = useMutation(async (at: Selection, input: PromptInput) => {
    const stored = await (at.kind === 'new' ? createPrompt(input) : updatePrompt(at.id, input))

    await onChanged()

    return stored
  })
  // runner performs an action on the library and reloads it, opening the prompt the
  // action names, if any, once the reload brings it in.
  const runner = useMutation(async (action: () => Promise<Selection | null>) => {
    setAfterReload(await action())
    await onChanged()
  })
  const busy = saver.pending || runner.pending

  const save = async () => {
    if (selection === null || busy) {
      return
    }

    setStatus(null)

    // submitted is what is being saved; edits typed while it saves are kept.
    const submitted = draft
    const result = await saver.run(selection, submitted)

    if (!result.ok) {
      setStatus({ tone: 'error', text: result.error })

      return
    }

    const stored = result.data

    setSelection({ kind: 'prompt', id: stored.id })
    setBaseline(toInput(stored))
    setDraft((current) => (sameInput(current, submitted) ? toInput(stored) : current))
    setStatus({
      tone: 'success',
      text: selection.kind === 'new' ? `${replyMode ? 'Reply' : 'Prompt'} created.` : 'Saved.',
    })
  }

  const canSave = dirty && problemCount === 0 && hiddenProblems === 0 && !busy

  // ⌘S saves while the page is showing, and ⌘F finds a prompt in the library: a page
  // hidden behind another runs no effects, so it stops listening.
  const saveOnKey = useEffectEvent((e: globalThis.KeyboardEvent) => {
    if (e.metaKey && e.key === 's') {
      e.preventDefault()

      if (canSave) {
        void save()
      }
    } else if (isFindKey(e)) {
      e.preventDefault()
      searchRef.current?.focus()
      searchRef.current?.select()
    }
  })

  useEffect(() => {
    const onKeyDown = (e: globalThis.KeyboardEvent) => {
      saveOnKey(e)
    }

    document.addEventListener('keydown', onKeyDown)

    return () => {
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [])

  // run performs an action on the library and says how it went.
  const run = async (action: () => Promise<Selection | null>, done: string) => {
    setStatus(null)

    const result = await runner.run(action)

    if (result.ok) {
      setStatus({ tone: 'success', text: done })
    } else {
      setAfterReload(null)
      setStatus({ tone: 'error', text: result.error })
    }
  }

  const exporter = useMutation((path: string, ids: number[]) => exportPrompts(path, ids))

  // exportTo asks where to save the prompts with ids (every prompt when ids is empty),
  // offering filename, then writes them there.
  const exportTo = async (ids: number[], filename: string) => {
    setStatus(null)

    let path: string

    try {
      path = await chooseFileToSave({ title: 'Export prompts', button: 'Export', filters: promptFiles, filename })
    } catch (err) {
      setStatus({ tone: 'error', text: `Couldn’t open the save dialog: ${errorMessage(err)}` })

      return
    }

    if (path === '') {
      return
    }

    const result = await exporter.run(path, ids)

    setStatus(
      result.ok
        ? { tone: 'success', text: `Exported ${counted(result.data, 'prompt', 'prompts')} to “${baseName(path)}”.` }
        : { tone: 'error', text: result.error },
    )
  }

  // importFile asks for a prompt file, then shows what importing it would do.
  const importFile = async () => {
    setStatus(null)

    try {
      const path = await chooseFileToOpen({ title: 'Import prompts', button: 'Import', filters: importFiles })

      if (path !== '') {
        setImporting({ kind: 'file', path })
      }
    } catch (err) {
      setStatus({ tone: 'error', text: `Couldn’t open the file dialog: ${errorMessage(err)}` })
    }
  }

  // An import opens the prompt it brought in, so unsaved changes are dealt with first.
  const unsavedTitle = dirty ? 'Save or discard your changes first.' : undefined

  // libraryActions are what can be done with the whole library.
  const libraryActions: MenuItem[] = [
    {
      label: 'Import from a file…',
      icon: <ImportIcon className="size-4 text-muted" />,
      disabled: dirty,
      title: unsavedTitle,
      onSelect: () => {
        void importFile()
      },
    },
    {
      label: 'Restore from a backup…',
      icon: <HistoryIcon className="size-4 text-muted" />,
      disabled: dirty,
      title: unsavedTitle,
      onSelect: () => {
        setStatus(null)
        setImporting({ kind: 'backups' })
      },
    },
    {
      label: 'Export all prompts…',
      icon: <ExportIcon className="size-4 text-muted" />,
      divided: true,
      disabled: prompts.length + replies.length === 0,
      onSelect: () => {
        void exportTo([], `${brand.name} prompts ${datePart(new Date())}.json`)
      },
    },
  ]

  // shown is the status the footer shows: a failed action, then a failed check, then how
  // the last action went.
  const shown = status?.tone === 'error' || checkError === '' ? status : { tone: 'error' as const, text: checkError }

  // replyDefaultItems offers to make a reply the default for each review it is written
  // for, where it is not already.
  const replyDefaultItems = (p: Prompt): MenuItem[] =>
    replySlots(p.replyAction)
      .filter((slot) => !p.usage.replies.includes(slot))
      .map((slot) => ({
        label: `Make the default for ${replySlotPlaces[slot]}`,
        icon: <StarIcon className="size-4 text-muted" />,
        onSelect: () => {
          void run(async () => {
            await setReplyDefault(slot, p.id)

            return null
          }, `“${p.nickname}” is now the default for ${replySlotPlaces[slot]}.`)
        },
      }))

  // promptDefaultItems offers to make an agent prompt the app's default for each kind of
  // item it applies to, where it is not already.
  const promptDefaultItems = (p: Prompt): MenuItem[] =>
    (['pr', 'issue'] as const)
      .filter((kind) => promptApplies(p, kind) && (kind === 'pr' ? defaults?.pr : defaults?.issue) !== p.id)
      .map((kind) => ({
        label: `Make the default for ${kindPlural(kind)}`,
        icon: <StarIcon className="size-4 text-muted" />,
        onSelect: () => {
          void run(
            async () => {
              await setDefaultPrompt(kind, p.id)

              return null
            },
            `“${p.nickname}” is now the default for ${kindPlural(kind)}.`,
          )
        },
      }))

  const menuItems = (p: Prompt): MenuItem[] => {
    const items = p.purpose === 'reply' ? replyDefaultItems(p) : promptDefaultItems(p)

    items.push({
      label: 'Duplicate',
      icon: <CopyIcon className="size-4 text-muted" />,
      disabled: dirty,
      title: dirty ? 'Save or discard your changes first.' : undefined,
      onSelect: () => {
        void run(async () => {
          const copy = await duplicatePrompt(p.id)

          return { kind: 'prompt', id: copy.id }
        }, `Duplicated “${p.nickname}”.`)
      },
    })

    items.push({
      label: 'Export…',
      icon: <ExportIcon className="size-4 text-muted" />,
      title: dirty ? 'Exports it as last saved.' : undefined,
      onSelect: () => {
        void exportTo([p.id], `${fileSafe(p.nickname)}.json`)
      },
    })

    if (p.modified) {
      items.push({
        label: 'Reset to original',
        icon: <ResetIcon className="size-4 text-muted" />,
        onSelect: () => {
          setPending({ kind: 'reset', prompt: p })
        },
      })
    }

    items.push({
      label: 'Delete…',
      icon: <TrashIcon className="size-4" />,
      danger: true,
      onSelect: () => {
        setPending(usageCount(p.usage) > 0 ? { kind: 'in-use', prompt: p } : { kind: 'delete', prompt: p })
      },
    })

    return items
  }

  // question words the dialog for a pending question, and what confirming it does.
  const question = (asked: Pending): Question => {
    switch (asked.kind) {
      case 'discard':
        return {
          title: 'Discard your changes?',
          confirmLabel: 'Discard changes',
          cancelLabel: 'Keep editing',
          children:
            selection?.kind === 'new'
              ? `The new ${noun} hasn’t been created yet.`
              : `“${baseline.nickname}” has changes that aren’t saved.`,
          onConfirm: () => {
            select(asked.then, asked.mode)
          },
        }
      case 'delete': {
        const what = nounOf(asked.prompt.purpose)
        const gone =
          asked.prompt.starterKey !== ''
            ? `It’s a starter ${what}, and it won’t be added back.`
            : 'This can’t be undone.'
        // A reply that is a default can go: the review then has no default reply.
        const slots = asked.prompt.usage.replies
        const undefaulted = slots.length > 0 ? ` It stops being the default for ${replyDefaultsText(slots)}.` : ''

        return {
          title: `Delete “${asked.prompt.nickname}”?`,
          confirmLabel: `Delete ${what}`,
          children: gone + undefaulted,
          onConfirm: () => {
            void run(async () => {
              await deletePrompt(asked.prompt.id)

              return null
            }, `Deleted “${asked.prompt.nickname}”.`)
          },
        }
      }
      case 'in-use':
        return {
          title: `“${asked.prompt.nickname}” is still in use`,
          confirmLabel: 'Got it',
          tone: 'primary',
          cancelLabel: null,
          children: (
            <>
              <p>A prompt can be deleted once nothing uses it as a default. Choose another default here first:</p>
              <ul className="mt-2 list-disc pl-5 text-text">
                {usagePlaces(asked.prompt.usage).map((place) => (
                  <li key={place}>{place}</li>
                ))}
              </ul>
              <p className="mt-2">
                App defaults are in Settings → Launching; org and repo defaults in Settings → Repositories.
              </p>
            </>
          ),
          onConfirm: () => {
            // Acknowledging is all there is to do.
          },
        }
      default: {
        const reply = asked.prompt.purpose === 'reply'

        return {
          title: `Reset “${asked.prompt.nickname}”?`,
          confirmLabel: 'Reset to original',
          tone: 'primary',
          children: `Its title, text, and ${reply ? 'action' : 'launch settings'} go back to how the app ships them.${dirty ? ' Your unsaved changes are discarded too.' : ''}`,
          onConfirm: () => {
            void run(
              async () => {
                await resetPrompt(asked.prompt.id)

                return { kind: 'prompt', id: asked.prompt.id }
              },
              `Reset “${asked.prompt.nickname}” to its original text and ${reply ? 'action' : 'settings'}.`,
            )
          },
        }
      }
    }
  }

  const asking = pending === null ? null : question(pending)

  const previewFor = (() => {
    const item = items.find((i) => i.id === previewItem)

    return item === undefined ? 'the sample' : `#${String(item.number)}`
  })()

  const forPRs = (() => {
    const item = items.find((i) => i.id === previewItem)

    return item === undefined ? draft.appliesTo !== 'issue' : item.kind === 'pr'
  })()

  return (
    <div className="flex h-full min-h-0">
      <PromptLibrary
        mode={mode}
        onModeChange={switchMode}
        prompts={listed}
        selection={selection}
        newNickname={draft.nickname}
        dirty={dirty}
        onAsk={(next) => {
          ask(next)
        }}
        actions={libraryActions}
        searchRef={searchRef}
      />

      <section aria-label="Prompt editor" className="flex min-w-0 flex-1 flex-col">
        {selection === null ? (
          <div className="flex flex-1 flex-col items-center justify-center gap-3 text-center">
            <DocumentIcon className="size-8 text-muted" />
            <p className="text-sm text-muted">{replyMode ? 'No replies yet.' : 'No prompts yet.'}</p>
            <div className="flex gap-2">
              <button
                type="button"
                onClick={() => {
                  select({ kind: 'new' })
                }}
                className={primaryButtonClass}
              >
                <PlusIcon className="size-4" />
                New {noun}
              </button>
              <button
                type="button"
                onClick={() => {
                  setImporting({ kind: 'backups' })
                }}
                className={buttonClass}
              >
                <HistoryIcon className="size-4" />
                Restore from a backup
              </button>
            </div>
          </div>
        ) : (
          <>
            <div className="flex items-center justify-end gap-2 px-6 pt-4">
              {saved?.starterKey !== '' && saved !== null && (
                <span className="mr-auto text-xs text-muted">
                  Starter {noun}
                  {saved.modified ? ' · changed' : ''}
                </span>
              )}
              {saved !== null && (
                <MenuButton
                  label={`More actions for ${saved.nickname}`}
                  title="More actions"
                  items={menuItems(saved)}
                  className="flex size-8 items-center justify-center rounded-md text-muted transition-colors hover:bg-raised hover:text-text aria-expanded:bg-raised"
                >
                  <MoreIcon className="size-4" />
                </MenuButton>
              )}
            </div>
            <PromptEditor
              agents={agents}
              draft={draft}
              onChange={(next) => {
                setDraft(next)
                setStatus(null)
              }}
              saved={saved}
              fieldErrors={fieldErrors}
              diagnostics={diagnostics}
              variables={variables}
              editorRef={editorRef}
              checked={check !== null}
              onReveal={(d) => {
                editorRef.current?.reveal(d.line, d.column, d.length)
              }}
            />
            <footer className="flex items-center justify-end gap-2 border-t border-border px-6 py-3">
              <p
                role={shown?.tone === 'error' ? 'alert' : 'status'}
                className={`mr-auto min-w-0 truncate text-xs ${shown?.tone === 'error' ? 'text-danger' : shown?.tone === 'success' && !dirty ? 'text-success' : 'text-muted'}`}
                title={shown?.text}
              >
                {shown?.tone === 'error'
                  ? shown.text
                  : busy
                    ? 'Saving…'
                    : dirty && problemCount > 0
                      ? `Fix ${String(problemCount)} ${problemCount === 1 ? 'problem' : 'problems'} to save.`
                      : hiddenProblems > 0
                        ? `Give the ${noun} a title and a nickname to create it.`
                        : dirty
                          ? 'Unsaved changes'
                          : (shown?.text ?? '')}
              </p>
              {dirty && (
                <button
                  type="button"
                  disabled={busy}
                  onClick={() => {
                    if (selection.kind === 'new') {
                      const first = listed[0]

                      select(first === undefined ? null : { kind: 'prompt', id: first.id })
                    } else {
                      setDraft(baseline)
                    }
                  }}
                  className={buttonClass}
                >
                  {selection.kind === 'new' ? 'Cancel' : 'Discard'}
                </button>
              )}
              <button
                type="button"
                disabled={!canSave}
                onClick={() => {
                  void save()
                }}
                title="Save (⌘S)"
                className={primaryButtonClass}
              >
                {selection.kind === 'new' ? `Create ${noun}` : 'Save'}
              </button>
            </footer>
          </>
        )}
      </section>

      {selection !== null && (
        <aside
          aria-label="Preview and variables"
          className="hidden w-80 shrink-0 flex-col border-l border-border lg:flex"
        >
          <Tabs
            baseId={id}
            label="Side panel"
            tabs={sidePanels}
            value={panel}
            onChange={setPanel}
            className="flex gap-1 border-b border-border px-3 pt-3"
          />
          <TabPanel baseId={id} value={panel} className="flex min-h-0 flex-1 flex-col">
            {panel === 'preview' ? (
              <PromptPreview
                items={applicableItems}
                itemId={previewItem}
                onItemChange={setPickedItem}
                inWorktree={inWorktree}
                onInWorktreeChange={setInWorktree}
                atPrHead={atPrHead}
                onAtPrHeadChange={setAtPrHead}
                forPRs={forPRs}
                result={check}
                hasErrors={diagnostics.some((d) => d.severity === 'error')}
                reply={replyMode}
              />
            ) : (
              <VariablesPanel
                variables={variables}
                values={check?.values ?? null}
                valuesFor={previewFor}
                forIssuesOnly={draft.appliesTo === 'issue'}
                onInsertVariable={(name) => {
                  editorRef.current?.insertVariable(name)
                }}
                onInsertText={(text) => {
                  editorRef.current?.insertText(text)
                }}
              />
            )}
          </TabPanel>
        </aside>
      )}

      {importing !== null && (
        <ImportPromptsDialog
          start={importing}
          agents={agents}
          onCancel={() => {
            setImporting(null)
          }}
          onImported={(result) => {
            const restored = importing.kind === 'backups'

            setImporting(null)
            void run(
              () => Promise.resolve(result.first === 0 ? null : { kind: 'prompt' as const, id: result.first }),
              importedText(result, restored),
            )
          }}
        />
      )}

      {asking !== null && (
        <ConfirmDialog
          {...asking}
          onCancel={() => {
            setPending(null)
          }}
          onConfirm={() => {
            setPending(null)
            asking.onConfirm()
          }}
        />
      )}
    </div>
  )
}
