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

// PageURL is the URL the window opens its page at: the app's root, carrying the text size
// the page shows from its first frame.
func PageURL(textSize settings.TextSize) string {
	return "/?" + url.Values{TextSizeParam: {string(textSize)}}.Encode()
}
