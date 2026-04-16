package log

import (
	"errors"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestNewLogger_Info(t *testing.T) {
	t.Parallel()

	core, logs := observer.New(zapcore.InfoLevel)
	zapLog := zap.New(core)

	log := NewLogger(zapLog)
	log.Info("hello", "key", "value")

	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("got %d log entries, want 1", len(entries))
	}
	if entries[0].Message != "hello" {
		t.Errorf("Message = %q, want %q", entries[0].Message, "hello")
	}
	if len(entries[0].ContextMap()) == 0 {
		t.Fatal("expected context fields")
	}
	if entries[0].ContextMap()["key"] != "value" {
		t.Errorf("key = %v, want %q", entries[0].ContextMap()["key"], "value")
	}
}

func TestNewLogger_Error(t *testing.T) {
	t.Parallel()

	core, logs := observer.New(zapcore.ErrorLevel)
	zapLog := zap.New(core)

	log := NewLogger(zapLog)
	cause := errors.New("something broke")
	log.Error(cause, "operation failed", "op", "save")

	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("got %d log entries, want 1", len(entries))
	}
	if entries[0].Message != "operation failed" {
		t.Errorf("Message = %q", entries[0].Message)
	}
	if entries[0].Level != zapcore.ErrorLevel {
		t.Errorf("Level = %v, want Error", entries[0].Level)
	}
	ctx := entries[0].ContextMap()
	if ctx["op"] != "save" {
		t.Errorf("op = %v, want %q", ctx["op"], "save")
	}
	if ctx["error"] != "something broke" {
		t.Errorf("error = %v, want %q", ctx["error"], "something broke")
	}
}

func TestNewLogger_WithValues(t *testing.T) {
	t.Parallel()

	core, logs := observer.New(zapcore.InfoLevel)
	zapLog := zap.New(core)

	log := NewLogger(zapLog).WithValues("component", "dapr")
	log.Info("starting")

	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("got %d log entries, want 1", len(entries))
	}
	if entries[0].ContextMap()["component"] != "dapr" {
		t.Errorf("component = %v, want %q", entries[0].ContextMap()["component"], "dapr")
	}
}

func TestNewLogger_WithName(t *testing.T) {
	t.Parallel()

	core, logs := observer.New(zapcore.InfoLevel)
	zapLog := zap.New(core)

	log := NewLogger(zapLog).WithName("myservice")
	log.Info("test")

	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("got %d log entries, want 1", len(entries))
	}
	if entries[0].LoggerName != "myservice" {
		t.Errorf("LoggerName = %q, want %q", entries[0].LoggerName, "myservice")
	}
}

func TestNewLogger_VerbosityFiltering(t *testing.T) {
	t.Parallel()

	// Observer at Info level — V(1) and higher should be suppressed
	core, logs := observer.New(zapcore.InfoLevel)
	zapLog := zap.New(core)

	log := NewLogger(zapLog)
	log.V(0).Info("visible")
	log.V(1).Info("suppressed")

	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("got %d log entries, want 1 (V(1) should be filtered)", len(entries))
	}
	if entries[0].Message != "visible" {
		t.Errorf("Message = %q, want %q", entries[0].Message, "visible")
	}
}

func TestNewLogger_Enabled(t *testing.T) {
	t.Parallel()

	core, _ := observer.New(zapcore.InfoLevel)
	zapLog := zap.New(core)

	log := NewLogger(zapLog)
	if !log.V(0).Enabled() {
		t.Error("V(0) should be enabled at Info level")
	}
	if log.V(1).Enabled() {
		t.Error("V(1) should be disabled at Info level")
	}
}

func TestToZapLevel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		logr int
		zap  zapcore.Level
	}{
		{0, zapcore.InfoLevel}, // logr 0 = zap Info (0)
		{1, zapcore.Level(-1)}, // logr 1 = zap Debug (-1)
		{5, zapcore.Level(-5)}, // logr 5 = very verbose
	}

	for _, tt := range tests {
		if got := toZapLevel(tt.logr); got != tt.zap {
			t.Errorf("toZapLevel(%d) = %v, want %v", tt.logr, got, tt.zap)
		}
	}
}
