package scriptrun

import "encoding/json"

// Decode reads back, as a T, what a run recorded as its intent or its kind's own result
// (Result.Intent, Result.Outcome): the history decodes them as plain JSON values, which
// Decode encodes again and decodes into a T. It returns the zero T, with json's error,
// when they can't be read as one.
func Decode[T any](recorded any) (T, error) {
	var v T

	data, err := json.Marshal(recorded)
	if err == nil {
		err = json.Unmarshal(data, &v)
	}

	if err != nil {
		var zero T

		return zero, err
	}

	return v, nil
}
