package devtools

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/mrz1836/nilbox/internal/event"
)

// maxEventBytes is how much of an event's JSON an entry keeps; a larger one keeps only
// its size.
const maxEventBytes = 8 << 10

// EventEntry is one event a service emitted: its name, and what it carried as JSON.
type EventEntry struct {
	Time time.Time       `json:"time"`
	Name string          `json:"name"`
	Data json.RawMessage `json:"data,omitempty"`
}

// tee is the Sink Tee returns.
type tee struct {
	next event.Sink
	feed *Feed[EventEntry]
	now  func() time.Time
}

// Tee returns a Sink that keeps every event in feed, stamped with now, then hands it on
// to next.
func Tee(next event.Sink, feed *Feed[EventEntry], now func() time.Time) event.Sink {
	return tee{next: event.Or(next), feed: feed, now: now}
}

// Emit keeps e, then hands it on.
func (t tee) Emit(e event.Event) {
	t.feed.Add(EventEntry{Time: t.now(), Name: e.Name(), Data: eventData(e)})
	t.next.Emit(e)
}

// eventData is e as JSON, or its size when that is more than maxEventBytes, or nothing
// when it has no JSON form.
func eventData(e event.Event) json.RawMessage {
	data, err := json.Marshal(e)
	if err != nil {
		return nil
	}

	if len(data) > maxEventBytes {
		return json.RawMessage(`{"bytes":` + strconv.Itoa(len(data)) + `}`)
	}

	return data
}
