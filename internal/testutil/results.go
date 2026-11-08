package testutil

// ErrOf returns err and drops the value that came with it, for a call a test makes only
// for its effect, as in require.NoError(t, testutil.ErrOf(svc.SetTheme(ctx, theme))).
func ErrOf[T any](_ T, err error) error {
	return err
}
