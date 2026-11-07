package store

import (
	"slices"

	inboxmodel "github.com/mrz1836/nilbox/internal/inbox/model"
	launchdefaultsmodel "github.com/mrz1836/nilbox/internal/launchdefaults/model"
	notifymodel "github.com/mrz1836/nilbox/internal/notify/model"
	profilesmodel "github.com/mrz1836/nilbox/internal/profiles/model"
	promptsmodel "github.com/mrz1836/nilbox/internal/prompts/model"
	settingsmodel "github.com/mrz1836/nilbox/internal/settings/model"
	worklogmodel "github.com/mrz1836/nilbox/internal/worklog/model"
)

// AllModels returns every persisted model across the bounded contexts, one per migrated
// application table. The parity test checks it against the migrated schema.
func AllModels() []any {
	return slices.Concat(inboxmodel.AllModels(), settingsmodel.AllModels(), profilesmodel.AllModels(),
		promptsmodel.AllModels(), launchdefaultsmodel.AllModels(), worklogmodel.AllModels(),
		notifymodel.AllModels())
}
