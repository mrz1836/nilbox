//go:build desktop

package main

import "github.com/mrz1836/nilbox/internal/notify"

// Keys of the data a banner carries back when it is clicked.
const (
	bannerItemKey = "itemId"
	bannerURLKey  = "url"
)

// poster is the notification center core posts banners to, or nil when this build cannot
// show them; a nil interface, never a nil *banners, so core sees that there is none.
func (b *banners) poster() notify.Poster {
	if b == nil {
		return nil
	}

	return b
}

// bannerTarget reads the item a clicked banner was for from the data it carried: the
// item's id, 0 for none, and its URL. The data comes back through JSON, so the id is a
// float64.
func bannerTarget(data map[string]any) (itemID int64, url string) {
	switch id := data[bannerItemKey].(type) {
	case float64:
		itemID = int64(id)
	case int64:
		itemID = id
	case int:
		itemID = int64(id)
	}

	url, _ = data[bannerURLKey].(string)

	return itemID, url
}
