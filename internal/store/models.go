package store

import (
	"slices"

	cleanuplogmodel "github.com/mrz1836/nilbox/internal/cleanuplog/model"
	inboxmodel "github.com/mrz1836/nilbox/internal/inbox/model"
	launchdefaultsmodel "github.com/mrz1836/nilbox/internal/launchdefaults/model"
	launchlogmodel "github.com/mrz1836/nilbox/internal/launchlog/model"
	profilesmodel "github.com/mrz1836/nilbox/internal/profiles/model"
	promptsmodel "github.com/mrz1836/nilbox/internal/prompts/model"
	settingsmodel "github.com/mrz1836/nilbox/internal/settings/model"
)

// AllModels returns every persisted model across the bounded contexts, one per migrated
// application table. The parity test checks it against the migrated schema.
func AllModels() []any {
	return slices.Concat(inboxmodel.AllModels(), settingsmodel.AllModels(), profilesmodel.AllModels(),
		promptsmodel.AllModels(), launchdefaultsmodel.AllModels(), launchlogmodel.AllModels(),
		cleanuplogmodel.AllModels())
}
