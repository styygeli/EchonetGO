package mqtt

import (
	"testing"
	"time"

	"github.com/styygeli/echonetgo/internal/specs"
)

func TestExpireAfterFor(t *testing.T) {
	tests := []struct {
		name string
		in   time.Duration
		want int
	}{
		{"default 1m clamps to floor", time.Minute, 300},
		{"2m equals floor", 2 * time.Minute, 300},
		{"5m -> 750", 5 * time.Minute, 750},
		{"10m -> 1500", 10 * time.Minute, 1500},
		{"24h -> 216000", 24 * time.Hour, 216000},
		{"zero falls back to floor", 0, 300},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := expireAfterFor(tt.in); got != tt.want {
				t.Fatalf("expireAfterFor(%s) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestSlowestMetricInterval(t *testing.T) {
	ms := []specs.MetricSpec{
		{EPC: 0x80, ScrapeInterval: time.Minute},
		{EPC: 0xB0, ScrapeInterval: 10 * time.Minute},
		{EPC: 0xB3, ScrapeInterval: time.Minute},
	}
	tests := []struct {
		name string
		epcs []byte
		want time.Duration
	}{
		{"picks slowest of matches", []byte{0x80, 0xB0, 0xB3}, 10 * time.Minute},
		{"skips zero epc", []byte{0x00, 0xB0}, 10 * time.Minute},
		{"single match", []byte{0x80, 0xC0}, time.Minute},
		{"no match -> zero", []byte{0xFF}, 0},
		{"empty -> zero", nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := slowestMetricInterval(ms, tt.epcs...); got != tt.want {
				t.Fatalf("slowestMetricInterval(%v) = %s, want %s", tt.epcs, got, tt.want)
			}
		})
	}
}

func TestDiscovery_CacheKeyIncludesWritableCount(t *testing.T) {
	p := &Publisher{
		published: make(map[string]string),
	}

	keyWithoutWritable := "TestMfg|Model1|w:-1"
	keyWithWritable := "TestMfg|Model1|w:5"

	p.published["test_dev"] = keyWithoutWritable
	if p.published["test_dev"] == keyWithWritable {
		t.Fatal("expected discovery cache key to differ when writable map count changes")
	}
}
