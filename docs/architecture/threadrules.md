# internal/threadrules

`internal/threadrules/` holds the rules for the user's GitHub notifications (`threadrules.*`:
whether they are read, whether they are cleared on their own, marked done or read, by which
triggers, and whether a watched repository's or a bot's activity counts), which settings
alone writes (each rule's setter emitting the Go-only `event.ThreadRulesChanged` once it
is stored, which core's relay turns into the ghthreads `Syncer`'s `RulesChanged`), and the
pure `Decide`, which says whether one thread is cleared, skipped, or held at the moment its
item was finished; it is a leaf that imports only `internal/item` and `prefval`.
