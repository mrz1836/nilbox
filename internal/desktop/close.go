package desktop

// CloseAction is what a click on the window's close button does (Shell.WindowClosing).
type CloseAction string

// The close's actions.
const (
	// CloseThrough lets the close go through: the shell is stopping, and the desktop
	// runtime's own shutdown closes the window once the services have stopped.
	CloseThrough CloseAction = "close"

	// CloseHide keeps the app running: the window only hides, and the menu bar icon or the
	// Dock icon brings it back (HidesOnClose).
	CloseHide CloseAction = "hide"

	// CloseQuit keeps the window and quits the app the normal way, as Quit does, so the
	// quit's question (ShouldQuit) is asked while the window still exists, and a quit it
	// holds back has a window to show in.
	CloseQuit CloseAction = "quit"
)

// CloseOf decides what a click on the window's close button does: it goes through while
// the shell is stopping, whatever else holds; it only hides the window while the app keeps
// running on a close (hides, HidesOnClose's answer); and otherwise it quits the app.
func CloseOf(stopping, hides bool) CloseAction {
	switch {
	case stopping:
		return CloseThrough
	case hides:
		return CloseHide
	default:
		return CloseQuit
	}
}

// WindowClosing answers a click on the window's close button, which the desktop runtime
// asks about before the window closes (CloseOf). The caller hides the window for
// CloseHide, and keeps it from closing for CloseHide and CloseQuit.
//
// For CloseQuit the shell quits the app here, the normal way, which asks first while runs
// go in the app or holds the quit while a move to Applications runs (ShouldQuit), so the
// window stays open for the question. A quit that goes through stops the shell before the
// runtime closes the window, and that close, the shell stopping by then, goes through: a
// quit's own close never quits again. The quit runs in the background, since it stops the
// shell, which waits for its tracked work, so it is not tracked work itself.
//
// HidesOnClose is asked before whether the shell is stopping: once stopping, it stays so,
// and a close that came as the shell began to stop goes through rather than quitting
// again.
func (s *Shell) WindowClosing() CloseAction {
	hides := s.HidesOnClose()

	action := CloseOf(s.stopping.Load(), hides)
	if action == CloseQuit {
		go s.app.Quit()
	}

	return action
}
