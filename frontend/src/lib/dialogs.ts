// The system's file dialogs as the pages open them: the path chosen, '' when the user
// cancels, or why the dialog couldn't open, worded for the user.

import { chooseFileToOpen, chooseFileToSave, type FileChoice } from '../platform'
import { attempt, start, type Result } from './call'

// Wording turns why a dialog couldn't open, errorMessage's sentence, into what the page
// shows.
export type Wording = (why: string) => string

// openFailed and saveFailed are how the pages word a file dialog, and a save dialog, that
// couldn't open.
const openFailed: Wording = (why) => `Couldn’t open the file dialog: ${why}`
const saveFailed: Wording = (why) => `Couldn’t open the save dialog: ${why}`

// picked opens a dialog, a throw as it opens failing as a rejection does.
async function picked(dialog: () => PromiseLike<string>, word: Wording): Promise<Result<string>> {
  const chosen = await attempt(start(dialog))

  return chosen.ok ? chosen : { ok: false, error: word(chosen.error) }
}

// pickFileToOpen asks for a file in the system's open dialog, and resolves with its path,
// '' when the user cancels, or the dialog's failure as word puts it.
export function pickFileToOpen(choice: FileChoice, word: Wording = openFailed): Promise<Result<string>> {
  return picked(() => chooseFileToOpen(choice), word)
}

// pickFileToSave asks where to save a file in the system's save dialog, and resolves with
// the path, '' when the user cancels, or the dialog's failure as word puts it.
export function pickFileToSave(choice: FileChoice, word: Wording = saveFailed): Promise<Result<string>> {
  return picked(() => chooseFileToSave(choice), word)
}
