package review

import "context"

// PeopleSummary is how much the People directory holds: the people the app learned of, and
// the repositories it knows anyone of, which Settings shows beside Forget.
type PeopleSummary struct {
	People int `json:"people"`
	Repos  int `json:"repos"`
}

// PeopleSummary counts what the People directory holds, which both sheets' pickers suggest
// from: the reviewers' and the assignees'.
func (s *Service) PeopleSummary(ctx context.Context) (PeopleSummary, error) {
	if s.people == nil {
		return PeopleSummary{}, nil
	}

	return s.people.Summary(ctx)
}

// ForgetPeople empties the People directory: everyone the app learned of, who works where,
// who can be assigned where, the teams, and every read, so the pickers learn again from the
// next read, and the next picker opened reads its repository again. Nothing on GitHub
// changes.
func (s *Service) ForgetPeople(ctx context.Context) (PeopleSummary, error) {
	if s.people == nil {
		return PeopleSummary{}, nil
	}

	if err := s.people.Forget(ctx); err != nil {
		return PeopleSummary{}, err
	}

	return s.people.Summary(ctx)
}
