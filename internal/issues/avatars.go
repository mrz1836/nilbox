package issues

import "context"

// Avatars returns the profile pictures of the people with logins, as the sheets show them
// beside their names (the review sheet's window asks through this one method too), as data:
// URLs keyed by login, leaving out anyone without one (see avatars.Service.Pictures). Each
// picture is downloaded from GitHub's avatar host once and kept, so the window loads
// nothing remote itself.
func (s *Service) Avatars(ctx context.Context, logins []string) (map[string]string, error) {
	if s.pictures == nil {
		return map[string]string{}, nil
	}

	return s.pictures.Pictures(ctx, logins)
}
