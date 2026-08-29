package mqtt

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	pahomqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/styygeli/echonetgo/internal/config"
	"github.com/styygeli/echonetgo/internal/echonet"
	"github.com/styygeli/echonetgo/internal/poller"
	"github.com/styygeli/echonetgo/internal/specs"
)

const (
	operationStatusEPC = 0x80
	onStatus           = 0x30
	offStatus          = 0x31
	// addrPort is the ECHONET Lite UDP port suffix appended to a device IP.
	addrPort = ":3610"
)

// CapabilityReconciler triggers capability discovery when a command touches an unmapped device.
type CapabilityReconciler interface {
	ReconcileDevice(ctx context.Context, client *echonet.Client, dev config.Device, eoj [3]byte)
}

// Commander subscribes to MQTT command topics and performs ECHONET SET requests.
type Commander struct {
	client      *echonet.Client
	cache       *poller.Cache
	cfg         *config.Config
	topicPrefix string
	reconciler  CapabilityReconciler
}

// commandTimeout bounds a single synchronous SET command (including any
// pre-set and the SetC response wait).
const commandTimeout = 5 * time.Second

// NewCommander creates a Commander. Call Run to subscribe and process commands.
func NewCommander(client *echonet.Client, cache *poller.Cache, cfg *config.Config, topicPrefix string) *Commander {
	return &Commander{
		client:      client,
		cache:       cache,
		cfg:         cfg,
		topicPrefix: topicPrefix,
	}
}

// SetReconciler configures an optional capability reconciler to heal missing maps on demand.
func (c *Commander) SetReconciler(r CapabilityReconciler) {
	c.reconciler = r
}

// Run subscribes to command topics and blocks until ctx is cancelled.
// If readyFunc is non-nil, it is called once all subscriptions have succeeded.
func (c *Commander) Run(ctx context.Context, mqttPub *Publisher, readyFunc func()) {
	if c.topicPrefix == "" {
		c.topicPrefix = "echonetgo"
	}

	var readyOnce sync.Once

	mqttPub.RegisterOnConnect(func(mqttClient pahomqtt.Client) {
		// paho's MessageHandler signature carries no context, so capture the
		// service-lifetime ctx in closures and thread it to each handler. The
		// background verification goroutines need a context that survives the
		// handler return but is cancelled on shutdown.
		// Subscribe to climate command topics: {prefix}/{device}/climate/#
		climateTopic := c.topicPrefix + "/+/climate/#"
		token := mqttClient.Subscribe(climateTopic, 1, func(cl pahomqtt.Client, m pahomqtt.Message) {
			c.handleClimateMessage(ctx, cl, m)
		})
		if !token.WaitTimeout(connectTimeout) {
			mqttLog.Warnf("commander subscribe timeout for %s", climateTopic)
		} else if err := token.Error(); err != nil {
			mqttLog.Warnf("commander subscribe failed: %v", err)
		}

		// Subscribe to light command topics: {prefix}/{device}/light/#
		lightTopic := c.topicPrefix + "/+/light/#"
		tok := mqttClient.Subscribe(lightTopic, 1, func(cl pahomqtt.Client, m pahomqtt.Message) {
			c.handleLightMessage(ctx, cl, m)
		})
		if !tok.WaitTimeout(connectTimeout) || tok.Error() != nil {
			mqttLog.Warnf("commander subscribe failed for %s", lightTopic)
		}

		// Subscribe to switch/select/number command topics
		for _, entityType := range []string{"switch", "select", "number"} {
			topic := c.topicPrefix + "/+/" + entityType + "/+/set"
			tok := mqttClient.Subscribe(topic, 1, func(cl pahomqtt.Client, m pahomqtt.Message) {
				c.handleWritableMessage(ctx, cl, m)
			})
			if !tok.WaitTimeout(connectTimeout) || tok.Error() != nil {
				mqttLog.Warnf("commander subscribe failed for %s", topic)
			}
		}
		mqttLog.Infof("commander subscribed to %s, %s, and switch/select/number topics", climateTopic, lightTopic)

		if readyFunc != nil {
			readyOnce.Do(readyFunc)
		}
	})

	<-ctx.Done()
	mqttClient := mqttPub.Client()
	if mqttClient != nil {
		_ = mqttClient.Unsubscribe(c.topicPrefix + "/+/climate/#")
		_ = mqttClient.Unsubscribe(c.topicPrefix + "/+/light/#")
		for _, entityType := range []string{"switch", "select", "number"} {
			_ = mqttClient.Unsubscribe(c.topicPrefix + "/+/" + entityType + "/+/set")
		}
	}
}

func (c *Commander) handleClimateMessage(lifetimeCtx context.Context, _ pahomqtt.Client, msg pahomqtt.Message) {
	topic := msg.Topic()
	payload := strings.TrimSpace(string(msg.Payload()))
	if payload == "" {
		return
	}
	// topic = prefix/deviceName/climate/attr/set or prefix/deviceName/climate/attr
	parts := strings.Split(topic, "/")
	if len(parts) < 4 {
		return
	}
	// parts[0]=prefix, parts[1]=deviceName, parts[2]=climate, parts[3]=attr, optional parts[4]=set
	deviceName := parts[1]
	if parts[2] != "climate" {
		return
	}
	attr := parts[3]
	isSet := len(parts) > 4 && parts[4] == "set"
	if !isSet {
		return
	}
	dev := c.deviceByName(deviceName)
	if dev == nil {
		mqttLog.Warnf("commander: unknown device %q", deviceName)
		return
	}
	eoj, ok := c.cache.GetDeviceEOJ(*dev)
	if !ok {
		mqttLog.Warnf("commander: no EOJ for device %s", deviceName)
		return
	}
	specs, _ := c.cache.GetDeviceSpecs(*dev)
	climateSpec := c.cache.GetDeviceClimate(*dev)
	writable, hasMap := c.cache.GetWritableEPCs(*dev)
	if !hasMap && c.reconciler != nil {
		c.reconciler.ReconcileDevice(lifetimeCtx, c.client, *dev, eoj)
	}

	addr := dev.IP + addrPort

	switch attr {
	case "mode":
		c.handleClimateMode(lifetimeCtx, addr, eoj, dev, payload, climateSpec, specs)
	case "temperature":
		c.handleClimateTemperature(lifetimeCtx, addr, eoj, dev, payload, climateSpec, specs, writable, hasMap)
	case "fan_mode":
		c.handleClimateFanMode(lifetimeCtx, addr, eoj, dev, payload, climateSpec, specs, writable, hasMap)
	case "power":
		c.handleClimatePower(lifetimeCtx, addr, eoj, dev, payload, writable, hasMap)
	default:
		mqttLog.Debugf("commander: ignored climate attribute %q", attr)
	}
}

// handleWritableMessage handles switch/select/number command messages: prefix/device/switch|select|number/metricname/set
func (c *Commander) handleWritableMessage(lifetimeCtx context.Context, _ pahomqtt.Client, msg pahomqtt.Message) {
	topic := msg.Topic()
	payload := strings.TrimSpace(string(msg.Payload()))
	if payload == "" {
		return
	}
	parts := strings.Split(topic, "/")
	if len(parts) != 5 || parts[4] != "set" {
		return
	}
	deviceName := parts[1]
	entityType := parts[2]
	metricName := parts[3]
	if entityType != "switch" && entityType != "select" && entityType != "number" {
		return
	}
	dev := c.deviceByName(deviceName)
	if dev == nil {
		mqttLog.Warnf("commander: unknown device %q", deviceName)
		return
	}
	eoj, ok := c.cache.GetDeviceEOJ(*dev)
	if !ok {
		mqttLog.Warnf("commander: no EOJ for device %s", deviceName)
		return
	}
	metricSpecs, ok := c.cache.GetDeviceSpecs(*dev)
	if !ok {
		return
	}
	writable, hasMap := c.cache.GetWritableEPCs(*dev)
	climateSpec := c.cache.GetDeviceClimate(*dev)
	lightSpec := c.cache.GetDeviceLight(*dev)
	ms := metricSpecByName(metricSpecs, metricName)
	if ms == nil {
		mqttLog.Warnf("commander: unknown metric %q for device %s", metricName, deviceName)
		return
	}
	if hasMap {
		if _, ok := writable[ms.EPC]; !ok {
			mqttLog.Warnf("commander: metric %s (EPC 0x%02x) not writable per SETMAP", metricName, ms.EPC)
			return
		}
	} else if c.reconciler != nil {
		c.reconciler.ReconcileDevice(lifetimeCtx, c.client, *dev, eoj)
	}
	if isClimateEPC(ms.EPC, climateSpec) {
		return
	}
	if isLightEPC(ms.EPC, lightSpec) {
		return
	}
	if ms.ExcludeSet {
		return
	}
	addr := dev.IP + addrPort
	c.executeWritableSet(lifetimeCtx, addr, eoj, dev, ms, metricSpecs, entityType, payload)
}

func metricSpecByName(specs []specs.MetricSpec, name string) *specs.MetricSpec {
	for i := range specs {
		if specs[i].Name == name {
			return &specs[i]
		}
	}
	return nil
}

func (c *Commander) executeWritableSet(lifetimeCtx context.Context, addr string, eoj [3]byte, dev *config.Device, ms *specs.MetricSpec, metricSpecs []specs.MetricSpec, entityType, payload string) {
	ctx, cancel := context.WithTimeout(lifetimeCtx, commandTimeout)
	defer cancel()
	var preEDT []byte
	if ms.PreSetEPC != 0 {
		preMs := metricSpecByEPC(metricSpecs, ms.PreSetEPC)
		if preMs == nil {
			mqttLog.Warnf("commander: pre-set EPC 0x%02x not found in specs for %s", ms.PreSetEPC, dev.Name)
			return
		}
		var err error
		preEDT, err = echonet.EncodeValueToEDT(float64(ms.PreSetValue), *preMs)
		if err != nil {
			mqttLog.Warnf("commander: pre-set encode failed for 0x%02x: %v", ms.PreSetEPC, err)
			return
		}
		_, err = c.client.SendSet(ctx, addr, eoj, ms.PreSetEPC, preEDT)
		if err != nil {
			mqttLog.Warnf("commander: pre-set 0x%02x failed for %s: %v (continuing)", ms.PreSetEPC, dev.Name, err)
		} else {
			mqttLog.Infof("commander: pre-set %s EPC 0x%02x = 0x%02x", dev.Name, ms.PreSetEPC, ms.PreSetValue)
		}
	}
	var value float64
	switch entityType {
	case "switch":
		switch strings.ToUpper(payload) {
		case "ON", "1", "TRUE":
			if raw, ok := ms.ReverseEnum["on"]; ok {
				value = float64(raw)
			} else {
				value = 1
			}
		case "OFF", "0", "FALSE":
			if raw, ok := ms.ReverseEnum["off"]; ok {
				value = float64(raw)
			} else {
				value = 0
			}
		default:
			mqttLog.Warnf("commander: invalid switch payload %q", payload)
			return
		}
	case "select":
		raw, ok := ms.ReverseEnum[payload]
		if !ok {
			mqttLog.Warnf("commander: unknown select option %q for %s", payload, ms.Name)
			return
		}
		value = float64(raw)
	case "number":
		var err error
		value, err = strconv.ParseFloat(payload, 64)
		if err != nil {
			mqttLog.Warnf("commander: invalid number payload %q: %v", payload, err)
			return
		}
	default:
		return
	}
	edt, err := echonet.EncodeValueToEDT(value, *ms)
	if err != nil {
		mqttLog.Warnf("commander: encode failed for %s: %v", ms.Name, err)
		return
	}
	if ms.SetMode == "seti" {
		err = c.client.SendSetI(ctx, addr, eoj, ms.EPC, edt)
		if err != nil {
			mqttLog.Warnf("commander: SetI %s (0x%02x) failed for %s: %v", ms.Name, ms.EPC, dev.Name, err)
			return
		}
		mqttLog.Infof("commander: seti %s %s = %s (fire-and-forget)", dev.Name, ms.Name, payload)
		return
	}
	_, err = c.client.SendSet(ctx, addr, eoj, ms.EPC, edt)
	if err != nil {
		mqttLog.Warnf("commander: Set %s (0x%02x) failed for %s: %v", ms.Name, ms.EPC, dev.Name, err)
		c.triggerStateUpdate(lifetimeCtx, dev, 0, eoj, ms.EPC)
		return
	}
	mqttLog.Infof("commander: set %s %s = %s", dev.Name, ms.Name, payload)
	updates := []pendingUpdate{{epc: ms.EPC, edt: edt}}
	if ms.PreSetEPC != 0 {
		updates = append(updates, pendingUpdate{epc: ms.PreSetEPC, edt: preEDT})
	}
	c.verifyStateUpdate(lifetimeCtx, dev, eoj, updates)
}

func (c *Commander) deviceByName(name string) *config.Device {
	for i := range c.cfg.Devices {
		if c.cfg.Devices[i].Name == name {
			return &c.cfg.Devices[i]
		}
	}
	return nil
}

func (c *Commander) handleClimatePower(lifetimeCtx context.Context, addr string, eoj [3]byte, dev *config.Device, payload string, writable map[byte]struct{}, hasMap bool) {
	if hasMap {
		if _, ok := writable[operationStatusEPC]; !ok {
			mqttLog.Warnf("commander: device %s operation_status (0x80) not writable per SETMAP", dev.Name)
			return
		}
	} else if c.reconciler != nil {
		c.reconciler.ReconcileDevice(lifetimeCtx, c.client, *dev, eoj)
	}
	var edt []byte
	switch strings.ToUpper(payload) {
	case "ON", "1", "TRUE":
		edt = []byte{onStatus}
	case "OFF", "0", "FALSE":
		edt = []byte{offStatus}
	default:
		mqttLog.Warnf("commander: invalid power payload %q", payload)
		return
	}
	ctx, cancel := context.WithTimeout(lifetimeCtx, commandTimeout)
	defer cancel()
	_, err := c.client.SendSet(ctx, addr, eoj, operationStatusEPC, edt)
	if err != nil {
		mqttLog.Warnf("commander: Set 0x80 failed for %s: %v", dev.Name, err)
		c.triggerStateUpdate(lifetimeCtx, dev, 0, eoj, operationStatusEPC)
		return
	}
	mqttLog.Infof("commander: set %s power %s", dev.Name, payload)
	c.verifyStateUpdate(lifetimeCtx, dev, eoj, []pendingUpdate{{epc: operationStatusEPC, edt: edt}})
}

func (c *Commander) handleClimateMode(lifetimeCtx context.Context, addr string, eoj [3]byte, dev *config.Device, payload string, climateSpec *specs.ClimateSpec, metricSpecs []specs.MetricSpec) {
	if climateSpec == nil {
		mqttLog.Warnf("commander: device %s has no climate spec", dev.Name)
		return
	}
	ctx, cancel := context.WithTimeout(lifetimeCtx, commandTimeout)
	defer cancel()
	payload = strings.ToLower(payload)
	if payload == "off" {
		_, err := c.client.SendSet(ctx, addr, eoj, operationStatusEPC, []byte{offStatus})
		if err != nil {
			mqttLog.Warnf("commander: Set 0x80=off failed for %s: %v", dev.Name, err)
			c.triggerStateUpdate(lifetimeCtx, dev, 0, eoj, operationStatusEPC)
			return
		}
		mqttLog.Infof("commander: set %s mode off", dev.Name)
		c.verifyStateUpdate(lifetimeCtx, dev, eoj, []pendingUpdate{{epc: operationStatusEPC, edt: []byte{offStatus}}})
		return
	}
	// Turn on first, then set operation mode
	_, err := c.client.SendSet(ctx, addr, eoj, operationStatusEPC, []byte{onStatus})
	if err != nil {
		mqttLog.Warnf("commander: Set 0x80=on failed for %s: %v", dev.Name, err)
		c.triggerStateUpdate(lifetimeCtx, dev, 0, eoj, operationStatusEPC)
		return
	}
	raw, ok := climateSpec.Modes[payload]
	if !ok || raw == nil {
		mqttLog.Warnf("commander: unknown mode %q for %s", payload, dev.Name)
		return
	}
	epc := climateSpec.ModeEPC
	ms := metricSpecByEPC(metricSpecs, epc)
	if ms == nil {
		mqttLog.Warnf("commander: no metric spec for mode EPC 0x%02x", epc)
		return
	}
	edt, err := echonet.EncodeValueToEDT(float64(*raw), *ms)
	if err != nil {
		mqttLog.Warnf("commander: encode mode failed: %v", err)
		return
	}
	_, err = c.client.SendSet(ctx, addr, eoj, epc, edt)
	if err != nil {
		mqttLog.Warnf("commander: Set mode failed for %s: %v", dev.Name, err)
		c.triggerStateUpdate(lifetimeCtx, dev, 0, eoj, operationStatusEPC, epc)
		return
	}
	mqttLog.Infof("commander: set %s mode %s", dev.Name, payload)
	c.verifyStateUpdate(lifetimeCtx, dev, eoj, []pendingUpdate{{epc: operationStatusEPC, edt: []byte{onStatus}}, {epc: epc, edt: edt}})
}

func (c *Commander) handleClimateTemperature(lifetimeCtx context.Context, addr string, eoj [3]byte, dev *config.Device, payload string, climateSpec *specs.ClimateSpec, metricSpecs []specs.MetricSpec, writable map[byte]struct{}, hasMap bool) {
	if climateSpec == nil {
		return
	}
	epc := climateSpec.TemperatureEPC
	if hasMap {
		if _, ok := writable[epc]; !ok {
			mqttLog.Warnf("commander: device %s temperature EPC 0x%02x not writable per SETMAP", dev.Name, epc)
			return
		}
	} else if c.reconciler != nil {
		c.reconciler.ReconcileDevice(lifetimeCtx, c.client, *dev, eoj)
	}
	temp, err := strconv.ParseFloat(payload, 64)
	if err != nil {
		mqttLog.Warnf("commander: invalid temperature payload %q: %v", payload, err)
		return
	}
	ms := metricSpecByEPC(metricSpecs, epc)
	if ms == nil {
		mqttLog.Warnf("commander: no metric spec for temperature EPC 0x%02x", epc)
		return
	}
	_, _, _, cached := c.cache.Get(*dev)
	prev, hasPrev := 0.0, false
	if mv, ok := cached[ms.Name]; ok {
		prev, hasPrev = mv.Value, true
	}
	temp = normalizeClimateSetpoint(temp, prev, hasPrev, *ms)
	edt, err := echonet.EncodeValueToEDT(temp, *ms)
	if err != nil {
		mqttLog.Warnf("commander: encode temperature failed: %v", err)
		return
	}
	ctx, cancel := context.WithTimeout(lifetimeCtx, commandTimeout)
	defer cancel()
	_, err = c.client.SendSet(ctx, addr, eoj, epc, edt)
	if err != nil {
		mqttLog.Warnf("commander: Set temperature failed for %s: %v", dev.Name, err)
		c.triggerStateUpdate(lifetimeCtx, dev, 0, eoj, epc)
		return
	}
	mqttLog.Infof("commander: set %s temperature %s", dev.Name, payload)
	c.verifyStateUpdate(lifetimeCtx, dev, eoj, []pendingUpdate{{epc: epc, edt: edt}})
}

// normalizeClimateSetpoint rounds a requested temperature to the integer
// resolution used by most ECHONET Lite HVAC devices. When the request has a
// .5 fractional part, it rounds in the direction of change relative to prev
// (up when raising, down when lowering) so Matter bridges that emit 0.5°C
// steps produce the setpoint the user intended. Other fractions use
// nearest-integer rounding; integer inputs and sub-integer-scale metrics
// pass through.
func normalizeClimateSetpoint(req, prev float64, hasPrev bool, ms specs.MetricSpec) float64 {
	scale := ms.Scale
	if scale == 0 {
		scale = 1
	}
	if scale != 1 {
		return req
	}
	if req == math.Trunc(req) {
		return req
	}
	frac := req - math.Floor(req)
	if math.Abs(frac-0.5) < 1e-9 && hasPrev {
		if req >= prev {
			return math.Ceil(req)
		}
		return math.Floor(req)
	}
	return math.Round(req)
}

func (c *Commander) handleClimateFanMode(lifetimeCtx context.Context, addr string, eoj [3]byte, dev *config.Device, payload string, climateSpec *specs.ClimateSpec, metricSpecs []specs.MetricSpec, writable map[byte]struct{}, hasMap bool) {
	if climateSpec == nil || climateSpec.FanModeEPC == 0 {
		return
	}
	epc := climateSpec.FanModeEPC
	if hasMap {
		if _, ok := writable[epc]; !ok {
			mqttLog.Warnf("commander: device %s fan_mode EPC 0x%02x not writable per SETMAP", dev.Name, epc)
			return
		}
	} else if c.reconciler != nil {
		c.reconciler.ReconcileDevice(lifetimeCtx, c.client, *dev, eoj)
	}
	ms := metricSpecByEPC(metricSpecs, epc)
	if ms == nil || len(ms.ReverseEnum) == 0 {
		mqttLog.Warnf("commander: no metric spec or ReverseEnum for fan EPC 0x%02x", epc)
		return
	}
	raw, ok := ms.ReverseEnum[payload]
	if !ok {
		mqttLog.Warnf("commander: unknown fan_mode %q for %s", payload, dev.Name)
		return
	}
	edt, err := echonet.EncodeValueToEDT(float64(raw), *ms)
	if err != nil {
		mqttLog.Warnf("commander: encode fan_mode failed: %v", err)
		return
	}
	ctx, cancel := context.WithTimeout(lifetimeCtx, commandTimeout)
	defer cancel()
	_, err = c.client.SendSet(ctx, addr, eoj, epc, edt)
	if err != nil {
		mqttLog.Warnf("commander: Set fan_mode failed for %s: %v", dev.Name, err)
		c.triggerStateUpdate(lifetimeCtx, dev, 0, eoj, epc)
		return
	}
	mqttLog.Infof("commander: set %s fan_mode %s", dev.Name, payload)
	c.verifyStateUpdate(lifetimeCtx, dev, eoj, []pendingUpdate{{epc: epc, edt: edt}})
}

func (c *Commander) handleLightMessage(lifetimeCtx context.Context, _ pahomqtt.Client, msg pahomqtt.Message) {
	topic := msg.Topic()
	payload := strings.TrimSpace(string(msg.Payload()))
	if payload == "" {
		return
	}
	parts := strings.Split(topic, "/")
	if len(parts) < 4 {
		return
	}
	deviceName := parts[1]
	if parts[2] != "light" {
		return
	}
	attr := parts[3]
	isSet := len(parts) > 4 && parts[4] == "set"
	if !isSet {
		return
	}
	dev := c.deviceByName(deviceName)
	if dev == nil {
		mqttLog.Warnf("commander: unknown device %q", deviceName)
		return
	}
	eoj, ok := c.cache.GetDeviceEOJ(*dev)
	if !ok {
		mqttLog.Warnf("commander: no EOJ for device %s", deviceName)
		return
	}
	metricSpecs, _ := c.cache.GetDeviceSpecs(*dev)
	lightSpec := c.cache.GetDeviceLight(*dev)
	writable, hasMap := c.cache.GetWritableEPCs(*dev)
	if !hasMap && c.reconciler != nil {
		c.reconciler.ReconcileDevice(lifetimeCtx, c.client, *dev, eoj)
	}

	addr := dev.IP + addrPort

	switch attr {
	case "power":
		c.handleLightPower(lifetimeCtx, addr, eoj, dev, payload, writable, hasMap)
	case "brightness":
		c.handleLightBrightness(lifetimeCtx, addr, eoj, dev, payload, lightSpec, metricSpecs, writable, hasMap)
	case "effect":
		c.handleLightEffect(lifetimeCtx, addr, eoj, dev, payload, lightSpec, metricSpecs, writable, hasMap)
	default:
		mqttLog.Debugf("commander: ignored light attribute %q", attr)
	}
}

func (c *Commander) handleLightPower(lifetimeCtx context.Context, addr string, eoj [3]byte, dev *config.Device, payload string, writable map[byte]struct{}, hasMap bool) {
	if hasMap {
		if _, ok := writable[operationStatusEPC]; !ok {
			mqttLog.Warnf("commander: device %s operation_status (0x80) not writable per SETMAP", dev.Name)
			return
		}
	} else if c.reconciler != nil {
		c.reconciler.ReconcileDevice(lifetimeCtx, c.client, *dev, eoj)
	}
	var edt []byte
	switch strings.ToUpper(payload) {
	case "ON", "1", "TRUE":
		edt = []byte{onStatus}
	case "OFF", "0", "FALSE":
		edt = []byte{offStatus}
	default:
		mqttLog.Warnf("commander: invalid light power payload %q", payload)
		return
	}
	ctx, cancel := context.WithTimeout(lifetimeCtx, commandTimeout)
	defer cancel()
	_, err := c.client.SendSet(ctx, addr, eoj, operationStatusEPC, edt)
	if err != nil {
		mqttLog.Warnf("commander: Set 0x80 failed for %s: %v", dev.Name, err)
		c.triggerStateUpdate(lifetimeCtx, dev, 0, eoj, operationStatusEPC)
		return
	}
	mqttLog.Infof("commander: set %s light power %s", dev.Name, payload)
	c.verifyStateUpdate(lifetimeCtx, dev, eoj, []pendingUpdate{{epc: operationStatusEPC, edt: edt}})
}

func (c *Commander) handleLightBrightness(lifetimeCtx context.Context, addr string, eoj [3]byte, dev *config.Device, payload string, lightSpec *specs.LightSpec, metricSpecs []specs.MetricSpec, writable map[byte]struct{}, hasMap bool) {
	if lightSpec == nil || lightSpec.BrightnessEPC == 0 {
		return
	}
	epc := lightSpec.BrightnessEPC
	if hasMap {
		if _, ok := writable[epc]; !ok {
			mqttLog.Warnf("commander: device %s brightness EPC 0x%02x not writable per SETMAP", dev.Name, epc)
			return
		}
	} else if c.reconciler != nil {
		c.reconciler.ReconcileDevice(lifetimeCtx, c.client, *dev, eoj)
	}
	brightness, err := strconv.ParseFloat(payload, 64)
	if err != nil {
		mqttLog.Warnf("commander: invalid brightness payload %q: %v", payload, err)
		return
	}
	ms := metricSpecByEPC(metricSpecs, epc)
	if ms == nil {
		mqttLog.Warnf("commander: no metric spec for brightness EPC 0x%02x", epc)
		return
	}
	edt, err := echonet.EncodeValueToEDT(brightness, *ms)
	if err != nil {
		mqttLog.Warnf("commander: encode brightness failed: %v", err)
		return
	}
	ctx, cancel := context.WithTimeout(lifetimeCtx, commandTimeout)
	defer cancel()
	_, err = c.client.SendSet(ctx, addr, eoj, epc, edt)
	if err != nil {
		mqttLog.Warnf("commander: Set brightness failed for %s: %v", dev.Name, err)
		c.triggerStateUpdate(lifetimeCtx, dev, 0, eoj, epc)
		return
	}
	mqttLog.Infof("commander: set %s brightness %s", dev.Name, payload)
	c.verifyStateUpdate(lifetimeCtx, dev, eoj, []pendingUpdate{{epc: epc, edt: edt}})
}

func (c *Commander) handleLightEffect(lifetimeCtx context.Context, addr string, eoj [3]byte, dev *config.Device, payload string, lightSpec *specs.LightSpec, metricSpecs []specs.MetricSpec, writable map[byte]struct{}, hasMap bool) {
	if lightSpec == nil {
		return
	}
	var epc byte
	var value float64

	if lightSpec.ColorSettingEPC != 0 {
		epc = lightSpec.ColorSettingEPC
		raw, ok := lightSpec.ColorSettings[payload]
		if !ok {
			mqttLog.Warnf("commander: unknown light effect %q for %s", payload, dev.Name)
			return
		}
		value = float64(raw)
	} else if lightSpec.SceneEPC != 0 {
		epc = lightSpec.SceneEPC
		var sceneNum int
		if _, err := fmt.Sscanf(payload, "scene_%d", &sceneNum); err != nil {
			mqttLog.Warnf("commander: invalid scene effect %q for %s: %v", payload, dev.Name, err)
			return
		}
		if sceneNum < 1 || (lightSpec.MaxScenes > 0 && sceneNum > lightSpec.MaxScenes) {
			mqttLog.Warnf("commander: scene %d out of range for %s (max %d)", sceneNum, dev.Name, lightSpec.MaxScenes)
			return
		}
		value = float64(sceneNum)
	} else {
		return
	}

	if hasMap {
		if _, ok := writable[epc]; !ok {
			mqttLog.Warnf("commander: device %s effect EPC 0x%02x not writable per SETMAP", dev.Name, epc)
			return
		}
	} else if c.reconciler != nil {
		c.reconciler.ReconcileDevice(lifetimeCtx, c.client, *dev, eoj)
	}
	ms := metricSpecByEPC(metricSpecs, epc)
	if ms == nil {
		mqttLog.Warnf("commander: no metric spec for effect EPC 0x%02x", epc)
		return
	}
	edt, err := echonet.EncodeValueToEDT(value, *ms)
	if err != nil {
		mqttLog.Warnf("commander: encode effect failed: %v", err)
		return
	}
	ctx, cancel := context.WithTimeout(lifetimeCtx, commandTimeout)
	defer cancel()
	_, err = c.client.SendSet(ctx, addr, eoj, epc, edt)
	if err != nil {
		mqttLog.Warnf("commander: Set effect failed for %s: %v", dev.Name, err)
		c.triggerStateUpdate(lifetimeCtx, dev, 0, eoj, epc)
		return
	}
	mqttLog.Infof("commander: set %s effect %s", dev.Name, payload)
	c.verifyStateUpdate(lifetimeCtx, dev, eoj, []pendingUpdate{{epc: epc, edt: edt}})
}

func metricSpecByEPC(specs []specs.MetricSpec, epc byte) *specs.MetricSpec {
	for i := range specs {
		if specs[i].EPC == epc {
			return &specs[i]
		}
	}
	return nil
}

type pendingUpdate struct {
	epc byte
	edt []byte
}

func (c *Commander) verifyStateUpdate(lifetimeCtx context.Context, dev *config.Device, eoj [3]byte, updates []pendingUpdate) {
	go func() {
		defer mqttLog.RecoverPanic("verify state update for " + dev.Name)
		delays := []time.Duration{1 * time.Second, 3 * time.Second, 3 * time.Second}
		for attempt, delay := range delays {
			select {
			case <-lifetimeCtx.Done():
				return
			case <-time.After(delay):
			}

			ctx, cancel := context.WithTimeout(lifetimeCtx, commandTimeout)
			epcs := make([]byte, len(updates))
			for i, u := range updates {
				epcs[i] = u.epc
			}
			props, err := c.client.GetProps(ctx, dev.IP, eoj, epcs)
			cancel()

			if err != nil {
				mqttLog.Warnf("commander: failed verify read for %s (attempt %d): %v", dev.Name, attempt+1, err)
				continue
			}

			allMatched := true
			for _, u := range updates {
				found := false
				for _, p := range props {
					if p.EPC == u.epc {
						found = true
						if !bytes.Equal(p.EDT, u.edt) {
							allMatched = false
						}
						break
					}
				}
				if !found {
					allMatched = false
				}
				if !allMatched {
					break
				}
			}

			if allMatched || attempt == len(delays)-1 {
				deviceSpecs, ok := c.cache.GetDeviceSpecs(*dev)
				if !ok {
					return
				}
				var specsForRequested []specs.MetricSpec
				for _, p := range props {
					if ms := metricSpecByEPC(deviceSpecs, p.EPC); ms != nil {
						specsForRequested = append(specsForRequested, *ms)
					}
				}
				metrics := echonet.ParsePropsToMetrics(props, specsForRequested)
				if len(metrics) > 0 {
					c.cache.MergeMetrics(*dev, metrics)
					if !allMatched {
						mqttLog.Warnf("commander: device %s did not reflect requested state after retries", dev.Name)
					} else {
						mqttLog.Infof("commander: verified device %s updated successfully on attempt %d", dev.Name, attempt+1)
					}
				}
				return
			}
		}
	}()
}

func (c *Commander) triggerStateUpdate(lifetimeCtx context.Context, dev *config.Device, delay time.Duration, eoj [3]byte, epcs ...byte) {
	go func() {
		defer mqttLog.RecoverPanic("trigger state update for " + dev.Name)
		if delay > 0 {
			select {
			case <-lifetimeCtx.Done():
				return
			case <-time.After(delay):
			}
		} else {
			if lifetimeCtx.Err() != nil {
				return
			}
		}
		ctx, cancel := context.WithTimeout(lifetimeCtx, commandTimeout)
		defer cancel()

		props, err := c.client.GetProps(ctx, dev.IP, eoj, epcs)
		if err != nil {
			mqttLog.Warnf("commander: failed delayed read for %s: %v", dev.Name, err)
			return
		}

		deviceSpecs, ok := c.cache.GetDeviceSpecs(*dev)
		if !ok {
			return
		}
		// Only parse specs for EPCs we requested; passing full deviceSpecs would log
		// "missing EPC" for every other property we didn't ask for.
		requestedEPCs := make(map[byte]struct{}, len(epcs))
		for _, epc := range epcs {
			requestedEPCs[epc] = struct{}{}
		}
		var specsForRequested []specs.MetricSpec
		for _, ms := range deviceSpecs {
			if _, ok := requestedEPCs[ms.EPC]; ok {
				specsForRequested = append(specsForRequested, ms)
			}
		}

		metrics := echonet.ParsePropsToMetrics(props, specsForRequested)
		if len(metrics) > 0 {
			c.cache.MergeMetrics(*dev, metrics)
			mqttLog.Debugf("commander: immediate update for %s parsed %d metrics", dev.Name, len(metrics))
		}
	}()
}
