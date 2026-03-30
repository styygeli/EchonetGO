package mqtt

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
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
)

// Commander subscribes to MQTT command topics and performs ECHONET SET requests.
type Commander struct {
	client      *echonet.Client
	cache       *poller.Cache
	cfg         *config.Config
	topicPrefix string
	subscribed  pahomqtt.Token
	ctx         context.Context
}

// NewCommander creates a Commander. Call Run to subscribe and process commands.
func NewCommander(client *echonet.Client, cache *poller.Cache, cfg *config.Config, topicPrefix string) *Commander {
	return &Commander{
		client:      client,
		cache:       cache,
		cfg:         cfg,
		topicPrefix: topicPrefix,
	}
}

// Run subscribes to command topics and blocks until ctx is cancelled.
// If readyFunc is non-nil, it is called once all subscriptions have succeeded.
func (c *Commander) Run(ctx context.Context, mqttClient pahomqtt.Client, readyFunc func()) {
	c.ctx = ctx
	if c.topicPrefix == "" {
		c.topicPrefix = "echonetgo"
	}
	// Subscribe to climate command topics: {prefix}/{device}/climate/#
	climateTopic := c.topicPrefix + "/+/climate/#"
	token := mqttClient.Subscribe(climateTopic, 1, c.handleClimateMessage)
	c.subscribed = token
	if !token.WaitTimeout(connectTimeout) {
		mqttLog.Warnf("commander subscribe timeout for %s", climateTopic)
		return
	}
	if err := token.Error(); err != nil {
		mqttLog.Warnf("commander subscribe failed: %v", err)
		return
	}
	// Subscribe to light command topics: {prefix}/{device}/light/#
	lightTopic := c.topicPrefix + "/+/light/#"
	tok := mqttClient.Subscribe(lightTopic, 1, c.handleLightMessage)
	if !tok.WaitTimeout(connectTimeout) || tok.Error() != nil {
		mqttLog.Warnf("commander subscribe failed for %s", lightTopic)
	}
	// Subscribe to switch/select/number command topics
	for _, entityType := range []string{"switch", "select", "number"} {
		topic := c.topicPrefix + "/+/" + entityType + "/+/set"
		tok := mqttClient.Subscribe(topic, 1, c.handleWritableMessage)
		if !tok.WaitTimeout(connectTimeout) || tok.Error() != nil {
			mqttLog.Warnf("commander subscribe failed for %s", topic)
		}
	}
	mqttLog.Infof("commander subscribed to %s, %s, and switch/select/number", climateTopic, lightTopic)
	if readyFunc != nil {
		readyFunc()
	}
	<-ctx.Done()
	_ = mqttClient.Unsubscribe(climateTopic)
	_ = mqttClient.Unsubscribe(lightTopic)
	for _, entityType := range []string{"switch", "select", "number"} {
		_ = mqttClient.Unsubscribe(c.topicPrefix + "/+/" + entityType + "/+/set")
	}
}

func (c *Commander) handleClimateMessage(_ pahomqtt.Client, msg pahomqtt.Message) {
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
	writable, _ := c.cache.GetWritableEPCs(*dev)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	addr := dev.IP + ":3610"

	switch attr {
	case "mode":
		c.handleClimateMode(ctx, addr, eoj, dev, payload, climateSpec, specs)
	case "temperature":
		c.handleClimateTemperature(ctx, addr, eoj, dev, payload, climateSpec, specs, writable)
	case "fan_mode":
		c.handleClimateFanMode(ctx, addr, eoj, dev, payload, climateSpec, specs, writable)
	case "power":
		c.handleClimatePower(ctx, addr, eoj, dev, payload, writable)
	default:
		mqttLog.Debugf("commander: ignored climate attribute %q", attr)
	}
}

// handleWritableMessage handles switch/select/number command messages: prefix/device/switch|select|number/metricname/set
func (c *Commander) handleWritableMessage(_ pahomqtt.Client, msg pahomqtt.Message) {
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
	writable, _ := c.cache.GetWritableEPCs(*dev)
	climateSpec := c.cache.GetDeviceClimate(*dev)
	lightSpec := c.cache.GetDeviceLight(*dev)
	ms := metricSpecByName(metricSpecs, metricName)
	if ms == nil {
		mqttLog.Warnf("commander: unknown metric %q for device %s", metricName, deviceName)
		return
	}
	if _, ok := writable[ms.EPC]; !ok {
		mqttLog.Warnf("commander: metric %s (EPC 0x%02x) not writable", metricName, ms.EPC)
		return
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	addr := dev.IP + ":3610"
	c.executeWritableSet(ctx, addr, eoj, dev, ms, metricSpecs, entityType, payload)
}

func metricSpecByName(specs []specs.MetricSpec, name string) *specs.MetricSpec {
	for i := range specs {
		if specs[i].Name == name {
			return &specs[i]
		}
	}
	return nil
}

func (c *Commander) executeWritableSet(ctx context.Context, addr string, eoj [3]byte, dev *config.Device, ms *specs.MetricSpec, metricSpecs []specs.MetricSpec, entityType, payload string) {
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
			mqttLog.Warnf("commander: pre-set 0x%02x failed for %s: %v", ms.PreSetEPC, dev.Name, err)
			return
		}
		mqttLog.Infof("commander: pre-set %s EPC 0x%02x = 0x%02x", dev.Name, ms.PreSetEPC, ms.PreSetValue)
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
		c.triggerStateUpdate(dev, 0, eoj, ms.EPC)
		return
	}
	mqttLog.Infof("commander: set %s %s = %s", dev.Name, ms.Name, payload)
	updates := []pendingUpdate{{epc: ms.EPC, edt: edt}}
	if ms.PreSetEPC != 0 {
		updates = append(updates, pendingUpdate{epc: ms.PreSetEPC, edt: preEDT})
	}
	c.verifyStateUpdate(dev, eoj, updates)
}

func (c *Commander) deviceByName(name string) *config.Device {
	for i := range c.cfg.Devices {
		if c.cfg.Devices[i].Name == name {
			return &c.cfg.Devices[i]
		}
	}
	return nil
}

func (c *Commander) handleClimatePower(ctx context.Context, addr string, eoj [3]byte, dev *config.Device, payload string, writable map[byte]struct{}) {
	if _, ok := writable[operationStatusEPC]; !ok {
		mqttLog.Warnf("commander: device %s operation_status (0x80) not writable", dev.Name)
		return
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
	_, err := c.client.SendSet(ctx, addr, eoj, operationStatusEPC, edt)
	if err != nil {
		mqttLog.Warnf("commander: Set 0x80 failed for %s: %v", dev.Name, err)
		c.triggerStateUpdate(dev, 0, eoj, operationStatusEPC)
		return
	}
	mqttLog.Infof("commander: set %s power %s", dev.Name, payload)
	c.verifyStateUpdate(dev, eoj, []pendingUpdate{{epc: operationStatusEPC, edt: edt}})
}

func (c *Commander) handleClimateMode(ctx context.Context, addr string, eoj [3]byte, dev *config.Device, payload string, climateSpec *specs.ClimateSpec, metricSpecs []specs.MetricSpec) {
	if climateSpec == nil {
		mqttLog.Warnf("commander: device %s has no climate spec", dev.Name)
		return
	}
	payload = strings.ToLower(payload)
	if payload == "off" {
		_, err := c.client.SendSet(ctx, addr, eoj, operationStatusEPC, []byte{offStatus})
		if err != nil {
			mqttLog.Warnf("commander: Set 0x80=off failed for %s: %v", dev.Name, err)
			c.triggerStateUpdate(dev, 0, eoj, operationStatusEPC)
			return
		}
		mqttLog.Infof("commander: set %s mode off", dev.Name)
		c.verifyStateUpdate(dev, eoj, []pendingUpdate{{epc: operationStatusEPC, edt: []byte{offStatus}}})
		return
	}
	// Turn on first, then set operation mode
	_, err := c.client.SendSet(ctx, addr, eoj, operationStatusEPC, []byte{onStatus})
	if err != nil {
		mqttLog.Warnf("commander: Set 0x80=on failed for %s: %v", dev.Name, err)
		c.triggerStateUpdate(dev, 0, eoj, operationStatusEPC)
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
		c.triggerStateUpdate(dev, 0, eoj, operationStatusEPC, epc)
		return
	}
	mqttLog.Infof("commander: set %s mode %s", dev.Name, payload)
	c.verifyStateUpdate(dev, eoj, []pendingUpdate{{epc: operationStatusEPC, edt: []byte{onStatus}}, {epc: epc, edt: edt}})
}

func (c *Commander) handleClimateTemperature(ctx context.Context, addr string, eoj [3]byte, dev *config.Device, payload string, climateSpec *specs.ClimateSpec, metricSpecs []specs.MetricSpec, writable map[byte]struct{}) {
	if climateSpec == nil {
		return
	}
	epc := climateSpec.TemperatureEPC
	if _, ok := writable[epc]; !ok {
		mqttLog.Warnf("commander: device %s temperature EPC 0x%02x not writable", dev.Name, epc)
		return
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
	edt, err := echonet.EncodeValueToEDT(temp, *ms)
	if err != nil {
		mqttLog.Warnf("commander: encode temperature failed: %v", err)
		return
	}
	_, err = c.client.SendSet(ctx, addr, eoj, epc, edt)
	if err != nil {
		mqttLog.Warnf("commander: Set temperature failed for %s: %v", dev.Name, err)
		c.triggerStateUpdate(dev, 0, eoj, epc)
		return
	}
	mqttLog.Infof("commander: set %s temperature %s", dev.Name, payload)
	c.verifyStateUpdate(dev, eoj, []pendingUpdate{{epc: epc, edt: edt}})
}

func (c *Commander) handleClimateFanMode(ctx context.Context, addr string, eoj [3]byte, dev *config.Device, payload string, climateSpec *specs.ClimateSpec, metricSpecs []specs.MetricSpec, writable map[byte]struct{}) {
	if climateSpec == nil || climateSpec.FanModeEPC == 0 {
		return
	}
	epc := climateSpec.FanModeEPC
	if _, ok := writable[epc]; !ok {
		mqttLog.Warnf("commander: device %s fan_mode EPC 0x%02x not writable", dev.Name, epc)
		return
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
	_, err = c.client.SendSet(ctx, addr, eoj, epc, edt)
	if err != nil {
		mqttLog.Warnf("commander: Set fan_mode failed for %s: %v", dev.Name, err)
		c.triggerStateUpdate(dev, 0, eoj, epc)
		return
	}
	mqttLog.Infof("commander: set %s fan_mode %s", dev.Name, payload)
	c.verifyStateUpdate(dev, eoj, []pendingUpdate{{epc: epc, edt: edt}})
}

func (c *Commander) handleLightMessage(_ pahomqtt.Client, msg pahomqtt.Message) {
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
	writable, _ := c.cache.GetWritableEPCs(*dev)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	addr := dev.IP + ":3610"

	switch attr {
	case "power":
		c.handleLightPower(ctx, addr, eoj, dev, payload, writable)
	case "brightness":
		c.handleLightBrightness(ctx, addr, eoj, dev, payload, lightSpec, metricSpecs, writable)
	case "effect":
		c.handleLightEffect(ctx, addr, eoj, dev, payload, lightSpec, metricSpecs, writable)
	default:
		mqttLog.Debugf("commander: ignored light attribute %q", attr)
	}
}

func (c *Commander) handleLightPower(ctx context.Context, addr string, eoj [3]byte, dev *config.Device, payload string, writable map[byte]struct{}) {
	if _, ok := writable[operationStatusEPC]; !ok {
		mqttLog.Warnf("commander: device %s operation_status (0x80) not writable", dev.Name)
		return
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
	_, err := c.client.SendSet(ctx, addr, eoj, operationStatusEPC, edt)
	if err != nil {
		mqttLog.Warnf("commander: Set 0x80 failed for %s: %v", dev.Name, err)
		c.triggerStateUpdate(dev, 0, eoj, operationStatusEPC)
		return
	}
	mqttLog.Infof("commander: set %s light power %s", dev.Name, payload)
	c.verifyStateUpdate(dev, eoj, []pendingUpdate{{epc: operationStatusEPC, edt: edt}})
}

func (c *Commander) handleLightBrightness(ctx context.Context, addr string, eoj [3]byte, dev *config.Device, payload string, lightSpec *specs.LightSpec, metricSpecs []specs.MetricSpec, writable map[byte]struct{}) {
	if lightSpec == nil || lightSpec.BrightnessEPC == 0 {
		return
	}
	epc := lightSpec.BrightnessEPC
	if _, ok := writable[epc]; !ok {
		mqttLog.Warnf("commander: device %s brightness EPC 0x%02x not writable", dev.Name, epc)
		return
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
	_, err = c.client.SendSet(ctx, addr, eoj, epc, edt)
	if err != nil {
		mqttLog.Warnf("commander: Set brightness failed for %s: %v", dev.Name, err)
		c.triggerStateUpdate(dev, 0, eoj, epc)
		return
	}
	mqttLog.Infof("commander: set %s brightness %s", dev.Name, payload)
	c.verifyStateUpdate(dev, eoj, []pendingUpdate{{epc: epc, edt: edt}})
}

func (c *Commander) handleLightEffect(ctx context.Context, addr string, eoj [3]byte, dev *config.Device, payload string, lightSpec *specs.LightSpec, metricSpecs []specs.MetricSpec, writable map[byte]struct{}) {
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

	if _, ok := writable[epc]; !ok {
		mqttLog.Warnf("commander: device %s effect EPC 0x%02x not writable", dev.Name, epc)
		return
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
	_, err = c.client.SendSet(ctx, addr, eoj, epc, edt)
	if err != nil {
		mqttLog.Warnf("commander: Set effect failed for %s: %v", dev.Name, err)
		c.triggerStateUpdate(dev, 0, eoj, epc)
		return
	}
	mqttLog.Infof("commander: set %s effect %s", dev.Name, payload)
	c.verifyStateUpdate(dev, eoj, []pendingUpdate{{epc: epc, edt: edt}})
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

func (c *Commander) verifyStateUpdate(dev *config.Device, eoj [3]byte, updates []pendingUpdate) {
	go func() {
		delays := []time.Duration{1 * time.Second, 3 * time.Second, 3 * time.Second}
		for attempt, delay := range delays {
			select {
			case <-c.ctx.Done():
				return
			case <-time.After(delay):
			}

			ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
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
					c.cache.Update(*dev, "verify_update", 0, true, 0, metrics, "")
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

func (c *Commander) triggerStateUpdate(dev *config.Device, delay time.Duration, eoj [3]byte, epcs ...byte) {
	go func() {
		if delay > 0 {
			select {
			case <-c.ctx.Done():
				return
			case <-time.After(delay):
			}
		} else {
			if c.ctx.Err() != nil {
				return
			}
		}
		ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
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
			c.cache.Update(*dev, "set_update", 0, true, 0, metrics, "")
			mqttLog.Debugf("commander: immediate update for %s parsed %d metrics", dev.Name, len(metrics))
		}
	}()
}
