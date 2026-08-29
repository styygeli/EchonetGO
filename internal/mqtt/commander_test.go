package mqtt

import (
	"context"
	"testing"

	"github.com/styygeli/echonetgo/internal/config"
	"github.com/styygeli/echonetgo/internal/echonet"
	"github.com/styygeli/echonetgo/internal/poller"
	"github.com/styygeli/echonetgo/internal/specs"
)

func TestNormalizeClimateSetpoint(t *testing.T) {
	intMs := specs.MetricSpec{Scale: 1}
	autoMs := specs.MetricSpec{Scale: 0}
	fineMs := specs.MetricSpec{Scale: 0.1}

	cases := []struct {
		name    string
		req     float64
		prev    float64
		hasPrev bool
		ms      specs.MetricSpec
		want    float64
	}{
		{"integer passthrough", 22.0, 22.0, true, intMs, 22.0},
		{"half up when raising", 22.5, 22.0, true, intMs, 23.0},
		{"half down when lowering", 22.5, 23.0, true, intMs, 22.0},
		{"half equal to prev rounds up", 22.5, 22.5, true, intMs, 23.0},
		{"half no prev uses math.Round", 22.5, 0, false, intMs, 23.0},
		{"sub-half fraction nearest", 22.3, 22.0, true, intMs, 22.0},
		{"super-half fraction nearest", 22.7, 22.0, true, intMs, 23.0},
		{"scale zero treated as one", 22.5, 22.0, true, autoMs, 23.0},
		{"sub-integer scale passthrough", 22.5, 22.0, true, fineMs, 22.5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeClimateSetpoint(tc.req, tc.prev, tc.hasPrev, tc.ms)
			if got != tc.want {
				t.Fatalf("normalizeClimateSetpoint(%v, %v, %v, scale=%v) = %v, want %v",
					tc.req, tc.prev, tc.hasPrev, tc.ms.Scale, got, tc.want)
			}
		})
	}
}

type mockReconciler struct {
	called bool
	dev    config.Device
}

func (m *mockReconciler) ReconcileDevice(_ context.Context, dev config.Device, _ [3]byte) {
	m.called = true
	m.dev = dev
}

func TestCommander_WritableCheck_PermissiveFallback(t *testing.T) {
	cache := poller.NewCache()
	dev := config.Device{Name: "ac_test", IP: "192.168.1.50", Class: "home_ac"}
	cfg := &config.Config{Devices: []config.Device{dev}}
	cmd := NewCommander(nil, cache, cfg, "test")
	mockRec := &mockReconciler{}
	cmd.SetReconciler(mockRec)

	climateSpec := &specs.ClimateSpec{TemperatureEPC: 0xB3}
	metricSpecs := []specs.MetricSpec{{EPC: 0xB3, Name: "set_temperature_celsius", Size: 1}}

	// When hasMap is false: should trigger reconciler and proceed (not drop before network)
	// (Note: it will fail on c.client == nil when calling SendSet, but it should NOT return early at the writable check)
	defer func() {
		_ = recover() // Catch nil client dereference if it gets past the check
	}()

	cmd.handleClimateTemperature(context.Background(), "192.168.1.50:3610", [3]byte{1, 0x30, 1}, &dev, "24", climateSpec, metricSpecs, nil, false)

	if !mockRec.called {
		t.Fatal("expected reconciler to be called when hasMap is false")
	}
	if mockRec.dev.Name != "ac_test" {
		t.Fatalf("reconciler called with dev %q, want ac_test", mockRec.dev.Name)
	}
}

func TestCommander_WritableCheck_EnforcesWhenMapPresent(t *testing.T) {
	cache := poller.NewCache()
	dev := config.Device{Name: "ac_test", IP: "192.168.1.50", Class: "home_ac"}
	cfg := &config.Config{Devices: []config.Device{dev}}
	cmd := NewCommander(nil, cache, cfg, "test")
	mockRec := &mockReconciler{}
	cmd.SetReconciler(mockRec)

	climateSpec := &specs.ClimateSpec{TemperatureEPC: 0xB3}
	metricSpecs := []specs.MetricSpec{{EPC: 0xB3, Name: "set_temperature_celsius", Size: 1}}
	writable := map[byte]struct{}{0x80: {}} // 0xB3 is NOT writable

	// When hasMap is true and 0xB3 not in writable: must return early WITHOUT calling client or reconciler
	cmd.handleClimateTemperature(context.Background(), "192.168.1.50:3610", [3]byte{1, 0x30, 1}, &dev, "24", climateSpec, metricSpecs, writable, true)

	if mockRec.called {
		t.Fatal("reconciler should NOT be called when hasMap is true")
	}
}

func TestCommander_HandleClimateMode_WritableCheck(t *testing.T) {
	cache := poller.NewCache()
	dev := config.Device{Name: "ac_test", IP: "192.168.1.50", Class: "home_ac"}
	cfg := &config.Config{Devices: []config.Device{dev}}
	cmd := NewCommander(nil, cache, cfg, "test")
	mockRec := &mockReconciler{}
	cmd.SetReconciler(mockRec)

	valCool := 0x42
	climateSpec := &specs.ClimateSpec{ModeEPC: 0xB0, Modes: map[string]*int{"cool": &valCool}}
	metricSpecs := []specs.MetricSpec{
		{EPC: 0x80, Name: "operation_status", Size: 1},
		{EPC: 0xB0, Name: "operation_mode", Size: 1},
	}

	// 1. When hasMap is true but 0xB0 is not writable: returns early
	writable := map[byte]struct{}{0x80: {}}
	cmd.handleClimateMode(context.Background(), "192.168.1.50:3610", [3]byte{1, 0x30, 1}, &dev, "cool", climateSpec, metricSpecs, writable, true)
	if mockRec.called {
		t.Fatal("reconciler should NOT be called when hasMap is true")
	}

	// 2. When hasMap is false: triggers reconciler
	defer func() { _ = recover() }()
	cmd.handleClimateMode(context.Background(), "192.168.1.50:3610", [3]byte{1, 0x30, 1}, &dev, "cool", climateSpec, metricSpecs, nil, false)
	if !mockRec.called {
		t.Fatal("expected reconciler to be called when hasMap is false")
	}
}

func TestCommander_MergePropsToCache(t *testing.T) {
	cache := poller.NewCache()
	dev := config.Device{Name: "ac_test", IP: "192.168.1.50", Class: "home_ac"}
	cache.SetDeviceSpecs(dev, []specs.MetricSpec{
		{EPC: 0x80, Name: "operation_status", Size: 1, Scale: 1, Type: "gauge"},
		{EPC: 0xB3, Name: "set_temperature_celsius", Size: 1, Scale: 1, Type: "gauge"},
	})
	cmd := NewCommander(nil, cache, &config.Config{}, "test")

	// Merge only 0x80
	props := []echonet.Property{
		{EPC: 0x80, PDC: 1, EDT: []byte{0x30}},
		{EPC: 0xB3, PDC: 1, EDT: []byte{0x18}}, // 24 deg
	}
	cmd.mergePropsToCache(&dev, props, []byte{0x80})

	_, _, _, metrics := cache.Get(dev)
	if metrics["operation_status"].Value != 0x30 {
		t.Fatalf("operation_status = %v, want 0x30", metrics["operation_status"].Value)
	}
	// 0xB3 was not in requested epcs, so it should not be merged
	if _, ok := metrics["set_temperature_celsius"]; ok {
		t.Fatal("set_temperature_celsius should not be merged when not requested")
	}
}
