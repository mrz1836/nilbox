package issues

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePictures hands every login asked a picture named after it.
type fakePictures struct{ asked []string }

func (f *fakePictures) Pictures(_ context.Context, logins []string) (map[string]string, error) {
	f.asked = logins

	out := map[string]string{}
	for _, login := range logins {
		out[login] = "data:image/png;base64," + login
	}

	return out, nil
}

func TestAvatarsServesThePictures(t *testing.T) {
	t.Parallel()

	pictures := &fakePictures{}
	svc := NewService(Config{Pictures: pictures})

	got, err := svc.Avatars(t.Context(), []string{"octo-user"})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"octo-user": "data:image/png;base64,octo-user"}, got)
	assert.Equal(t, []string{"octo-user"}, pictures.asked)
}

func TestAvatarsServesNoneWithoutPictures(t *testing.T) {
	t.Parallel()

	got, err := NewService(Config{}).Avatars(t.Context(), []string{"octo-user"})
	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got)
}
