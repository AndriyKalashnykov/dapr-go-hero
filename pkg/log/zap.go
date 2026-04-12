// Package log provides a minimal logr.LogSink backed by zap.
//
// It replaces go-logr/zapr for projects that only need basic Info/Error
// logging through the logr interface.
package log

import (
	"github.com/go-logr/logr"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// zapSink implements logr.LogSink using a *zap.Logger.
type zapSink struct {
	l *zap.Logger
}

var _ logr.LogSink = (*zapSink)(nil)

// NewLogger creates a logr.Logger backed by the given zap.Logger.
// Signature matches zapr.NewLogger for drop-in replacement.
func NewLogger(l *zap.Logger) logr.Logger {
	// AddCallerSkip(1) accounts for the logr wrapper layer.
	sink := &zapSink{l: l.WithOptions(zap.AddCallerSkip(1))}
	return logr.New(sink)
}

func (s *zapSink) Init(info logr.RuntimeInfo) {
	s.l = s.l.WithOptions(zap.AddCallerSkip(info.CallDepth))
}

func (s *zapSink) Enabled(level int) bool {
	return s.l.Core().Enabled(toZapLevel(level))
}

func (s *zapSink) Info(level int, msg string, keysAndValues ...any) {
	if ce := s.l.Check(toZapLevel(level), msg); ce != nil {
		ce.Write(fields(keysAndValues)...)
	}
}

func (s *zapSink) Error(err error, msg string, keysAndValues ...any) {
	if ce := s.l.Check(zap.ErrorLevel, msg); ce != nil {
		ce.Write(fields(keysAndValues, zap.NamedError("error", err))...)
	}
}

func (s *zapSink) WithValues(keysAndValues ...any) logr.LogSink {
	return &zapSink{l: s.l.With(fields(keysAndValues)...)}
}

func (s *zapSink) WithName(name string) logr.LogSink {
	return &zapSink{l: s.l.Named(name)}
}

// toZapLevel converts a logr verbosity level to a zap level.
// logr: 0 = info, higher = more verbose. zap: lower = more severe.
func toZapLevel(level int) zapcore.Level {
	// Clamp to int8 range to prevent overflow (gosec G115).
	// zapcore.Level is int8; logr levels above 127 are impractical.
	if level > 127 {
		level = 127
	}
	return -zapcore.Level(level)
}

// fields converts logr-style key/value pairs into zap fields.
func fields(keysAndValues []any, extra ...zap.Field) []zap.Field {
	if len(keysAndValues) == 0 && len(extra) == 0 {
		return nil
	}
	out := make([]zap.Field, 0, len(keysAndValues)/2+len(extra))
	for i := 0; i < len(keysAndValues)-1; i += 2 {
		key, _ := keysAndValues[i].(string)
		out = append(out, zap.Any(key, keysAndValues[i+1]))
	}
	return append(out, extra...)
}
