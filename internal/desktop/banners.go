package desktop

import (
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/notify"
)

// Keys of the data a banner carries back when it is clicked.
const (
	bannerItemKey = "itemId"
	bannerURLKey  = "url"
)

// BannerData is the data a banner carries for b, which the system hands back when the
// banner is clicked: the item it is for, and the item's URL.
func BannerData(b notify.Banner) map[string]any {
	return map[string]any{bannerItemKey: b.ItemID, bannerURLKey: b.URL}
}

// BannerClicked reads the data a clicked banner carried, as BannerData wrote it, into the
// request to reveal its item. A banner that stands for many items names none. The data
// comes back through JSON, so the id is a float64; anything that is not a number or a
// string reads as none.
func BannerClicked(data map[string]any) event.RevealItem {
	var r event.RevealItem

	switch id := data[bannerItemKey].(type) {
	case float64:
		r.ItemID = int64(id)
	case int64:
		r.ItemID = id
	case int:
		r.ItemID = int64(id)
	}

	r.URL, _ = data[bannerURLKey].(string)

	return r
}
