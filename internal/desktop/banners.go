package desktop

import (
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/notify"
)

// Keys of the data a banner carries back when it is clicked.
const (
	bannerItemKey = "itemId"
	bannerURLKey  = "url"
	bannerRunKey  = "runId"
	bannerStarted = "runStarted"
	bannerEntry   = "notificationId"

	// The Autopilot target's keys.
	bannerRoutine = "autopilotRoutine"
	bannerMatch   = "autopilotMatch"
	bannerAgent   = "autopilotRun"
	bannerCreated = "autopilotCreated"
)

// BannerData is the data a banner carries for b, which the system hands back when the
// banner is clicked: the item it is for and the item's URL, or the page or the run of an
// entry about no item, with when the run started, what an Autopilot notification opens in
// the Autopilot tab, and the notification it stands for.
func BannerData(b notify.Banner) map[string]any {
	return map[string]any{
		bannerItemKey: b.ItemID, bannerURLKey: b.URL, bannerRunKey: b.RunID, bannerStarted: b.RunStarted,
		bannerEntry: b.NotificationID, bannerRoutine: b.Autopilot.RoutineID, bannerMatch: b.Autopilot.MatchID,
		bannerAgent: b.Autopilot.RunID, bannerCreated: b.Autopilot.CreatedAt,
	}
}

// BannerClicked reads the data a clicked banner carried, as BannerData wrote it, into the
// request to reveal its item, the run it is about, or what it opens in the Autopilot tab,
// and to mark its notification read. A banner that stands for many items names none. The
// data comes back through JSON, so an id is a float64; an id that is not a number, or a
// URL or a start that is not a string, reads as none.
func BannerClicked(data map[string]any) event.RevealItem {
	r := event.RevealItem{
		ItemID: bannerID(data[bannerItemKey]), RunID: bannerID(data[bannerRunKey]),
		NotificationID: bannerID(data[bannerEntry]),
		Autopilot: event.AutopilotTarget{
			RoutineID: bannerID(data[bannerRoutine]), MatchID: bannerID(data[bannerMatch]),
			RunID: bannerID(data[bannerAgent]),
		},
	}
	r.URL, _ = data[bannerURLKey].(string)
	r.RunStarted, _ = data[bannerStarted].(string)
	r.Autopilot.CreatedAt, _ = data[bannerCreated].(string)

	return r
}

// bannerID reads an id a clicked banner carried, or 0 for one that is not a number.
func bannerID(v any) int64 {
	switch id := v.(type) {
	case float64:
		return int64(id)
	case int64:
		return id
	case int:
		return int64(id)
	default:
		return 0
	}
}
