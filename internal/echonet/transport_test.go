package echonet

import (
	"context"
	"errors"
	"syscall"
	"testing"
	"time"
)

func TestNormalizeHost(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"192.168.0.10:3610", "192.168.0.10"},
		{"192.168.0.10", "192.168.0.10"},
		{"10.0.0.1:1234", "10.0.0.1"},
	}
	for _, tt := range tests {
		if got := normalizeHost(tt.in); got != tt.want {
			t.Errorf("normalizeHost(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestWaiterKey(t *testing.T) {
	// Stable, collision-free across (host, tid).
	a := waiterKey("192.168.0.10", 0x0001)
	b := waiterKey("192.168.0.10", 0x0002)
	c := waiterKey("192.168.0.11", 0x0001)
	if a == b || a == c || b == c {
		t.Fatalf("waiterKey collisions: %q %q %q", a, b, c)
	}
	if got := waiterKey("192.168.0.10", 0x00ab); got != "192.168.0.10|00ab" {
		t.Fatalf("waiterKey = %q, want 192.168.0.10|00ab", got)
	}
}

func TestIsPortBindFailure(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"EADDRINUSE", syscall.EADDRINUSE, true},
		{"EACCES", syscall.EACCES, true},
		{"wrapped EADDRINUSE", errors.New("listen udp: address already in use"), true},
		{"permission denied text", errors.New("bind: permission denied"), true},
		{"unrelated error", errors.New("connection refused"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPortBindFailure(tt.err); got != tt.want {
				t.Fatalf("isPortBindFailure(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestNextTIDWraps(t *testing.T) {
	// NextTID is a uint16 truncation of an atomic uint32; just confirm it keeps
	// advancing and stays within uint16 across many calls.
	tr := NewTransport(false)
	prev := tr.NextTID()
	for i := 0; i < 5; i++ {
		cur := tr.NextTID()
		if cur != prev+1 {
			t.Fatalf("NextTID not sequential: %d then %d", prev, cur)
		}
		prev = cur
	}
}

func TestCallerContext(t *testing.T) {
	ctx := context.Background()
	if got := CallerFromContext(ctx); got != "" {
		t.Errorf("expected empty caller for default context, got %q", got)
	}

	tagged := ContextWithCaller(ctx, "epcube:scraper:10s")
	if got := CallerFromContext(tagged); got != "epcube:scraper:10s" {
		t.Errorf("expected caller 'epcube:scraper:10s', got %q", got)
	}
}

func TestInFlightCounter(t *testing.T) {
	tr := NewTransport(false)
	if got := tr.InFlight(); got != 0 {
		t.Fatalf("expected 0 in-flight ops initially, got %d", got)
	}
	tr.inFlight.Add(2)
	if got := tr.InFlight(); got != 2 {
		t.Fatalf("expected 2 in-flight ops, got %d", got)
	}
	tr.inFlight.Add(-2)
	if got := tr.InFlight(); got != 0 {
		t.Fatalf("expected 0 in-flight ops after decrement, got %d", got)
	}
}

func TestExpiredWaiters(t *testing.T) {
	tr := NewTransport(false)
	key := "192.168.3.249:0042"

	// Pop non-existent key
	if _, ok := tr.checkAndPopExpiredWaiter(key); ok {
		t.Fatal("expected ok=false for non-existent expired waiter")
	}

	// Record and pop
	tr.recordExpiredWaiter(key, "epcube:reconciler")
	exp, ok := tr.checkAndPopExpiredWaiter(key)
	if !ok {
		t.Fatal("expected ok=true for recorded expired waiter")
	}
	if exp.caller != "epcube:reconciler" {
		t.Errorf("expected caller 'epcube:reconciler', got %q", exp.caller)
	}
	if time.Since(exp.expiredAt) > 5*time.Second {
		t.Errorf("unexpected expiredAt timestamp %v", exp.expiredAt)
	}

	// Second pop should return false (already popped)
	if _, ok := tr.checkAndPopExpiredWaiter(key); ok {
		t.Fatal("expected ok=false after waiter was already popped")
	}
}
