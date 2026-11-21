package runevent

// EmptyPage is a page with nothing on it, every list there, as the window gets a run's
// page when there is none to read.
func EmptyPage() Page {
	return Page{}.Listed()
}

// Listed returns the page with none of its lists nil, as the window gets them: [], never
// null. When it has an Ending, the page returned has a copy of its own, its lists filled
// the same way, so p and the Ending it points to are left as they were; the lists that hold
// something are shared, not copied.
func (p Page) Listed() Page {
	if p.Events == nil {
		p.Events = []Event{}
	}

	if p.Progress.Todos == nil {
		p.Progress.Todos = []Todo{}
	}

	if p.Ending != nil {
		e := *p.Ending

		if e.Todos == nil {
			e.Todos = []Todo{}
		}

		if e.Changed == nil {
			e.Changed = []string{}
		}

		if e.Models == nil {
			e.Models = []string{}
		}

		p.Ending = &e
	}

	return p
}
