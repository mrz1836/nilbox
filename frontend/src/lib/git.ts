// Git's names as the window shows them, and the commands it offers to copy, which the app
// never runs itself.

// shortCommit is a commit's ID as git and GitHub abbreviate it: its first seven characters.
export function shortCommit(oid: string): string {
  return oid.slice(0, 7)
}

// plainWord matches a word a POSIX shell reads as it is, with no character it treats
// specially.
const plainWord = /^[\w@%+=:,./-]+$/

// shellQuoted quotes text as one word for a POSIX shell: as it is when no character in it
// means anything to the shell, and otherwise in single quotes, each single quote inside
// closed, escaped, and reopened. An empty word is two quotes.
export function shellQuoted(text: string): string {
  return plainWord.test(text) ? text : `'${text.replaceAll("'", `'\\''`)}'`
}
