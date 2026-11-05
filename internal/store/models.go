package store

import (
	"slices"

	inboxmodel "github.com/mrz1836/nilbox/internal/inbox/model"
	profilesmodel "github.com/mrz1836/nilbox/internal/profiles/model"
	settingsmodel "github.com/mrz1836/nilbox/internal/settings/model"
)

// AllModels returns every persisted model across the bounded contexts, one per migrated
// application table. The parity test checks it against the migrated schema.
func AllModels() []any {
	return slices.Concat(inboxmodel.AllModels(), settingsmodel.AllModels(), profilesmodel.AllModels())
}
