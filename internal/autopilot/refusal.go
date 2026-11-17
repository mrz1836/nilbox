package autopilot

// RefusalCode names a refusal the window answers on its own, so it tells which one from
// the code and never from the words.
type RefusalCode string

// The refusals the window tells apart.
const (
	// RefusalNoMatch is a run from an item's menu refused because the routine doesn't
	// match the item now (ErrNoMatch): the window offers Run anyway, with the refusal's
	// words for why. A Run anyway that is refused, as on a kind the routine doesn't act on,
	// carries no code, since running anyway again would be refused the same way.
	RefusalNoMatch RefusalCode = "noMatch"
)

// RefusalDetail is what the window gets beside a RefusalError's message: {"code": "..."}.
type RefusalDetail struct {
	Code RefusalCode `json:"code"`
}

// RefusalError is a refusal the window answers on its own. Its message and chain are the
// refusal's (errors.Is sees ErrNoMatch, say), and Detail hands the desktop runtime its
// code, which it sends the window beside the message.
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
