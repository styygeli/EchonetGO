package echonet

import (
	"errors"
	"syscall"
	"testing"
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
