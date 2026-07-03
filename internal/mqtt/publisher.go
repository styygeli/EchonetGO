// Package mqtt publishes ECHONET device state to an MQTT broker using
// Home Assistant's MQTT auto-discovery protocol.
package mqtt

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	pahomqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/styygeli/echonetgo/internal/config"
	"github.com/styygeli/echonetgo/internal/echonet"
	"github.com/styygeli/echonetgo/internal/logging"
	"github.com/styygeli/echonetgo/internal/poller"
	"github.com/styygeli/echonetgo/internal/specs"
)

var mqttLog = logging.New("mqtt")

const (
	connectTimeout = 10 * time.Second
	publishTimeout = 5 * time.Second
	qos            = 1
)

// Publisher handles MQTT connection and publishes HA discovery + state.
type Publisher struct {
	client          pahomqtt.Client
	topicPrefix     string
	discoveryPrefix string
	swVersion       string

	mu        sync.Mutex
	published map[string]string // tracks device name -> "manufacturer|model" published
	infoSkips map[string]int    // tracks discovery skips while waiting for device info

	muConnect          sync.Mutex
	onConnectCallbacks []func(pahomqtt.Client)

	// Async publish pipeline: cache updates are enqueued (coalesced per device)
	// and published by a single worker goroutine, so a slow/unreachable broker
	// never stalls the scraper/INF/command goroutine that produced the update.
	publishFn func(poller.DeviceState)      // defaults to PublishDeviceState; overridable in tests
	pubMu     sync.Mutex                    // guards pending
	pending   map[string]poller.DeviceState // dev.Name -> latest state awaiting publish
	wake      chan struct{}                 // cap 1: non-blocking signal to the worker
	stop      chan struct{}                 // closed on Disconnect to stop the worker
	done      chan struct{}                 // closed when the worker goroutine exits
}

// NewPublisher creates a connected MQTT publisher. Returns nil if broker is empty.
func NewPublisher(cfg config.MQTTConfig, swVersion string) (*Publisher, error) {
	if cfg.Broker == "" {
		return nil, nil
	}
	pub := &Publisher{
		topicPrefix:     cfg.TopicPrefix,
		discoveryPrefix: cfg.DiscoveryPrefix,
		swVersion:       swVersion,
		published:       make(map[string]string),
		infoSkips:       make(map[string]int),
		pending:         make(map[string]poller.DeviceState),
		wake:            make(chan struct{}, 1),
		stop:            make(chan struct{}),
		done:            make(chan struct{}),
	}
	pub.publishFn = pub.PublishDeviceState
	go pub.publishWorker()

	opts := pahomqtt.NewClientOptions().
		AddBroker(cfg.Broker).
		SetClientID("echonetgo").
		SetKeepAlive(60 * time.Second).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(10 * time.Second).
		SetCleanSession(true).
		SetConnectionLostHandler(func(_ pahomqtt.Client, err error) {
			mqttLog.Warnf("connection lost: %v", err)
		}).
		SetOnConnectHandler(func(c pahomqtt.Client) {
			mqttLog.Infof("connected to %s", cfg.Broker)
			// (Re)publish the bridge device on every connect so HA discovery
			// survives broker restarts and a deferred first connection. Run in a
			// goroutine to avoid blocking paho's connection routine on publish
			// acks. Retained publishes make this idempotent.
			go pub.publishBridgeDevice()
			pub.muConnect.Lock()
			callbacks := append([]func(pahomqtt.Client){}, pub.onConnectCallbacks...)
			pub.muConnect.Unlock()
			for _, cb := range callbacks {
				go cb(c)
			}
		})
	if cfg.Username != "" {
		opts.SetUsername(cfg.Username)
	}
	if cfg.Password != "" {
		opts.SetPassword(cfg.Password)
	}

	client := pahomqtt.NewClient(opts)
	pub.client = client
	// ConnectRetry + AutoReconnect are enabled, so the broker being unreachable
	// at boot is recoverable — common when the MQTT add-on restarts alongside
	// this one. Wait briefly for a clean connect for nicer startup logs, but do
	// NOT hard-fail: returning an error here would make the caller disable MQTT
	// for the entire process lifetime, defeating the background retry. The
	// OnConnect handler reconciles discovery and subscriptions once connected.
	token := client.Connect()
	if !token.WaitTimeout(connectTimeout) {
		mqttLog.Warnf("mqtt broker %s not reachable yet; continuing with background retry", cfg.Broker)
	} else if err := token.Error(); err != nil {
		mqttLog.Warnf("mqtt connect to %s failed: %v; continuing with background retry", cfg.Broker, err)
	}
	return pub, nil
}

// RegisterOnConnect registers a callback to be run whenever the client connects or reconnects.
func (p *Publisher) RegisterOnConnect(cb func(pahomqtt.Client)) {
	p.muConnect.Lock()
	defer p.muConnect.Unlock()
	p.onConnectCallbacks = append(p.onConnectCallbacks, cb)
	if p.client != nil && p.client.IsConnected() {
		go cb(p.client)
	}
}

// Client returns the MQTT client for subscriptions (e.g. Commander).
func (p *Publisher) Client() pahomqtt.Client {
	return p.client
}

// Disconnect cleanly shuts down the MQTT connection.
func (p *Publisher) Disconnect() {
	// Stop the publish worker and let it flush any pending state before we drop
	// the connection.
	close(p.stop)
	select {
	case <-p.done:
	case <-time.After(publishTimeout):
		mqttLog.Warnf("publish worker did not drain within %s", publishTimeout)
	}
	topic := fmt.Sprintf("%s/bridge/availability", p.topicPrefix)
	token := p.client.Publish(topic, qos, true, "offline")
	token.WaitTimeout(publishTimeout)
	p.client.Disconnect(1000)
	mqttLog.Infof("disconnected")
}

// EnqueueDeviceState records the latest state for a device and signals the
// publish worker. It never blocks on broker I/O, so the producing goroutine
// (scraper, INF handler, or command verification) is decoupled from broker
// latency. Registered as the cache onUpdate callback.
func (p *Publisher) EnqueueDeviceState(st poller.DeviceState) {
	p.pubMu.Lock()
	p.pending[st.Device.Name] = st // coalesce: latest state wins
	p.pubMu.Unlock()
	select {
	case p.wake <- struct{}{}:
	default: // worker already signaled
	}
}

// publishWorker drains queued device state until Disconnect closes p.stop.
func (p *Publisher) publishWorker() {
	defer mqttLog.RecoverPanic("mqtt publish worker")
	defer close(p.done)
	for {
		select {
		case <-p.stop:
			p.drainPending() // flush remaining before exit
			return
		case <-p.wake:
			p.drainPending()
		}
	}
}

// drainPending publishes the latest pending state for every device, recovering
// per item so one failing publish cannot kill the worker.
func (p *Publisher) drainPending() {
	for {
		p.pubMu.Lock()
		var name string
		var st poller.DeviceState
		found := false
		for k, v := range p.pending {
			name, st, found = k, v, true
			break
		}
		if found {
			delete(p.pending, name)
		}
		p.pubMu.Unlock()
		if !found {
			return
		}
		func() {
			defer mqttLog.RecoverPanic("mqtt publish for " + st.Device.Name)
			p.publishFn(st)
		}()
	}
}

// PublishDeviceState publishes state for a device and ensures discovery has been sent.
func (p *Publisher) PublishDeviceState(st poller.DeviceState) {
	dev := st.Device
	p.publishAvailability(dev, st.Success)
	if st.Success && len(st.Metrics) > 0 {
		p.ensureDiscovery(dev, st.Info, st.MetricSpecs, st.Writable, st.Climate, st.Light, st.Metrics)
		p.publishState(dev, st.Metrics)
		if st.Climate != nil {
			p.publishClimateState(dev, st.Metrics, st.MetricSpecs, st.Climate)
		}
		if st.Light != nil {
			p.publishLightState(dev, st.Metrics, st.MetricSpecs, st.Light)
		}
		if st.Writable != nil {
			p.publishWritableState(dev, st.Metrics, st.MetricSpecs, st.Writable, st.Climate, st.Light)
		}
	}
}

func (p *Publisher) publishClimateState(dev config.Device, metrics map[string]echonet.MetricValue, metricSpecs []specs.MetricSpec, cl *specs.ClimateSpec) {
	base := fmt.Sprintf("%s/%s/climate", p.topicPrefix, dev.Name)
	modeName := metricNameForEPC(metricSpecs, cl.ModeEPC)
	tempName := metricNameForEPC(metricSpecs, cl.TemperatureEPC)
	currentName := metricNameForEPC(metricSpecs, cl.CurrentTemperatureEPC)
	fanName := metricNameForEPC(metricSpecs, cl.FanModeEPC)

	operationStatusName := metricNameForEPC(metricSpecs, 0x80)
	var modeStr string
	if operationStatusName != "" {
		if mv, ok := metrics[operationStatusName]; ok {
			if int(mv.Value) == 0x31 {
				modeStr = "off"
			}
		}
	}
	if modeStr != "off" && modeName != "" {
		if mv, ok := metrics[modeName]; ok {
			raw := int(mv.Value)
			for label, v := range cl.Modes {
				if v != nil && *v == raw {
					modeStr = label
					break
				}
			}
			if modeStr == "" {
				mqttLog.Debugf("device %s: operation_mode raw 0x%02x not in climate modes; leaving mode unpublished", dev.Name, raw)
			}
		}
	}
	if modeStr != "" {
		p.client.Publish(base+"/mode/state", qos, false, modeStr)
	}
	if tempName != "" {
		if mv, ok := metrics[tempName]; ok {
			p.client.Publish(base+"/temperature/state", qos, false, fmt.Sprintf("%.0f", mv.Value))
		}
	}
	if currentName != "" {
		if mv, ok := metrics[currentName]; ok {
			p.client.Publish(base+"/current_temperature", qos, false, fmt.Sprintf("%.0f", mv.Value))
		}
	}
	if fanName != "" && cl.FanModeEPC != 0 {
		if mv, ok := metrics[fanName]; ok && mv.EnumLabel != "" {
			p.client.Publish(base+"/fan_mode/state", qos, false, mv.EnumLabel)
		}
	}
}

func (p *Publisher) publishLightState(dev config.Device, metrics map[string]echonet.MetricValue, metricSpecs []specs.MetricSpec, lt *specs.LightSpec) {
	base := fmt.Sprintf("%s/%s/light", p.topicPrefix, dev.Name)

	// Power state from operation_status (0x80).
	opName := metricNameForEPC(metricSpecs, 0x80)
	if opName != "" {
		if mv, ok := metrics[opName]; ok {
			if int(mv.Value) == 0x30 {
				p.client.Publish(base+"/power/state", qos, false, "ON")
			} else {
				p.client.Publish(base+"/power/state", qos, false, "OFF")
			}
		}
	}

	// Brightness state.
	if lt.BrightnessEPC != 0 {
		brightnessName := metricNameForEPC(metricSpecs, lt.BrightnessEPC)
		if brightnessName != "" {
			if mv, ok := metrics[brightnessName]; ok {
				p.client.Publish(base+"/brightness/state", qos, false, fmt.Sprintf("%.0f", mv.Value))
			}
		}
	}

	// Effect state from color setting or scene.
	if lt.ColorSettingEPC != 0 {
		colorName := metricNameForEPC(metricSpecs, lt.ColorSettingEPC)
		if colorName != "" {
			if mv, ok := metrics[colorName]; ok && mv.EnumLabel != "" {
				// Only publish if the label is in our color_settings map.
				if _, inSettings := lt.ColorSettings[mv.EnumLabel]; inSettings {
					p.client.Publish(base+"/effect/state", qos, false, mv.EnumLabel)
				}
			}
		}
	} else if lt.SceneEPC != 0 {
		sceneName := metricNameForEPC(metricSpecs, lt.SceneEPC)
		if sceneName != "" {
			if mv, ok := metrics[sceneName]; ok && mv.Value >= 1 {
				p.client.Publish(base+"/effect/state", qos, false, fmt.Sprintf("scene_%.0f", mv.Value))
			}
		}
	}
}

func (p *Publisher) publishWritableState(dev config.Device, metrics map[string]echonet.MetricValue, metricSpecs []specs.MetricSpec, writable map[byte]struct{}, climateSpec *specs.ClimateSpec, lightSpec *specs.LightSpec) {
	for _, ms := range metricSpecs {
		if _, ok := writable[ms.EPC]; !ok {
			continue
		}
		if isClimateEPC(ms.EPC, climateSpec) {
			continue
		}
		if isLightEPC(ms.EPC, lightSpec) {
			continue
		}
		entityType := writableEntityType(ms)
		if entityType == "" {
			continue
		}
		mv, ok := metrics[ms.Name]
		if !ok {
			continue
		}
		if shouldSkipStateUpdate(ms, mv) {
			continue
		}
		base := fmt.Sprintf("%s/%s/%s/%s", p.topicPrefix, dev.Name, entityType, ms.Name)
		stateTopic := base + "/state"
		var payload string
		switch entityType {
		case "switch":
			if mv.EnumLabel != "" {
				switch strings.ToLower(mv.EnumLabel) {
				case "on":
					payload = "ON"
				case "off":
					payload = "OFF"
				default:
					payload = mv.EnumLabel
				}
			} else if mv.Value != 0 {
				payload = "ON"
			} else {
				payload = "OFF"
			}
		case "select":
			if mv.EnumLabel == "" {
				mqttLog.Debugf("device %s: select %s raw %.0f has no enum label; skipping (not an advertised option)", dev.Name, ms.Name, mv.Value)
				continue
			}
			payload = mv.EnumLabel
		case "number":
			payload = fmt.Sprintf("%v", mv.Value)
		default:
			continue
		}
		p.client.Publish(stateTopic, qos, false, payload)
	}
}

func (p *Publisher) publishState(dev config.Device, metrics map[string]echonet.MetricValue) {
	stateTopic := fmt.Sprintf("%s/%s/state", p.topicPrefix, dev.Name)
	payload := make(map[string]any, len(metrics))
	for name, mv := range metrics {
		payload[name] = mv.Value
		if mv.EnumLabel != "" {
			payload[name+"_str"] = mv.EnumLabel
		}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		mqttLog.Warnf("marshal state for %s: %v", dev.Name, err)
		return
	}
	token := p.client.Publish(stateTopic, qos, false, data)
	if !token.WaitTimeout(publishTimeout) {
		mqttLog.Warnf("publish state timeout for %s", dev.Name)
	}
}

func shouldSkipStateUpdate(ms specs.MetricSpec, mv echonet.MetricValue) bool {
	if ms.NumberMin != nil && mv.Value < *ms.NumberMin {
		return true
	}
	if ms.NumberMax != nil && mv.Value > *ms.NumberMax {
		return true
	}
	return false
}

func (p *Publisher) publishAvailability(dev config.Device, online bool) {
	topic := fmt.Sprintf("%s/%s/availability", p.topicPrefix, dev.Name)
	payload := "offline"
	if online {
		payload = "online"
	}
	token := p.client.Publish(topic, qos, true, payload)
	if !token.WaitTimeout(publishTimeout) {
		mqttLog.Warnf("publish availability timeout for %s", dev.Name)
	}
}
