package logging

import (
	"bytes"
	"strings"
	"testing"
)

func TestRecoverPanic_RecoversAndLogs(t *testing.T) {
	var errBuf bytes.Buffer
	l := NewWithWriters("test", nil, &errBuf)

	didPanic := func() (recovered bool) {
		defer func() {
			// If RecoverPanic did its job, the goroutine continues normally and
			// this outer recover sees nothing.
			if r := recover(); r != nil {
				recovered = false
			}
		}()
		defer l.RecoverPanic("unit test goroutine")
		panic("boom")
	}

	// Must not propagate the panic.
	_ = didPanic()

	out := errBuf.String()
	if !strings.Contains(out, "panic recovered in unit test goroutine") {
		t.Fatalf("expected recovery log, got: %q", out)
	}
	if !strings.Contains(out, "boom") {
		t.Fatalf("expected panic value in log, got: %q", out)
	}
}

func TestRecoverPanic_NoPanicIsNoop(t *testing.T) {
	var errBuf bytes.Buffer
	l := NewWithWriters("test", nil, &errBuf)
	func() {
		defer l.RecoverPanic("clean goroutine")
	}()
	if errBuf.Len() != 0 {
		t.Fatalf("expected no output when no panic, got: %q", errBuf.String())
	}
}
