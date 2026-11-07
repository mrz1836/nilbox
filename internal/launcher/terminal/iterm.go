package terminal

import "strings"

// appITerm2 names iTerm2 in error messages.
const appITerm2 = "iTerm2"

// itermWindowWaitTenths is how many tenths of a second the script waits for the window
// iTerm2 opens at launch before it opens one itself.
const itermWindowWaitTenths = "20"

// itermAppleScript returns the AppleScript that opens a launch script in iTerm2:
//
//  1. note whether iTerm2 is already running, then activate it;
//  2. if it was running with a window open, create a tab in the current window;
//  3. otherwise wait briefly for the window iTerm2 opens at launch and reuse it, creating
//     a window only when none appears;
//  4. type the single-quoted script path into the current session, so it runs in the
//     user's own login shell (with the user's PATH) and the tab stays usable afterwards.
//
// It is pure: running it is the darwin driver's job.
func itermAppleScript(scriptPath string) string {
	return `set wasRunning to application "iTerm" is running
tell application "iTerm"
	activate
	if wasRunning and (count of windows) > 0 then
		tell current window to create tab with default profile
	else
		repeat ` + itermWindowWaitTenths + ` times
			if (count of windows) > 0 then exit repeat
			delay 0.1
		end repeat
		if (count of windows) = 0 then
			create window with default profile
		end if
	end if
	tell current session of current window to write text "` + appleScriptString(shQuote(scriptPath)) + `"
end tell
`
}

// appleScriptString escapes s for use inside an AppleScript string literal, where only
// the backslash and the double quote are special.
func appleScriptString(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}
