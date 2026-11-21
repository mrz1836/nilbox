package catalog

// ScheduleStyleByID returns the style id names; "" is ScheduleEach.
func ScheduleStyleByID(id ScheduleStyle) (ScheduleStyleInfo, bool) {
	if id == "" {
		id = ScheduleEach
	}

	for _, s := range ScheduleStyles() {
		if s.ID == id {
			return s, true
		}
	}

	return ScheduleStyleInfo{}, false
}

// Dispatchers returns every dispatcher.
func Dispatchers() []Dispatcher {
	return []Dispatcher{
		DispatchTriage, DispatchNotices, DispatchGitHub, DispatchAgent, DispatchVerify, DispatchMerge, DispatchReview,
		DispatchScripts,
	}
}
