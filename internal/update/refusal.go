package update

// RefusalCode names a refusal the window answers on its own, without asking the user, so
// it tells which one from the code and never from the words.
type RefusalCode string

// The refusals the window tells apart.
const (
	// RefusalSettling is a download or a restart refused while the last update's helper may
	// still be at work (ErrSettling): the same click works in a moment, so the window says
	// so quietly and offers the update again.
	RefusalSettling RefusalCode = "settling"

	// RefusalFinishing is a restart refused while an earlier one finishes (ErrFinishing):
	// its quit was held, and the app is quitting on its own so the update installs, so the
	// window says so calmly and offers nothing to click.
	RefusalFinishing RefusalCode = "finishing"

	// RefusalUninstallable is a download or a restart refused for an update that failed to
	// install for good (ErrUninstallable): trying again fails the same way, so the window
	// offers only its release page, never Try again.
	RefusalUninstallable RefusalCode = "uninstallable"
)

// RefusalDetail is what the window gets beside a RefusalError's message: {"code": "..."}.
type RefusalDetail struct {
	Code RefusalCode `json:"code"`
}

// RefusalError is a refusal the window answers on its own. Its message and chain are the
// refusal's (errors.Is sees ErrSettling, say), and Detail hands the desktop runtime its
// code, which it sends the window beside the message, as a RunsGoingError's runs are.
type RefusalError struct {
	// Code says which refusal it is.
	Code RefusalCode

	err error
}

// newRefusal marks err, a refusal worded for the user, with code.
func newRefusal(code RefusalCode, err error) *RefusalError {
	return &RefusalError{Code: code, err: err}
}

// Error returns the refusal's text.
func (e *RefusalError) Error() string {
	return e.err.Error()
}

// Unwrap returns the refusal, with the words for the user.
func (e *RefusalError) Unwrap() error {
	return e.err
}

// Detail returns the code, for the window.
func (e *RefusalError) Detail() any {
	return RefusalDetail{Code: e.Code}
}
