package desktop

import (
	"net/url"

	"github.com/mrz1836/nilbox/internal/settings"
)

// TextSizeParam is the query parameter of the page's URL that carries the text size. The
// page reads it before it first renders (frontend/src/lib/textSize.ts), so its first frame
// already shows text at the stored size, and writes it back on every change, so a reload
// keeps the size the user chose since.
const TextSizeParam = "textSize"

// SetupParam is the query parameter of the page's URL that says the first-run setup shows,
// as SetupValue. The page reads it before it first renders (frontend/src/lib/setup.ts), so
// its first frame is the setup's, never a flash of the inbox, and drops it once the setup
// ends; the setup's own state, which the page reads next, is what counts.
const SetupParam = "setup"

// SetupValue is SetupParam's value while the setup shows.
const SetupValue = "1"

// PageURL is the URL the window opens its page at: the app's root, carrying the text size
// the page shows from its first frame, and whether it opens on the first-run setup. With
// no setup it is the same URL it always was.
func PageURL(textSize settings.TextSize, setup bool) string {
	q := url.Values{TextSizeParam: {string(textSize)}}
	if setup {
		q.Set(SetupParam, SetupValue)
	}

	return "/?" + q.Encode()
}
