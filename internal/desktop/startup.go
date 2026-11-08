package desktop

import (
	"errors"
	"io/fs"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// Failure is what the error dialog says when the app cannot start.
type Failure struct {
	// Title is the dialog's title.
	Title string

	// Message is its text: what went wrong, and what to do about it where that is known.
	Message string
}

// StartupFailure words err, which stopped the app from starting, for the error dialog
// shown before the app quits: nobody sees the standard error of an app opened from the
// Finder. A database saved by a newer build asks for an update. A database another
// process holds locked, and data the app may not read or write, say what to do. Anything
// else gives the error itself.
func StartupFailure(err error) Failure {
	couldNotStart := brand.Name + " couldn't start"

	switch {
	case errors.Is(err, store.ErrNewerSchema):
		return Failure{
			Title:   brand.Name + " needs an update",
			Message: textutil.Sentence(store.ErrNewerSchema.Error()),
		}
	case store.IsBusy(err):
		return Failure{
			Title: couldNotStart,
			Message: "Another copy of " + brand.Name + " may still be running with its data open. Quit it, then open " +
				brand.Name + " again.\n\n" + textutil.Sentence(err.Error()),
		}
	case errors.Is(err, fs.ErrPermission):
		return Failure{
			Title: couldNotStart,
			Message: brand.Name + " isn't allowed to read or write its data. Check the permissions of its data " +
				"folder, then open " + brand.Name + " again.\n\n" + textutil.Sentence(err.Error()),
		}
	default:
		return Failure{Title: couldNotStart, Message: textutil.Sentence(err.Error())}
	}
}
