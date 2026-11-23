// foldBot writes a bot's login the way the bot lists keep it (the trusted bots and the
// auto-merge bots): lowercase, without an "app/" prefix or "[bot]" suffix.
export function foldBot(login: string): string {
  return login
    .trim()
    .toLowerCase()
    .replace(/^app\//, '')
    .replace(/\[bot\]$/, '')
    .trim()
}
