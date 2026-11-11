//go:build desktop

package main

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/desktop"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// errAsks is the refusal the probe service's method fails with.
var errAsks = errors.New("the command needs the user's trust")

// askingError refuses until the window answers its detail, as a repository command's
// trust question does.
type askingError struct{ err error }

func (e askingError) Error() string { return e.err.Error() }
func (e askingError) Unwrap() error { return e.err }
func (askingError) Detail() any     { return map[string]string{"ref": "t3:check"} }

// probe is a bound service whose method fails with a worded refusal and its detail.
type probe struct{}

// Fail refuses, as Run refuses a command the user hasn't confirmed.
func (*probe) Fail() error {
	return askingError{err: uierr.New("Confirm it before it runs.", errAsks)}
}

// TestABoundMethodsErrorReachesTheWindowWorded binds a service as the app binds every
// service and calls its failing method through the desktop runtime's own bindings: the
// cause the window gets holds what the user is told and the detail it answers. The runtime
// encodes a bound method's error with its service's marshaler alone, so this fails if a
// runtime upgrade or a change to bindService drops either.
//
//nolint:paralleltest // application.New sets the runtime's one global application
func TestABoundMethodsErrorReachesTheWindowWorded(t *testing.T) {
	application.New(application.Options{Name: brand.Name, MarshalError: desktop.MarshalError})

	bindings := application.NewBindings(desktop.MarshalError, nil)
	require.NoError(t, bindings.Add(bindService(&probe{})))

	typ := reflect.TypeFor[probe]()
	method := bindings.Get(&application.CallOptions{MethodName: typ.PkgPath() + "." + typ.Name() + ".Fail"})
	require.NotNil(t, method, "the probe's method is bound")

	_, err := method.Call(t.Context(), nil)

	var callErr *application.CallError
	require.ErrorAs(t, err, &callErr)

	cause, err := json.Marshal(callErr.Cause)
	require.NoError(t, err)
	assert.JSONEq(t, `{"message":"Confirm it before it runs.","detail":{"ref":"t3:check"}}`, string(cause))
}
