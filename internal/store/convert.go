package store

// ConvertRows converts each of an owner's query rows with convert, in order, into a list
// that is never nil, so a bound result holding it carries [] rather than null. It stops at
// the first row convert refuses and returns convert's error as it is, for the owner to
// word.
func ConvertRows[R, T any](rows []R, convert func(R) (T, error)) ([]T, error) {
	out := make([]T, 0, len(rows))

	for _, row := range rows {
		v, err := convert(row)
		if err != nil {
			return nil, err
		}

		out = append(out, v)
	}

	return out, nil
}

// Deref returns what p points to, or T's zero value when p is nil: what a nullable
// column, or one from a LEFT JOIN whose row is missing, reads as.
func Deref[T any](p *T) T {
	if p == nil {
		var zero T

		return zero
	}

	return *p
}
