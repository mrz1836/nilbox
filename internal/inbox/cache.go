package inbox

import (
	"sync/atomic"

	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/trust"
)

// settingsKey is the settings a snapshot depends on beside the inbox's own tables, as
// stored: the trusted bots, which decide each item's trust, and the sensitive-file
// settings, which decide whose files are checked and what each check points out. The
// settings package writes them, so the inbox reads them back to tell whether a snapshot
// or a compiled scope still holds.
type settingsKey struct {
	bots       string
	botsStored bool
	scope      string
	off        string
	patterns   string
}

// settingsPrefs are the prefs keys a settingsKey is read from.
func settingsPrefs() []string {
	return []string{trust.PrefBots, sensitive.PrefScope, sensitive.PrefOff, sensitive.PrefPatterns}
}

// keyOf reads the settingsKey from stored prefs that include settingsPrefs.
func keyOf(stored map[string]string) settingsKey {
	bots, botsStored := stored[trust.PrefBots]

	return settingsKey{
		bots: bots, botsStored: botsStored, scope: stored[sensitive.PrefScope], off: stored[sensitive.PrefOff],
		patterns: stored[sensitive.PrefPatterns],
	}
}

// snapshotCache keeps the last snapshot built, with the settings it was built under,
// until a write empties it. A snapshot depends on nothing but what is stored: the time
// is applied by its readers (Item.Snoozed, Snapshot.NeedsYou), so a kept snapshot never
// goes stale while nothing is written, however long it is kept.
//
// The cache is emptied after every write commits. A snapshot is kept only if the cache
// is as it was before the snapshot was read: a snapshot read across a write is returned
// to its caller but not kept, so the cache never holds one older than the last write.
type snapshotCache struct {
	current atomic.Pointer[cachedSnapshot]
}

// cachedSnapshot is a kept snapshot and the settings it was built under. A nil snap is a
// cache a write emptied.
type cachedSnapshot struct {
	snap     *Snapshot
	settings settingsKey
}

// load returns the cache as it is now, which a later keep compares against.
func (c *snapshotCache) load() *cachedSnapshot {
	return c.current.Load()
}

// keep keeps snap, read after load returned seen, unless the cache changed since.
func (c *snapshotCache) keep(seen *cachedSnapshot, snap Snapshot, settings settingsKey) {
	c.current.CompareAndSwap(seen, &cachedSnapshot{snap: &snap, settings: settings})
}

// empty drops the kept snapshot once a write has committed, and with it any snapshot
// being read meanwhile.
func (c *snapshotCache) empty() {
	c.current.Store(&cachedSnapshot{})
}

// scopeCache keeps the file scope last compiled, with the settings it was compiled from,
// so the user's categories are compiled once per change of the settings instead of for
// every snapshot and file check.
type scopeCache struct {
	current atomic.Pointer[compiledScope]
}

// compiledScope is a file scope and the settings it was compiled from.
type compiledScope struct {
	settings settingsKey
	scope    fileScope
}

// scope returns the file scope of the stored status (see newFileScope), compiling it only
// when its settings differ from the last ones compiled.
func (c *scopeCache) scope(status map[string]string) fileScope {
	key := keyOf(status)

	compiled := c.current.Load()
	if compiled == nil || compiled.settings != key {
		compiled = &compiledScope{settings: key, scope: newFileScope(status)}
		c.current.Store(compiled)
	}

	scope := compiled.scope
	scope.viewer = status[prefViewer]

	return scope
}
