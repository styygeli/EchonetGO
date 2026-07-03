package mqtt

import (
	"sync"
	"testing"
	"time"

	"github.com/styygeli/echonetgo/internal/config"
	"github.com/styygeli/echonetgo/internal/echonet"
	"github.com/styygeli/echonetgo/internal/poller"
)

// newTestPublisher builds a Publisher with the async pipeline fields initialized
// but no MQTT client, so the enqueue/worker/drain logic can be tested without a
// broker. Callers set publishFn and (optionally) start the worker.
func newTestPublisher() *Publisher {
	return &Publisher{
		published: make(map[string]string),
		infoSkips: make(map[string]int),
		pending:   make(map[string]poller.DeviceState),
		wake:      make(chan struct{}, 1),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
}

func dstate(name string, val float64) poller.DeviceState {
	return poller.DeviceState{
		Device:  config.Device{Name: name},
		Success: true,
		Metrics: map[string]echonet.MetricValue{"m": {Value: val}},
	}
}

func TestEnqueueDeviceState_Coalesces(t *testing.T) {
	p := newTestPublisher()
	// Worker not started: enqueue only fills the pending map.
	p.EnqueueDeviceState(dstate("devA", 1))
	p.EnqueueDeviceState(dstate("devA", 2)) // coalesces onto devA
	p.EnqueueDeviceState(dstate("devB", 3))

	p.pubMu.Lock()
	defer p.pubMu.Unlock()
	if len(p.pending) != 2 {
		t.Fatalf("pending = %d, want 2 (devA coalesced)", len(p.pending))
	}
	if p.pending["devA"].Metrics["m"].Value != 2 {
		t.Fatalf("devA pending = %v, want 2 (latest wins)", p.pending["devA"].Metrics["m"].Value)
	}
	if p.pending["devB"].Metrics["m"].Value != 3 {
		t.Fatalf("devB pending = %v, want 3", p.pending["devB"].Metrics["m"].Value)
	}
}

func TestDrainPending_PublishesLatestAndEmpties(t *testing.T) {
	p := newTestPublisher()
	got := map[string]float64{}
	p.publishFn = func(st poller.DeviceState) { got[st.Device.Name] = st.Metrics["m"].Value }

	p.EnqueueDeviceState(dstate("devA", 1))
	p.EnqueueDeviceState(dstate("devA", 2))
	p.EnqueueDeviceState(dstate("devB", 3))
	p.drainPending()

	if got["devA"] != 2 || got["devB"] != 3 {
		t.Fatalf("published = %v, want devA=2 devB=3", got)
	}
	p.pubMu.Lock()
	n := len(p.pending)
	p.pubMu.Unlock()
	if n != 0 {
		t.Fatalf("pending not emptied: %d entries remain", n)
	}
}

func TestPublishWorker_StalledPublishDoesNotBlockEnqueue(t *testing.T) {
	p := newTestPublisher()
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var mu sync.Mutex
	got := map[string]float64{}
	p.publishFn = func(s poller.DeviceState) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release // simulate a stalled broker until released
		mu.Lock()
		got[s.Device.Name] = s.Metrics["m"].Value
		mu.Unlock()
	}
	go p.publishWorker()

	p.EnqueueDeviceState(dstate("devA", 1))
	<-entered // worker is now blocked inside publishFn(devA=1)

	// Producers must not block while a publish is stalled.
	enqDone := make(chan struct{})
	go func() {
		p.EnqueueDeviceState(dstate("devA", 2)) // coalesces
		p.EnqueueDeviceState(dstate("devB", 3))
		close(enqDone)
	}()
	select {
	case <-enqDone:
	case <-time.After(time.Second):
		t.Fatal("EnqueueDeviceState blocked while a publish was stalled")
	}

	close(release) // let publishes proceed
	close(p.stop)
	<-p.done

	mu.Lock()
	defer mu.Unlock()
	if got["devA"] != 2 || got["devB"] != 3 {
		t.Fatalf("published = %v, want devA=2 devB=3 (latest after stall)", got)
	}
}

func TestPublishWorker_FlushesPendingOnStop(t *testing.T) {
	p := newTestPublisher()
	var mu sync.Mutex
	got := map[string]float64{}
	p.publishFn = func(s poller.DeviceState) {
		mu.Lock()
		got[s.Device.Name] = s.Metrics["m"].Value
		mu.Unlock()
	}
	p.EnqueueDeviceState(dstate("devA", 7))
	p.EnqueueDeviceState(dstate("devB", 8))

	go p.publishWorker()
	close(p.stop)
	select {
	case <-p.done:
	case <-time.After(time.Second):
		t.Fatal("worker did not exit after stop")
	}

	mu.Lock()
	defer mu.Unlock()
	if got["devA"] != 7 || got["devB"] != 8 {
		t.Fatalf("flush published = %v, want devA=7 devB=8", got)
	}
}
