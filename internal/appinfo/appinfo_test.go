package appinfo

import (
	"bytes"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/timing"
)

var errLog = errors.New("log write failed")

// stubRecorder returns a fixed result and counts calls.
type stubRecorder struct {
	calls int
	m     timing.Measurement
	err   error
}

func (r *stubRecorder) FirstPaint() (timing.Measurement, bool, error) {
	r.calls++

	if r.calls > 1 {
		return r.m, false, nil
	}

	return r.m, true, r.err
}

func TestInfo(t *testing.T) {
	s := NewService(Config{Version: "1.2.3"})

	assert.Equal(t, Info{Name: brand.Name, Tagline: brand.Tagline, Version: "1.2.3"}, s.Info())
}

func TestReportFirstPaintCallsHookOnce(t *testing.T) {
	rec := &stubRecorder{m: timing.Measurement{FirstPaintMS: 321, Source: timing.SourceOS}}

	var got []timing.Measurement

	s := NewService(Config{Recorder: rec, OnFirstPaint: func(m timing.Measurement) { got = append(got, m) }})

	require.NoError(t, s.ReportFirstPaint())
	require.NoError(t, s.ReportFirstPaint())

	assert.Equal(t, 2, rec.calls)
	assert.Equal(t, []timing.Measurement{rec.m}, got)
}

func TestReportFirstPaintReturnsLogErrorAfterHook(t *testing.T) {
	rec := &stubRecorder{m: timing.Measurement{FirstPaintMS: 12, Source: timing.SourceRuntime}, err: errLog}

	hooked := false

	s := NewService(Config{Recorder: rec, OnFirstPaint: func(timing.Measurement) { hooked = true }})

	require.ErrorIs(t, s.ReportFirstPaint(), errLog)
	assert.True(t, hooked, "the measurement is valid even when the timing log fails")
}

func TestReportFirstPaintWithoutRecorderOrHook(t *testing.T) {
	require.NoError(t, NewService(Config{}).ReportFirstPaint())

	rec := &stubRecorder{}
	require.NoError(t, NewService(Config{Recorder: rec}).ReportFirstPaint())
	assert.Equal(t, 1, rec.calls)
}

func TestReportFirstPaintWithTimingRecorder(t *testing.T) {
	var buf bytes.Buffer

	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	rec := timing.NewRecorder(timing.Config{
		Logger: slog.New(slog.NewTextHandler(&buf, nil)),
		Start:  start,
		Source: timing.SourceOS,
		Now:    func() time.Time { return start.Add(250 * time.Millisecond) },
	})

	var got timing.Measurement

	s := NewService(Config{Recorder: rec, OnFirstPaint: func(m timing.Measurement) { got = m }})

	require.NoError(t, s.ReportFirstPaint())
	assert.Equal(t, "startup.first_paint_ms=250 source=os", got.String())
	assert.Contains(t, buf.String(), "startup.first_paint_ms=250 source=os")
}
