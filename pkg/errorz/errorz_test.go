package errorz

import (
	"errors"
	"testing"
)

func TestNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		errType     string
		code        int
		message     string
		details     []interface{}
		wantDetails interface{}
	}{
		{
			name:    "basic error without details",
			errType: "BAD_REQUEST",
			code:    400,
			message: "invalid input",
		},
		{
			name:        "error with details",
			errType:     "CONFLICT",
			code:        409,
			message:     "resource conflict",
			details:     []interface{}{"extra info"},
			wantDetails: "extra info",
		},
		{
			name:    "error with empty details slice",
			errType: "INTERNAL",
			code:    500,
			message: "server error",
			details: []interface{}{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := New(tt.errType, tt.code, tt.message, tt.details...)

			if got.Type != tt.errType {
				t.Errorf("Type = %q, want %q", got.Type, tt.errType)
			}
			if got.Code != tt.code {
				t.Errorf("Code = %d, want %d", got.Code, tt.code)
			}
			if got.Message != tt.message {
				t.Errorf("Message = %q, want %q", got.Message, tt.message)
			}
			if got.Details != tt.wantDetails {
				t.Errorf("Details = %v, want %v", got.Details, tt.wantDetails)
			}
		})
	}
}

func TestInternal(t *testing.T) {
	t.Parallel()

	cause := errors.New("connection refused")
	got := Internal(cause, "failed to connect to %s", "database")

	if got.Type != "INTERNAL_SERVER_ERROR" {
		t.Errorf("Type = %q, want %q", got.Type, "INTERNAL_SERVER_ERROR")
	}
	if got.Code != 500 {
		t.Errorf("Code = %d, want 500", got.Code)
	}
	if got.Message != "failed to connect to database" {
		t.Errorf("Message = %q, want %q", got.Message, "failed to connect to database")
	}
	if got.Err != cause {
		t.Errorf("Err = %v, want %v", got.Err, cause)
	}
	if got.Metadata["error"] != cause {
		t.Errorf("Metadata[error] = %v, want %v", got.Metadata["error"], cause)
	}
}

func TestNotFound(t *testing.T) {
	t.Parallel()

	got := NotFound("widget %q not found", "abc")

	if got.Type != "NOT_FOUND" {
		t.Errorf("Type = %q, want %q", got.Type, "NOT_FOUND")
	}
	if got.Code != 404 {
		t.Errorf("Code = %d, want 404", got.Code)
	}
	if got.Message != `widget "abc" not found` {
		t.Errorf("Message = %q, want %q", got.Message, `widget "abc" not found`)
	}
}

func TestFrom(t *testing.T) {
	t.Parallel()

	t.Run("nil error returns nil", func(t *testing.T) {
		t.Parallel()
		if got := From(nil); got != nil {
			t.Errorf("From(nil) = %v, want nil", got)
		}
	})

	t.Run("Error type passes through", func(t *testing.T) {
		t.Parallel()
		original := NotFound("gone")
		got := From(original)
		if got != original {
			t.Errorf("From(*Error) returned different pointer")
		}
	})

	t.Run("standard error wraps as Internal", func(t *testing.T) {
		t.Parallel()
		std := errors.New("something broke")
		got := From(std)
		if got.Type != "INTERNAL_SERVER_ERROR" {
			t.Errorf("Type = %q, want INTERNAL_SERVER_ERROR", got.Type)
		}
		if got.Code != 500 {
			t.Errorf("Code = %d, want 500", got.Code)
		}
		if got.Message != "something broke" {
			t.Errorf("Message = %q, want %q", got.Message, "something broke")
		}
	})
}

func TestError_Error(t *testing.T) {
	t.Parallel()

	t.Run("message only", func(t *testing.T) {
		t.Parallel()
		e := New("ERR", 500, "server error")
		if got := e.Error(); got != "server error" {
			t.Errorf("Error() = %q, want %q", got, "server error")
		}
	})

	t.Run("message with wrapped error", func(t *testing.T) {
		t.Parallel()
		cause := errors.New("disk full")
		e := New("ERR", 500, "write failed").WithError(cause)
		want := "write failed: disk full"
		if got := e.Error(); got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})
}

func TestError_Unwrap(t *testing.T) {
	t.Parallel()

	t.Run("returns wrapped error", func(t *testing.T) {
		t.Parallel()
		cause := errors.New("root cause")
		e := Internal(cause, "wrapped")
		if !errors.Is(e, cause) {
			t.Error("errors.Is should find the wrapped cause")
		}
	})

	t.Run("nil when no wrapped error", func(t *testing.T) {
		t.Parallel()
		e := NotFound("gone")
		if e.Unwrap() != nil {
			t.Error("Unwrap() should be nil when no wrapped error")
		}
	})
}

func TestError_WithMethods(t *testing.T) {
	t.Parallel()

	e := New("ERR", 500, "initial")

	e = e.WithMessage("updated %s", "message")
	if e.Message != "updated message" {
		t.Errorf("WithMessage: Message = %q, want %q", e.Message, "updated message")
	}

	e = e.WithDetails(map[string]string{"key": "val"})
	details, ok := e.Details.(map[string]string)
	if !ok || details["key"] != "val" {
		t.Errorf("WithDetails: Details = %v", e.Details)
	}

	meta := Metadata{"foo": "bar"}
	e = e.WithMetadata(meta)
	if e.Metadata["foo"] != "bar" {
		t.Errorf("WithMetadata: Metadata = %v", e.Metadata)
	}

	cause := errors.New("cause")
	e = e.WithError(cause)
	if e.Err != cause {
		t.Errorf("WithError: Err = %v, want %v", e.Err, cause)
	}
}

func TestBuilder(t *testing.T) {
	t.Parallel()

	t.Run("full builder chain", func(t *testing.T) {
		t.Parallel()
		cause := errors.New("timeout")
		got := Build("TIMEOUT", 504).
			Message("gateway timeout").
			Details("upstream slow").
			Metadata(Metadata{"service": "auth"}).
			Error(cause).
			Err()

		if got.Type != "TIMEOUT" {
			t.Errorf("Type = %q, want TIMEOUT", got.Type)
		}
		if got.Code != 504 {
			t.Errorf("Code = %d, want 504", got.Code)
		}
		if got.Message != "gateway timeout" {
			t.Errorf("Message = %q", got.Message)
		}
		if got.Details != "upstream slow" {
			t.Errorf("Details = %v", got.Details)
		}
		if got.Metadata["service"] != "auth" {
			t.Errorf("Metadata = %v", got.Metadata)
		}
		if got.Err != cause {
			t.Errorf("Err = %v", got.Err)
		}
	})

	t.Run("Build with initial message", func(t *testing.T) {
		t.Parallel()
		got := Build("ERR", 500, "initial message").Err()
		if got.Message != "initial message" {
			t.Errorf("Message = %q, want %q", got.Message, "initial message")
		}
	})

	t.Run("Messagef formats correctly", func(t *testing.T) {
		t.Parallel()
		got := Build("ERR", 400).
			Messagef("invalid field %q", "email").
			Err()
		want := `invalid field "email"`
		if got.Message != want {
			t.Errorf("Message = %q, want %q", got.Message, want)
		}
	})
}
